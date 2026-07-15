package cli

import (
	"fmt"
	"runtime"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
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
				examplePath = `C:\path\to\boilerplates\environments\local`
			case "darwin":
				examplePath = `/Users/username/path/to/boilerplates/environments/local`
			default:
				examplePath = `/home/username/path/to/boilerplates/environments/local`
			}

			form := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Which variant do you want to create?").
						Options(
							huh.NewOption("Project (Currently Disabled)", "Project"),
							huh.NewOption("Feature", "Feature"),
						).
						Value(&variant).
						Validate(func(v string) error {
							if v == "Project" {
								return fmt.Errorf("Project initialization is currently disabled")
							}
							return nil
						}),
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
							huh.NewOption("Remote (Need Login)", "Remote"),
							huh.NewOption("Local (For Test Purposes)", "Local"),
						).
						Value(&accessMethod),
				),
			)

			err := form.Run()
			if err != nil {
				return err
			}

			if accessMethod == "Local" {
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
				fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700")).Render("\nRemote access is not implemented yet."))
				return nil
			}

			if variant == "Feature" && accessMethod == "Local" {
				sourcePath := localPath

				
				destPath := "."
				if targetLocation == "new_dir" {
					destPath = title
				}

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

				successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
				fmt.Println(successStyle.Render(fmt.Sprintf("✓ Successfully initialized feature '%s' in %s", title, destPath)))
			}

			return nil
		},
	}

	return cmd
}
