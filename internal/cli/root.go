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

	// Store globals in annotations or simply pass them down to subcommands when creating them.
	// For now, if we had subcommands, we would pass 'fileSystem' and '&globals' to their constructors.
	// Example:
	// cmd.AddCommand(NewInitCmd(fileSystem, &globals))

	// Example usage of the injected file system in a hypothetical subcommand or logic:
	// _ = fileSystem.MkdirAll("example", 0755)

	return cmd
}

// Execute is a convenience function if needed, but normally main.go will call root.Execute()
func Execute(fileSystem fs.FS) error {
	rootCmd := NewRootCmd(fileSystem)
	return rootCmd.Execute()
}
