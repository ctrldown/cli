package tui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

var (
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#FF5F87")).
			Padding(0, 1).
			MarginRight(1).
			Bold(true).
			SetString("ERROR")

	messageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F87")).
			Bold(true)
)

// PrintError nicely formats and prints an error message to stderr.
func PrintError(err error) {
	fmt.Fprintf(os.Stderr, "%s %s\n", errorStyle.Render(), messageStyle.Render(err.Error()))
}

// FatalError prints an error nicely and exits the program with a non-zero status.
// If debug is true, it may (in the future) print stack traces, but for now just prints the error.
func FatalError(err error, debug bool) {
	if debug {
		// In a more complex setup, you'd unwrap the error or print a stack trace here.
		// For now, we print it standardly.
		fmt.Fprintf(os.Stderr, "DEBUG: %+v\n", err)
	} else {
		PrintError(err)
	}
	os.Exit(1)
}
