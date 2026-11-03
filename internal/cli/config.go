package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/auth"
	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/fs"
)

type defaultMicrofeatureRepo struct {
	ID           string
	RepoURL      string
	RequiredTier string
}

var defaultMicrofeatures = []defaultMicrofeatureRepo{
	{ID: "bloub", RepoURL: "git@github.com:ctrldown/bloub.git", RequiredTier: "free"},
	{ID: "auth", RepoURL: "git@github.com:ctrldown/auth.git", RequiredTier: "free"},
	{ID: "ctrlweb", RepoURL: "git@github.com:ctrldown/ctrlweb.git", RequiredTier: "pro"},
}

// NewConfigCmd creates the config command for managing global ~/.ctrldown/config.yaml and syncing microfeature repositories.
func NewConfigCmd(fileSystem fs.FS) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [show|set|sync]",
		Short: "Manage global Ctrldown configuration and sync boilerplates & microfeature repositories",
		Long: `Manages user-wide settings in ~/.ctrldown/config.yaml so the standalone ctrldown binary
can locate boilerplates and independent microfeature repositories from any working directory on Linux, Windows, and macOS.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow(fileSystem)
		},
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Display active global configuration and resolved library paths",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow(fileSystem)
		},
	}

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a global configuration key (library_path, boilerplate_path, boilerplate_repo, github_repo)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigSet(fileSystem, args[0], args[1])
		},
	}

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync boilerplates and entitled microfeature repositories into ~/.ctrldown/",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigSync(fileSystem)
		},
	}

	cmd.AddCommand(showCmd, setCmd, syncCmd)
	return cmd
}

func runConfigShow(fileSystem fs.FS) error {
	globalConf, err := config.LoadGlobalConfig(fileSystem)
	if err != nil {
		return err
	}

	cfgPath, _ := config.GlobalConfigPath()
	resolvedLib := config.ResolveLibraryPath(fileSystem, "", ".")
	resolvedBp := config.ResolveBoilerplatePath(fileSystem, "", ".")
	creds, _ := auth.LoadCredentials(fileSystem)

	libExists, _ := afero.DirExists(fileSystem, resolvedLib)
	bpExists, _ := afero.DirExists(fileSystem, resolvedBp)

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
	labelStyle := lipgloss.NewStyle().Bold(true)
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))

	formatStatus := func(exists bool) string {
		if exists {
			return okStyle.Render("[ready]")
		}
		return warnStyle.Render("[not found]")
	}

	authDisplay := warnStyle.Render("Not logged in (Free tier)")
	if creds != nil && creds.Email != "" {
		authDisplay = okStyle.Render(fmt.Sprintf("%s (%s tier)", creds.Email, strings.ToUpper(creds.Tier)))
	}

	fmt.Println(titleStyle.Render("=== Ctrldown Global Configuration ==="))
	fmt.Printf("  %-22s %s\n", labelStyle.Render("Config File:"), cfgPath)
	fmt.Printf("  %-22s %s\n", labelStyle.Render("Auth Session:"), authDisplay)
	fmt.Printf("  %-22s %s %s\n", labelStyle.Render("Microfeatures Dir:"), resolvedLib, formatStatus(libExists))
	fmt.Printf("  %-22s %s %s\n", labelStyle.Render("Boilerplate Dir:"), resolvedBp, formatStatus(bpExists))
	fmt.Printf("  %-22s %s\n", labelStyle.Render("Boilerplate Repo:"), globalConf.BoilerplateRepo)
	fmt.Printf("  %-22s %s\n", labelStyle.Render("CLI GitHub Repo:"), globalConf.GitHubRepo)
	return nil
}

func runConfigSet(fileSystem fs.FS, key, value string) error {
	globalConf, err := config.LoadGlobalConfig(fileSystem)
	if err != nil {
		return err
	}

	normalizedKey := strings.ToLower(strings.TrimSpace(key))
	switch normalizedKey {
	case "library_path", "library", "features_path":
		if abs, err := filepath.Abs(value); err == nil {
			value = abs
		}
		globalConf.LibraryPath = value
	case "boilerplate_path", "boilerplate", "boilerplates_path":
		if abs, err := filepath.Abs(value); err == nil {
			value = abs
		}
		globalConf.BoilerplatePath = value
	case "library_repo":
		globalConf.LibraryRepo = value
	case "boilerplate_repo":
		globalConf.BoilerplateRepo = value
	case "github_repo":
		globalConf.GitHubRepo = value
	default:
		return fmt.Errorf("unknown configuration key %q (valid keys: library_path, boilerplate_path, boilerplate_repo, github_repo)", key)
	}

	if err := config.SaveGlobalConfig(fileSystem, globalConf); err != nil {
		return err
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Updated %s = %s in global configuration", normalizedKey, value)))
	return nil
}

func runConfigSync(fileSystem fs.FS) error {
	globalConf, err := config.LoadGlobalConfig(fileSystem)
	if err != nil {
		return err
	}
	creds, _ := auth.LoadCredentials(fileSystem)

	homeDir, err := config.GlobalHomeDir()
	if err != nil {
		return err
	}
	if err := fileSystem.MkdirAll(homeDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", homeDir, err)
	}

	targetBp := globalConf.BoilerplatePath
	if targetBp == "" {
		targetBp = filepath.Join(homeDir, "boilerplates")
	}
	targetLib := globalConf.LibraryPath
	if targetLib == "" {
		targetLib = filepath.Join(homeDir, "features")
	}
	if err := fileSystem.MkdirAll(targetLib, 0755); err != nil {
		return err
	}

	if err := syncGitRepository(targetBp, globalConf.BoilerplateRepo, "boilerplates"); err != nil {
		return err
	}

	lockedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87")).Bold(true)
	for _, mf := range defaultMicrofeatures {
		if !auth.IsEntitled(creds, mf.ID, mf.RequiredTier) {
			fmt.Println(lockedStyle.Render(fmt.Sprintf("🔒 Skipping locked microfeature '%s' (requires %s tier — run 'ctrldown login')", mf.ID, strings.ToUpper(mf.RequiredTier))))
			continue
		}
		mfDir := filepath.Join(targetLib, mf.ID)
		if err := syncGitRepository(mfDir, mf.RepoURL, "microfeature:"+mf.ID); err != nil {
			return err
		}
	}

	globalConf.BoilerplatePath = targetBp
	globalConf.LibraryPath = targetLib
	if err := config.SaveGlobalConfig(fileSystem, globalConf); err != nil {
		return err
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
	fmt.Println(successStyle.Render("✓ Synchronized boilerplates and entitled microfeature repositories!"))
	return nil
}

func syncGitRepository(targetDir, repoURL, label string) error {
	gitDir := filepath.Join(targetDir, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		fmt.Printf("• Pulling latest %s in %s...\n", label, targetDir)
		cmd := exec.Command("git", "-C", targetDir, "pull", "--ff-only")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to git pull %s in %s: %w", label, targetDir, err)
		}
		return nil
	}

	if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
		fmt.Printf("• Using existing local %s directory at %s\n", label, targetDir)
		return nil
	}

	fmt.Printf("• Cloning %s from %s into %s...\n", label, repoURL, targetDir)
	cmd := exec.Command("git", "clone", "--depth", "1", repoURL, targetDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to git clone %s (%s): %w", label, repoURL, err)
	}
	return nil
}
