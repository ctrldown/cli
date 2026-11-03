package cli

import (
	"fmt"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/engine"
	"ctrldown/cli/internal/fs"
)

// NewUpgradeCmd creates the upgrade command for migrating ctrl.yaml schema, syncing drift, and updating features.
func NewUpgradeCmd(fileSystem fs.FS) *cobra.Command {
	var (
		projectDir      string
		libraryPath     string
		upgradeFeatures bool
		nonInteractive  bool
	)

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade ctrl.yaml schema, sync architecture drift, and update installed features",
		Long:  `Upgrades the project's ctrl.yaml to the latest schema version, synchronizes untracked domains/Go Echo modules/packages from disk, locks unlocked features, and optionally updates outdated features from the Ctrldown Library.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedLibPath := resolveDefaultLibraryPath(fileSystem, libraryPath)

			// Check status first to see if any features are outdated and prompt if interactive
			if !nonInteractive && !upgradeFeatures {
				if currentStatus, err := engine.CheckProjectStatus(fileSystem, projectDir, resolvedLibPath); err == nil {
					hasOutdatedFeatures := false
					for _, f := range currentStatus.FeatureStatuses {
						if f.State == "outdated" {
							hasOutdatedFeatures = true
							break
						}
					}
					if hasOutdatedFeatures {
						_ = huh.NewForm(
							huh.NewGroup(
								huh.NewConfirm().
									Title("Outdated features were found in the Ctrldown Library. Re-apply and update them now?").
									Value(&upgradeFeatures),
							),
						).Run()
					}
				}
			}

			var report *engine.ProjectStatusReport
			var err error

			action := func() {
				report, err = engine.UpgradeProject(fileSystem, projectDir, resolvedLibPath, upgradeFeatures)
				time.Sleep(300 * time.Millisecond)
			}

			if nonInteractive {
				report, err = engine.UpgradeProject(fileSystem, projectDir, resolvedLibPath, upgradeFeatures)
			} else {
				_ = spinner.New().
					Title("Upgrading ctrl.yaml schema and synchronizing project state...").
					Action(action).
					Run()
			}

			if err != nil {
				return fmt.Errorf("failed to upgrade project: %w", err)
			}

			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
			fmt.Println(successStyle.Render(fmt.Sprintf("✓ Successfully upgraded project '%s' to schema v%d!\n", report.ProjectTitle, report.SchemaVersion)))

			printStatusReport(report)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectDir, "dir", ".", "Project directory to upgrade")
	cmd.Flags().StringVar(&libraryPath, "library", "", "Optional Ctrldown features library path")
	cmd.Flags().BoolVar(&upgradeFeatures, "features", false, "Re-apply and update outdated features from the Ctrldown Library")
	cmd.Flags().BoolVar(&nonInteractive, "yes", false, "Skip interactive prompts")

	return cmd
}
