package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/auth"
	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/engine"
	"ctrldown/cli/internal/fs"
)

// resolveDefaultLibraryPath finds the nearest or globally configured ctrldown/features directory.
func resolveDefaultLibraryPath(fileSystem afero.Fs, explicitPath string) string {
	return config.ResolveLibraryPath(fileSystem, explicitPath, ".")
}

// resolveDefaultBoilerplatePath finds the nearest or globally configured ctrldown/boilerplates directory.
func resolveDefaultBoilerplatePath(fileSystem afero.Fs, explicitPath string) string {
	return config.ResolveBoilerplatePath(fileSystem, explicitPath, ".")
}

// NewCreateCmd creates a new instance of the create command for scaffolding monolithic projects.
func NewCreateCmd(fileSystem fs.FS) *cobra.Command {
	var (
		title              string
		targetLocation     string
		database           string
		authStack          string
		frontendDomains    []string
		selectedFeatures   []string
		includeSharedUI    bool
		withCtrlwebExample bool
		boilerplatePath    string
		libraryPath        string
		nonInteractive     bool
	)

	cmd := &cobra.Command{
		Use:   "create [project-name]",
		Short: "Create a new monolithic project (Turborepo + pnpm + Go Echo Backend)",
		Long:  `Scaffolds a production-ready monolithic monorepo following the Etkilibagis standard with a Go Echo v4 backend (domains/api), workspace packages, optional Next.js frontend domains, and pre-installed Ctrldown features.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && title == "" {
				title = args[0]
			}

			resolvedLibPath := resolveDefaultLibraryPath(fileSystem, libraryPath)
			libFeatures, _ := engine.DiscoverLibraryFeatures(fileSystem, resolvedLibPath)
			creds, _ := auth.LoadCredentials(fileSystem)

			if !nonInteractive {
				if targetLocation == "" {
					targetLocation = "new_dir"
				}
				if database == "" {
					database = "postgres"
				}
				if authStack == "" {
					authStack = "ory"
				}
				if len(frontendDomains) == 0 {
					frontendDomains = []string{"landing", "dashboard"}
				}
				includeSharedUI = true

				groups := []*huh.Group{
					huh.NewGroup(
						huh.NewInput().
							Title("Project name:").
							Placeholder("my-monolith").
							Value(&title).
							Validate(func(s string) error {
								if len(s) == 0 {
									return fmt.Errorf("project name is required")
								}
								return nil
							}),
						huh.NewSelect[string]().
							Title("Where should the project be created?").
							Options(
								huh.NewOption("New directory named after project", "new_dir"),
								huh.NewOption("Current directory (.)", "current"),
							).
							Value(&targetLocation),
					),
					huh.NewGroup(
						huh.NewSelect[string]().
							Title("Select database for Go Echo Backend (domains/api):").
							Options(
								huh.NewOption("PostgreSQL (GORM + Docker Compose)", "postgres"),
								huh.NewOption("SQLite (Lightweight / Local)", "sqlite"),
								huh.NewOption("None (Stateless API)", "none"),
							).
							Value(&database),
						huh.NewSelect[string]().
							Title("Select authentication stack:").
							Options(
								huh.NewOption("Ory Kratos & Keto (Identity & RBAC)", "ory"),
								huh.NewOption("None", "none"),
							).
							Value(&authStack),
					),
					huh.NewGroup(
						huh.NewMultiSelect[string]().
							Title("Select frontend domains to scaffold (Space to toggle, Enter to confirm):").
							Options(
								huh.NewOption("Landing (Public Next.js Website)", "landing"),
								huh.NewOption("Dashboard (Admin Next.js Panel)", "dashboard"),
								huh.NewOption("Ctrlweb (Visual Page Builder)", "ctrlweb"),
							).
							Value(&frontendDomains),
						huh.NewConfirm().
							Title("Include shared @workspace/ui design system package?").
							Value(&includeSharedUI),
					),
				}

				if len(libFeatures) > 0 {
					featOptions := make([]huh.Option[string], 0, len(libFeatures))
					for _, f := range libFeatures {
						badge := "🔓"
						if !auth.IsEntitled(creds, f.ID, f.RequiredTier) {
							badge = fmt.Sprintf("🔒 [Locked: %s]", f.RequiredTier)
						}
						label := fmt.Sprintf("%s %s — %s (%s)", badge, f.ID, f.Title, f.Category)
						featOptions = append(featOptions, huh.NewOption(label, f.ID))
					}
					groups = append(groups, huh.NewGroup(
						huh.NewMultiSelect[string]().
							Title("Select microfeatures from the Ctrldown Catalog to pre-install (Space to toggle):").
							Options(featOptions...).
							Value(&selectedFeatures),
					))
				}

				form := huh.NewForm(groups...)
				if err := form.Run(); err != nil {
					return err
				}
			}

			// Verify entitlements for selected microfeatures
			featureMap := make(map[string]engine.FeatureManifest, len(libFeatures))
			for _, f := range libFeatures {
				featureMap[f.ID] = f
			}
			for _, sel := range selectedFeatures {
				if f, ok := featureMap[sel]; ok && !auth.IsEntitled(creds, f.ID, f.RequiredTier) {
					return fmt.Errorf("🔒 microfeature %q is locked (requires %s tier). Run 'ctrldown login' with an entitled account to unlock it", f.ID, f.RequiredTier)
				}
			}

			if title == "" {
				return fmt.Errorf("project name is required")
			}

			destPath := "."
			if targetLocation == "new_dir" || targetLocation == "" {
				destPath = title
			} else if targetLocation != "current" && targetLocation != "." {
				destPath = targetLocation
			}

			resolvedBpPath := resolveDefaultBoilerplatePath(fileSystem, boilerplatePath)
			opts := engine.ProjectOptions{
				Title:                 title,
				DestPath:              destPath,
				BoilerplatePath:       resolvedBpPath,
				LibraryPath:           resolvedLibPath,
				Database:              database,
				Auth:                  authStack,
				FrontendDomains:       frontendDomains,
				IncludeSharedUI:       includeSharedUI,
				Features:              selectedFeatures,
				WithCtrlwebExampleApp: withCtrlwebExample,
			}

			var bootErr error
			action := func() {
				_, bootErr = engine.BootstrapProject(fileSystem, opts)
				time.Sleep(300 * time.Millisecond)
			}

			if nonInteractive {
				_, bootErr = engine.BootstrapProject(fileSystem, opts)
			} else {
				_ = spinner.New().
					Title(fmt.Sprintf("Scaffolding monolithic project '%s'...", title)).
					Action(action).
					Run()
			}

			if bootErr != nil {
				return fmt.Errorf("failed to create monolithic project: %w", bootErr)
			}

			absDest, err := filepath.Abs(destPath)
			if err != nil {
				absDest = destPath
			}

			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
			infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
			fmt.Println(successStyle.Render(fmt.Sprintf("✓ Successfully created monolithic project '%s' in %s", title, absDest)))
			fmt.Println(infoStyle.Render("  • Backend: domains/api (Go Echo v4 + /health ready)"))
			if len(frontendDomains) > 0 {
				fmt.Println(infoStyle.Render(fmt.Sprintf("  • Frontend Domains: %v", frontendDomains)))
			}
			if len(selectedFeatures) > 0 {
				fmt.Println(infoStyle.Render(fmt.Sprintf("  • Pre-installed Features: %v", selectedFeatures)))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "Project title")
	cmd.Flags().StringVar(&targetLocation, "dir", "", "Target location ('current', 'new_dir', or path)")
	cmd.Flags().StringVar(&database, "db", "postgres", "Database for Go Echo backend (postgres, sqlite, none)")
	cmd.Flags().StringVar(&authStack, "auth", "ory", "Authentication stack (ory, none)")
	cmd.Flags().StringSliceVar(&frontendDomains, "domains", []string{"landing", "dashboard"}, "Frontend domains to scaffold")
	cmd.Flags().StringSliceVar(&selectedFeatures, "features", nil, "Features from Ctrldown Library to pre-install (e.g. bloub,auth,ctrlweb)")
	cmd.Flags().BoolVar(&includeSharedUI, "shared-ui", true, "Include @workspace/ui shared package")
	cmd.Flags().BoolVar(&withCtrlwebExample, "ctrlweb-example", false, "When ctrlweb is included, also scaffold an ultra-minimal Next.js example consumer app")
	cmd.Flags().StringVar(&boilerplatePath, "boilerplate", "", "Optional path to local boilerplates repository")
	cmd.Flags().StringVar(&libraryPath, "library", "", "Optional path to Ctrldown features library directory")
	cmd.Flags().BoolVar(&nonInteractive, "yes", false, "Skip interactive prompts and use flags/defaults")

	return cmd
}
