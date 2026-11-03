package auth

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
	"go.yaml.in/yaml/v3"
	"ctrldown/cli/internal/config"
)

const DefaultEdgeEndpoint = "https://api.ctrldown.dev"

// Credentials stores the user's Cloudflare Edge session and entitlement claims in ~/.ctrldown/credentials.yaml.
type Credentials struct {
	AccessToken     string   `yaml:"access_token" json:"access_token"`
	Email           string   `yaml:"email,omitempty" json:"email,omitempty"`
	Tier            string   `yaml:"tier,omitempty" json:"tier,omitempty"`
	Entitlements    []string `yaml:"entitlements,omitempty" json:"entitlements,omitempty"`
	Endpoint        string   `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	AuthenticatedAt string   `yaml:"authenticated_at,omitempty" json:"authenticated_at,omitempty"`
}

// RemoteCatalogItem represents a microfeature entry returned by the Cloudflare Worker /v1/catalog endpoint.
type RemoteCatalogItem struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Category     string `json:"category"`
	Repository   string `json:"repository,omitempty"`
	RequiredTier string `json:"required_tier"`
	Entitled     bool   `json:"entitled"`
}

// DeviceCodeResponse is returned by POST /v1/auth/device/code on the Cloudflare Worker.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// CredentialsPath returns ~/.ctrldown/credentials.yaml.
func CredentialsPath() (string, error) {
	homeDir, err := config.GlobalHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, "credentials.yaml"), nil
}

// LoadCredentials reads ~/.ctrldown/credentials.yaml if present.
func LoadCredentials(fs afero.Fs) (*Credentials, error) {
	credPath, err := CredentialsPath()
	if err != nil {
		return nil, err
	}
	exists, err := afero.Exists(fs, credPath)
	if err != nil || !exists {
		return nil, fmt.Errorf("not logged in")
	}
	data, err := afero.ReadFile(fs, credPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials: %w", err)
	}
	var creds Credentials
	if err := yaml.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}
	if creds.Endpoint == "" {
		creds.Endpoint = DefaultEdgeEndpoint
	}
	return &creds, nil
}

// SaveCredentials writes ~/.ctrldown/credentials.yaml with 0600 permissions.
func SaveCredentials(fs afero.Fs, creds *Credentials) error {
	credPath, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := fs.MkdirAll(filepath.Dir(credPath), 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}
	if creds.Endpoint == "" {
		creds.Endpoint = DefaultEdgeEndpoint
	}
	if creds.AuthenticatedAt == "" {
		creds.AuthenticatedAt = time.Now().Format(time.RFC3339)
	}
	data, err := yaml.Marshal(creds)
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}
	return afero.WriteFile(fs, credPath, data, 0600)
}

// ClearCredentials removes ~/.ctrldown/credentials.yaml.
func ClearCredentials(fs afero.Fs) error {
	credPath, err := CredentialsPath()
	if err != nil {
		return err
	}
	if exists, _ := afero.Exists(fs, credPath); !exists {
		return nil
	}
	return fs.Remove(credPath)
}

// IsEntitled evaluates whether the given user credentials grant access to a microfeature.
// Free-tier microfeatures are accessible unless explicitly restricted; pro/enterprise/custom
// microfeatures require an active login with matching tier or explicit feature entitlement.
func IsEntitled(creds *Credentials, featureID string, requiredTier string) bool {
	tier := strings.ToLower(strings.TrimSpace(requiredTier))
	if tier == "" || tier == "free" || tier == "public" {
		return true
	}
	if creds == nil || strings.TrimSpace(creds.AccessToken) == "" {
		return false
	}

	for _, ent := range creds.Entitlements {
		e := strings.ToLower(strings.TrimSpace(ent))
		if e == "*" || e == strings.ToLower(featureID) {
			return true
		}
	}

	userTierRank := tierRank(creds.Tier)
	reqTierRank := tierRank(tier)
	return reqTierRank > 0 && userTierRank >= reqTierRank
}

func tierRank(tier string) int {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "free":
		return 1
	case "pro":
		return 2
	case "enterprise", "admin":
		return 3
	default:
		return 0
	}
}

// FetchRemoteCatalog queries the Cloudflare Edge Gatekeeper for the microfeature catalog and user entitlements.
func FetchRemoteCatalog(endpoint string, accessToken string) ([]RemoteCatalogItem, error) {
	if endpoint == "" {
		endpoint = os.Getenv("CTRLDOWN_EDGE_URL")
	}
	if endpoint == "" {
		endpoint = DefaultEdgeEndpoint
	}

	url := strings.TrimRight(endpoint, "/") + "/v1/catalog"
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ctrldown-cli/"+config.Version)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach Cloudflare Edge catalog (%s): %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Cloudflare Edge catalog returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Features []RemoteCatalogItem `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode remote catalog response: %w", err)
	}
	return payload.Features, nil
}

// DownloadMicrofeature downloads and extracts an authorized microfeature tarball from the Cloudflare Edge Gatekeeper into destDir.
func DownloadMicrofeature(fs afero.Fs, creds *Credentials, featureID string, destDir string) error {
	if creds == nil || creds.AccessToken == "" {
		return fmt.Errorf("authentication required to download microfeature %q (run 'ctrldown login')", featureID)
	}
	endpoint := creds.Endpoint
	if endpoint == "" {
		endpoint = DefaultEdgeEndpoint
	}

	url := fmt.Sprintf("%s/v1/features/%s/download", strings.TrimRight(endpoint, "/"), featureID)
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("User-Agent", "ctrldown-cli/"+config.Version)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to contact Cloudflare Edge Gatekeeper: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("access denied (HTTP %d): your account is not entitled to pull microfeature %q", resp.StatusCode, featureID)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download microfeature %q: HTTP %d", featureID, resp.StatusCode)
	}

	rawBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return extractTarGzToFs(fs, rawBytes, destDir)
}

func extractTarGzToFs(fs afero.Fs, data []byte, destDir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to open gzip archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		cleanRel := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanRel, "..") {
			continue
		}
		targetPath := filepath.Join(destDir, cleanRel)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := fs.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := fs.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			fileBytes, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			if err := afero.WriteFile(fs, targetPath, fileBytes, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}
