package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/engine"
	"ctrldown/cli/internal/fs"
)

// NewStatusCmd creates the status (and doctor) command for inspecting project schema, drift, and feature updates.
func NewStatusCmd(fileSystem fs.FS) *cobra.Command {
	var (
		projectDir  string
		libraryPath string
	)

	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"doctor"},
		Short:   "Check project ctrl.yaml schema version, architecture drift, and feature updates",
		Long:    `Inspects the project's ctrl.yaml schema version, compares recorded domains/modules/packages against the live filesystem, and checks installed features against the Ctrldown Feature Library.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedLibPath := resolveDefaultLibraryPath(fileSystem, libraryPath)

			report, err := engine.CheckProjectStatus(fileSystem, projectDir, resolvedLibPath)
			if err != nil {
				return err
			}

			printStatusReport(report)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectDir, "dir", ".", "Project directory to check")
	cmd.Flags().StringVar(&libraryPath, "library", "", "Optional Ctrldown features library path")

	return cmd
}

func printStatusReport(report *engine.ProjectStatusReport) {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700")).Bold(true)
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87")).Bold(true)
	labelStyle := lipgloss.NewStyle().Bold(true)

	fmt.Println(titleStyle.Render(fmt.Sprintf("=== Ctrldown Project Status: %s ===", report.ProjectTitle)))

	// 1. Schema Status
	if report.IsSchemaOutdated {
		fmt.Printf("\n%s %s\n",
			warnStyle.Render("⚠ Config Schema:"),
			fmt.Sprintf("v%d -> v%d available (run 'ctrldown upgrade' to update ctrl.yaml schema)", report.SchemaVersion, report.LatestSchemaVersion),
		)
	} else {
		fmt.Printf("\n%s %s\n",
			okStyle.Render("✓ Config Schema:"),
			fmt.Sprintf("v%d (up to date, CLI v%s)", report.SchemaVersion, report.CLIVersion),
		)
	}

	// 2. Architecture / Filesystem Drift
	if !report.HasArchitectureDrift() {
		fmt.Printf("%s %s\n",
			okStyle.Render("✓ Architecture Sync:"),
			"All domains, Go Echo modules, and workspace packages match ctrl.yaml",
		)
	} else {
		fmt.Printf("%s %s\n",
			warnStyle.Render("⚠ Architecture Drift Detected:"),
			"(run 'ctrldown upgrade' to synchronize ctrl.yaml)",
		)
		if len(report.UntrackedDomains) > 0 {
			fmt.Printf("    %s %s\n", labelStyle.Render("+ Untracked Domains:"), strings.Join(report.UntrackedDomains, ", "))
		}
		if len(report.MissingDomains) > 0 {
			fmt.Printf("    %s %s\n", errStyle.Render("- Missing Domains:"), strings.Join(report.MissingDomains, ", "))
		}
		if len(report.UntrackedBackendModules) > 0 {
			fmt.Printf("    %s %s\n", labelStyle.Render("+ Untracked Go Echo Modules:"), strings.Join(report.UntrackedBackendModules, ", "))
		}
		if len(report.MissingBackendModules) > 0 {
			fmt.Printf("    %s %s\n", errStyle.Render("- Missing Go Echo Modules:"), strings.Join(report.MissingBackendModules, ", "))
		}
		if len(report.UntrackedPackages) > 0 {
			fmt.Printf("    %s %s\n", labelStyle.Render("+ Untracked Packages:"), strings.Join(report.UntrackedPackages, ", "))
		}
		if len(report.MissingPackages) > 0 {
			fmt.Printf("    %s %s\n", errStyle.Render("- Missing Packages:"), strings.Join(report.MissingPackages, ", "))
		}
	}

	// 3. Installed Features & Library Update Status
	if len(report.FeatureStatuses) == 0 {
		fmt.Printf("%s None installed\n", labelStyle.Render("• Installed Features:"))
		return
	}

	fmt.Printf("\n%s\n", labelStyle.Render("Installed Features:"))
	for _, f := range report.FeatureStatuses {
		driftBadge := ""
		if f.HasLocalDrift {
			driftBadge = warnStyle.Render(" [local files modified]")
		}

		switch f.State {
		case "up-to-date":
			fmt.Printf("  %s %-12s v%s (%s)%s\n",
				okStyle.Render("✓"),
				f.Name,
				f.InstalledVersion,
				f.InstalledChecksum,
				driftBadge,
			)
		case "outdated":
			fmt.Printf("  %s %-12s installed: v%s (%s) -> library: v%s (%s)%s\n",
				warnStyle.Render("↑"),
				f.Name,
				f.InstalledVersion,
				f.InstalledChecksum,
				f.LibraryVersion,
				f.LibraryChecksum,
				driftBadge,
			)
		case "unlocked":
			fmt.Printf("  %s %-12s unlocked (run 'ctrldown upgrade' to record version & checksum lock)\n",
				warnStyle.Render("!"),
				f.Name,
			)
		default:
			fmt.Printf("  • %-12s (%s)\n", f.Name, f.State)
		}
	}
}
