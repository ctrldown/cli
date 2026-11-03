package main

import (
	"ctrldown/cli/internal/cli"
	"ctrldown/cli/internal/fs"
	"ctrldown/cli/internal/tui"
)

func main() {
	// Initialize the virtual file system (backed by OS)
	fileSystem := fs.New()

	// Instantiate the root command with dependencies
	rootCmd := cli.NewRootCmd(fileSystem)

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		// Check if the --debug flag was set to show more detailed error information
		debug := false
		if flag := rootCmd.Flag("debug"); flag != nil && flag.Value.String() == "true" {
			debug = true
		}

		// Let the TUI package handle displaying the error gracefully
		tui.FatalError(err, debug)
	}
}
