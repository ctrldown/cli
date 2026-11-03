package cli

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/engine"
	"ctrldown/cli/internal/fs"
)

// NewInitCmd creates a new instance of the init command.
func NewInitCmd(fileSystem fs.FS) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new ctrldown project or feature",
		Long:  `Bootstraps a new project or feature by copying the foundational environment files from the boilerplate system.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var variant string
			var title string
			var accessMethod string
			var localPath string
			var targetLocation string

			var examplePath string
			switch runtime.GOOS {
			case "windows":
				examplePath = `C:\path\to\boilerplates`
			case "darwin":
				examplePath = `/Users/username/path/to/boilerplates`
			default:
				examplePath = `/home/username/path/to/boilerplates`
			}

			form := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Which variant do you want to create?").
						Options(
							huh.NewOption("Project (Monolithic Monorepo + Go Echo)", "Project"),
							huh.NewOption("Feature (Distributed Microfeature)", "Feature"),
						).
						Value(&variant),
				),

				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Where should the boilerplate be copied?").
						Options(
							huh.NewOption("Current Directory (.)", "current"),
							huh.NewOption("New directory named after title", "new_dir"),
						).
						Value(&targetLocation),
				),

				huh.NewGroup(
					huh.NewInput().
						Title("Give it a title:").
						Value(&title).
						Validate(func(s string) error {
							if len(s) == 0 {
								return fmt.Errorf("title is required")
							}
							return nil
						}),
				),

				huh.NewGroup(
					huh.NewSelect[string]().
						Title("How do you want to access the Boilerplate System?").
						Options(
							huh.NewOption("Global / Remote Sync (~/.ctrldown/boilerplates)", "Remote"),
							huh.NewOption("Custom Local Directory", "Local"),
						).
						Value(&accessMethod),
				),
			)

			err := form.Run()
			if err != nil {
				return err
			}

			if accessMethod == "Local" {
				localPath = resolveDefaultBoilerplatePath(fileSystem, "")
				localForm := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().
							Title("Enter the exact local boilerplate directory path:").
							Placeholder(examplePath).
							Value(&localPath).
							Validate(func(s string) error {
								if len(s) == 0 {
									return fmt.Errorf("local path is required")
								}
								return nil
							}),
					),
				)
				err = localForm.Run()
				if err != nil {
					return err
				}
			}

			if accessMethod == "Remote" {
				resolvedBp := resolveDefaultBoilerplatePath(fileSystem, "")
				if exists, _ := afero.DirExists(fileSystem, resolvedBp); !exists {
					if syncErr := runConfigSync(fileSystem); syncErr != nil {
						return fmt.Errorf("failed to sync remote boilerplates: %w", syncErr)
					}
					resolvedBp = resolveDefaultBoilerplatePath(fileSystem, "")
				}
				localPath = resolvedBp
			}

			destPath := "."
			if targetLocation == "new_dir" {
				destPath = title
			}

			absDestPath, pathErr := filepath.Abs(destPath)
			if pathErr != nil {
				absDestPath = destPath
			}

			absLocalPath, pathErr := filepath.Abs(localPath)
			if pathErr != nil {
				absLocalPath = localPath
			}

			if variant == "Project" {
				action := func() {
					_, err = engine.BootstrapProject(fileSystem, engine.ProjectOptions{
						Title:           title,
						DestPath:        destPath,
						BoilerplatePath: absLocalPath,
						LibraryPath:     resolveDefaultLibraryPath(fileSystem, ""),
						Database:        "postgres",
						Auth:            "ory",
						FrontendDomains: []string{"landing", "dashboard"},
						IncludeSharedUI: true,
					})
					time.Sleep(500 * time.Millisecond)
				}

				_ = spinner.New().
					Title("Initializing monolithic project...").
					Action(action).
					Run()

				if err != nil {
					return fmt.Errorf("failed to initialize project: %w", err)
				}

				successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
				fmt.Println(successStyle.Render(fmt.Sprintf("✓ Successfully initialized monolithic project '%s' in %s", title, absDestPath)))
				return nil
			}

			if variant == "Feature" {
				sourcePath := filepath.Join(localPath, "environments", "local")

				action := func() {
					err = engine.BootstrapFeature(fileSystem, sourcePath, destPath)
					// Sleep briefly just to let the spinner render if copy is too fast
					time.Sleep(500 * time.Millisecond)
				}

				_ = spinner.New().
					Title("Initializing local environment...").
					Action(action).
					Run()

				if err != nil {
					return fmt.Errorf("failed to initialize feature: %w", err)
				}

				// Create decorated ctrl.yaml configuration file
				ctrlConfig := fmt.Sprintf(`# ========================================================
# CTRLDOWN CONFIGURATION FILE
# ========================================================
# This file contains the local project context for ctrldown.
# Do not modify the boilerplate_path unless necessary.

boilerplate_path: "%s"

project:
  variant: "%s"
  title: "%s"
  created_at: "%s"
`, strings.ReplaceAll(absLocalPath, "\\", "\\\\"), variant, title, time.Now().Format(time.RFC3339))
				_ = afero.WriteFile(fileSystem, filepath.Join(absDestPath, "ctrl.yaml"), []byte(ctrlConfig), 0644)

				successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
				fmt.Println(successStyle.Render(fmt.Sprintf("✓ Successfully initialized feature '%s' in %s", title, absDestPath)))
			}

			return nil
		},
	}

	return cmd
}
