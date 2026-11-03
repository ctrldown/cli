package cli

import (
	"fmt"
	"runtime"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/config"
)

// FormatVersionString returns a single-line human-readable version summary.
func FormatVersionString() string {
	return fmt.Sprintf("ctrldown v%s (commit: %s, built: %s, %s/%s, schema: v%d)",
		config.Version,
		config.Commit,
		config.BuildDate,
		runtime.GOOS,
		runtime.GOARCH,
		config.CurrentSchemaVersion,
	)
}

// NewVersionCmd creates the version command displaying CLI build and platform details.
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ctrldown CLI version, build commit, OS/architecture, and schema version",
		Run: func(cmd *cobra.Command, args []string) {
			titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
			labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)

			fmt.Println(titleStyle.Render(fmt.Sprintf("ctrldown v%s", config.Version)))
			fmt.Printf("  %-16s %s/%s\n", labelStyle.Render("Platform:"), runtime.GOOS, runtime.GOARCH)
			fmt.Printf("  %-16s %s\n", labelStyle.Render("Commit:"), config.Commit)
			fmt.Printf("  %-16s %s\n", labelStyle.Render("Build Date:"), config.BuildDate)
			fmt.Printf("  %-16s v%d\n", labelStyle.Render("Config Schema:"), config.CurrentSchemaVersion)
		},
	}
}
