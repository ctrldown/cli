package cli

import (
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/fs"
)

// GlobalOptions holds flags that are available across all commands.
type GlobalOptions struct {
	Debug bool
}

// NewRootCmd creates a new instance of the root command.
// We inject dependencies here to avoid global state.
func NewRootCmd(fileSystem fs.FS) *cobra.Command {
	var globals GlobalOptions

	cmd := &cobra.Command{
		Use:   "ctrldown",
		Short: "A scaffolding and AST-manipulation engine",
		Long:  `ctrldown is a developer-friendly CLI tool for scaffolding microservices and manipulating ASTs.`,
		// We can define persistent PreRun hooks here to initialize configuration if needed.
		RunE: func(cmd *cobra.Command, args []string) error {
			// Print help by default if no subcommand is given
			return cmd.Help()
		},
	}

	// Persistent flags (available to all subcommands)
	cmd.PersistentFlags().BoolVar(&globals.Debug, "debug", false, "Enable debug output and stack traces")

	// Register subcommands
	cmd.AddCommand(NewInitCmd(fileSystem))
	cmd.AddCommand(NewInstallCmd(fileSystem))

	return cmd
}

// Execute is a convenience function if needed, but normally main.go will call root.Execute()
func Execute(fileSystem fs.FS) error {
	rootCmd := NewRootCmd(fileSystem)
	return rootCmd.Execute()
}
