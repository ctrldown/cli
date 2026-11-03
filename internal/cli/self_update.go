package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/fs"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Name    string        `json:"name"`
	HTMLURL string        `json:"html_url"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// NewSelfUpdateCmd creates the self-update command to update the standalone executable from GitHub Releases.
func NewSelfUpdateCmd(fileSystem fs.FS) *cobra.Command {
	var (
		repo      string
		checkOnly bool
	)

	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Update the ctrldown standalone binary from the latest GitHub Release (Linux, Windows, macOS)",
		Long: `Queries GitHub Releases for the latest ctrldown version matching your OS (linux, windows, darwin)
and architecture (amd64, arm64) and replaces the current executable in place.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if repo == "" {
				if globalConf, err := config.LoadGlobalConfig(fileSystem); err == nil && globalConf.GitHubRepo != "" {
					repo = globalConf.GitHubRepo
				} else {
					repo = config.GitHubRepo
				}
			}

			rel, err := fetchLatestGitHubRelease(repo)
			if err != nil {
				return err
			}

			latestVersion := strings.TrimPrefix(rel.TagName, "v")
			currentVersion := strings.TrimPrefix(config.Version, "v")

			okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
			infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)

			if latestVersion == currentVersion {
				fmt.Println(okStyle.Render(fmt.Sprintf("✓ ctrldown is already up to date (v%s)", currentVersion)))
				return nil
			}

			fmt.Println(infoStyle.Render(fmt.Sprintf("↑ New version available: v%s -> v%s (%s)", currentVersion, latestVersion, rel.HTMLURL)))
			if checkOnly {
				return nil
			}

			asset, found := MatchReleaseAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
			if !found {
				return fmt.Errorf("no release executable found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, rel.TagName)
			}

			fmt.Printf("• Downloading %s...\n", asset.Name)
			binBytes, err := downloadReleaseBinary(asset)
			if err != nil {
				return err
			}

			execPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to locate running executable path: %w", err)
			}
			if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
				execPath = resolved
			}

			if err := replaceExecutable(execPath, binBytes); err != nil {
				return err
			}

			fmt.Println(okStyle.Render(fmt.Sprintf("✓ Successfully updated ctrldown to v%s!", latestVersion)))
			return nil
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository in 'owner/name' format (defaults to ctrldown/cli)")
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates without downloading")

	return cmd
}

// MatchReleaseAsset selects the best release asset for the given OS and architecture.
// Prefers direct standalone executables (e.g. ctrldown_windows_amd64.exe, ctrldown_linux_amd64, ctrldown_darwin_arm64)
// and falls back to .zip or .tar.gz archives.
func MatchReleaseAsset(assets []githubAsset, goos, goarch string) (githubAsset, bool) {
	osAliases := []string{strings.ToLower(goos)}
	switch strings.ToLower(goos) {
	case "darwin":
		osAliases = append(osAliases, "macos", "mac", "osx")
	case "windows":
		osAliases = append(osAliases, "win")
	}

	archAliases := []string{strings.ToLower(goarch)}
	switch strings.ToLower(goarch) {
	case "amd64":
		archAliases = append(archAliases, "x86_64", "x64")
	case "arm64":
		archAliases = append(archAliases, "aarch64")
	}

	matchesPlatform := func(name string) bool {
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".sha256") || strings.HasSuffix(lower, ".sbom") {
			return false
		}
		osMatch := false
		for _, o := range osAliases {
			if strings.Contains(lower, o) {
				osMatch = true
				break
			}
		}
		archMatch := false
		for _, a := range archAliases {
			if strings.Contains(lower, a) {
				archMatch = true
				break
			}
		}
		return osMatch && archMatch
	}

	// First pass: direct standalone binary (not .tar.gz / .zip)
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if matchesPlatform(lower) && !strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".zip") {
			return a, true
		}
	}

	// Second pass: archive (.zip or .tar.gz)
	for _, a := range assets {
		if matchesPlatform(a.Name) {
			return a, true
		}
	}

	return githubAsset{}, false
}

func getGitHubAuthToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	// Try gh CLI if installed
	if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		tok := strings.TrimSpace(string(out))
		if tok != "" {
			return tok
		}
	}
	return ""
}

func fetchLatestGitHubRelease(repo string) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ctrldown-cli/"+config.Version)
	if token := getGitHubAuthToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query GitHub Releases (%s): %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub Releases API returned HTTP %d for %s", resp.StatusCode, repo)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub Release response: %w", err)
	}
	return &rel, nil
}

func downloadReleaseBinary(asset githubAsset) ([]byte, error) {
	token := getGitHubAuthToken()
	downloadURL := asset.URL
	if downloadURL == "" || token == "" {
		downloadURL = asset.BrowserDownloadURL
	}

	client := &http.Client{
		Timeout: 120 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			// If redirected to S3 or a different domain, strip Authorization to prevent S3 auth rejection
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}

	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ctrldown-cli/"+config.Version)
	req.Header.Set("Accept", "application/octet-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download asset %s: %w", asset.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download asset %s: HTTP %d", asset.Name, resp.StatusCode)
	}

	rawBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read downloaded asset: %w", err)
	}

	lower := strings.ToLower(asset.Name)
	if strings.HasSuffix(lower, ".zip") {
		return extractBinaryFromZip(rawBytes)
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return extractBinaryFromTarGz(rawBytes)
	}
	return rawBytes, nil
}

func extractBinaryFromZip(data []byte) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open zip archive: %w", err)
	}
	for _, f := range r.File {
		base := strings.ToLower(filepath.Base(f.Name))
		if base == "ctrldown" || base == "ctrldown.exe" || strings.HasPrefix(base, "ctrldown_") || strings.HasPrefix(base, "ctrldown-") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("ctrldown executable not found inside zip archive")
}

func extractBinaryFromTarGz(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to open gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := strings.ToLower(filepath.Base(hdr.Name))
		if base == "ctrldown" || base == "ctrldown.exe" || strings.HasPrefix(base, "ctrldown_") || strings.HasPrefix(base, "ctrldown-") {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("ctrldown executable not found inside tar.gz archive")
}

func replaceExecutable(execPath string, newBin []byte) error {
	dir := filepath.Dir(execPath)
	base := filepath.Base(execPath)
	tmpPath := filepath.Join(dir, "."+base+".new")
	oldPath := filepath.Join(dir, "."+base+".old")

	if err := os.WriteFile(tmpPath, newBin, 0755); err != nil {
		return fmt.Errorf("failed to write new binary to %s: %w", tmpPath, err)
	}

	_ = os.Remove(oldPath)
	if err := os.Rename(execPath, oldPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to move current binary %s to %s: %w", execPath, oldPath, err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		_ = os.Rename(oldPath, execPath)
		return fmt.Errorf("failed to activate new binary at %s: %w", execPath, err)
	}

	_ = os.Remove(oldPath)
	return nil
}
