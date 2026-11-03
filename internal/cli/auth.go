package cli

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/auth"
	"ctrldown/cli/internal/fs"
)

// openBrowser attempts to open the given URL in the user's default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func NewLoginCmd(fileSystem fs.FS) *cobra.Command {
	var (
		token        string
		email        string
		tier         string
		entitlements []string
		endpoint     string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Ctrldown Edge to unlock licensed microfeatures",
		Long: `Authenticates the CLI with the Ctrldown Cloudflare Edge Gatekeeper and stores your session token
and entitlements in ~/.ctrldown/credentials.yaml.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Direct Token Flag (CI/CD or Headless Mode)
			if token != "" {
				if tier == "" {
					tier = "pro"
				}
				if email == "" {
					email = "user@ctrldown.dev"
				}

				creds := &auth.Credentials{
					AccessToken:  strings.TrimSpace(token),
					Email:        strings.TrimSpace(email),
					Tier:         tier,
					Entitlements: entitlements,
					Endpoint:     endpoint,
				}

				if err := auth.SaveCredentials(fileSystem, creds); err != nil {
					return fmt.Errorf("failed to save credentials: %w", err)
				}

				okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
				fmt.Println(okStyle.Render(fmt.Sprintf("✓ Logged in as %s [Tier: %s]", creds.Email, strings.ToUpper(creds.Tier))))
				return nil
			}

			// 2. Browser Grant Flow (Standard Interactive Login)
			grant, err := auth.RequestDeviceGrant(endpoint)
			if err != nil {
				// Fallback to manual form if endpoint offline or unreachable
				warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
				fmt.Println(warnStyle.Render(fmt.Sprintf("⚠ Could not initiate browser session: %v", err)))
				fmt.Println("Falling back to manual token entry...")

				err := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().
							Title("Enter your Ctrldown Account Email:").
							Placeholder("developer@ctrldown.dev").
							Value(&email),
						huh.NewInput().
							Title("Enter your Ctrldown Edge Access Token:").
							Password(true).
							Value(&token).
							Validate(func(s string) error {
								if strings.TrimSpace(s) == "" {
									return fmt.Errorf("access token is required")
								}
								return nil
							}),
						huh.NewSelect[string]().
							Title("Select account tier:").
							Options(
								huh.NewOption("Pro Tier", "pro"),
								huh.NewOption("Enterprise / Full Access (*)", "enterprise"),
								huh.NewOption("Free / Community Tier", "free"),
							).
							Value(&tier),
					),
				).Run()
				if err != nil {
					return err
				}

				if tier == "" {
					tier = "pro"
				}
				if email == "" {
					email = "user@ctrldown.dev"
				}

				creds := &auth.Credentials{
					AccessToken:  strings.TrimSpace(token),
					Email:        strings.TrimSpace(email),
					Tier:         tier,
					Entitlements: entitlements,
					Endpoint:     endpoint,
				}

				if err := auth.SaveCredentials(fileSystem, creds); err != nil {
					return fmt.Errorf("failed to save credentials: %w", err)
				}

				okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
				fmt.Println(okStyle.Render(fmt.Sprintf("✓ Logged in as %s [Tier: %s]", creds.Email, strings.ToUpper(creds.Tier))))
				return nil
			}

			titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
			linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Underline(true)
			dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B"))

			fmt.Println()
			fmt.Println(titleStyle.Render("=== Ctrldown Authorization ==="))
			fmt.Println("To authenticate, authorize your CLI in the browser:")
			fmt.Println("👉  " + linkStyle.Render(grant.VerificationURI))
			fmt.Println()
			fmt.Println(dimStyle.Render("Opening browser automatically..."))
			fmt.Println(dimStyle.Render("Waiting for authorization in browser..."))
			fmt.Println()

			// Open browser in background
			openBrowser(grant.VerificationURI)

			pollInterval := time.Duration(grant.Interval) * time.Second
			if pollInterval < time.Second {
				pollInterval = 2 * time.Second
			}
			timeout := time.After(time.Duration(grant.ExpiresIn) * time.Second)
			ticker := time.NewTicker(pollInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					creds, err := auth.PollDeviceToken(endpoint, grant.DeviceCode)
					if err == nil && creds != nil {
						if err := auth.SaveCredentials(fileSystem, creds); err != nil {
							return fmt.Errorf("failed to save credentials: %w", err)
						}
						okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
						fmt.Println()
						fmt.Println(okStyle.Render(fmt.Sprintf("✓ Successfully authenticated as %s [Tier: %s]", creds.Email, strings.ToUpper(creds.Tier))))
						return nil
					}
				case <-timeout:
					return fmt.Errorf("authorization timed out. Please run 'ctrldown login' again")
				}
			}
		},
	}

	cmd.Flags().StringVar(&token, "token", "", "Ctrldown Cloudflare Edge API access token (headless/CI)")
	cmd.Flags().StringVar(&email, "email", "", "Account email address (when using --token)")
	cmd.Flags().StringVar(&tier, "tier", "", "Account license tier (free, pro, enterprise)")
	cmd.Flags().StringSliceVar(&entitlements, "entitlements", nil, "Explicit feature IDs unlocked for this token")
	cmd.Flags().StringVar(&endpoint, "endpoint", auth.DefaultEdgeEndpoint, "Cloudflare Edge API endpoint")

	return cmd
}

// NewLogoutCmd removes stored credentials from ~/.ctrldown/credentials.yaml.
func NewLogoutCmd(fileSystem fs.FS) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out from Ctrldown Edge and remove local credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.ClearCredentials(fileSystem); err != nil {
				return err
			}
			okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
			fmt.Println(okStyle.Render("✓ Logged out and removed ~/.ctrldown/credentials.yaml"))
			return nil
		},
	}
}

// NewWhoamiCmd displays the currently authenticated Ctrldown Edge user and entitlements.
func NewWhoamiCmd(fileSystem fs.FS) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Display the currently authenticated Ctrldown account and unlocked entitlements",
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, err := auth.LoadCredentials(fileSystem)
			if err != nil {
				warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700")).Bold(true)
				fmt.Println(warnStyle.Render("⚠ Not logged in (anonymous / free tier). Run 'ctrldown login' to authenticate."))
				return nil
			}

			titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
			labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)

			fmt.Println(titleStyle.Render("=== Ctrldown Authenticated Session ==="))
			fmt.Printf("  %-18s %s\n", labelStyle.Render("Account:"), creds.Email)
			fmt.Printf("  %-18s %s\n", labelStyle.Render("License Tier:"), strings.ToUpper(creds.Tier))
			fmt.Printf("  %-18s %s\n", labelStyle.Render("Edge Endpoint:"), creds.Endpoint)
			if len(creds.Entitlements) > 0 {
				fmt.Printf("  %-18s %s\n", labelStyle.Render("Entitlements:"), strings.Join(creds.Entitlements, ", "))
			}
			fmt.Printf("  %-18s %s\n", labelStyle.Render("Authenticated At:"), creds.AuthenticatedAt)
			return nil
		},
	}
}
