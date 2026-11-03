package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/auth"
	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/engine"
	"ctrldown/cli/internal/fs"
)

// NewListCmd creates the list/catalog command to inspect available Microfeatures, Templates, and Mixins.
func NewListCmd(fileSystem fs.FS) *cobra.Command {
	var (
		libraryPath     string
		boilerplatePath string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"catalog", "ls"},
		Short:   "List available Microfeatures (with lock/entitlement status), Templates, and Mixins",
		Long:    `Displays all independent Microfeatures (with versions, topologies, repositories, and Cloudflare Edge lock/unlock entitlement status) as well as Templates and Mixins from the Boilerplate Registry.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedLib := resolveDefaultLibraryPath(fileSystem, libraryPath)
			resolvedBp := resolveDefaultBoilerplatePath(fileSystem, boilerplatePath)
			creds, _ := auth.LoadCredentials(fileSystem)

			titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
			itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
			lockedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87")).Bold(true)
			unlockedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
			mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

			sessionSummary := "Anonymous (Free Tier)"
			if creds != nil && creds.Email != "" {
				sessionSummary = fmt.Sprintf("%s (%s Tier)", creds.Email, strings.ToUpper(creds.Tier))
			}

			// 1. Microfeatures Catalog
			fmt.Println(titleStyle.Render(fmt.Sprintf("=== Ctrldown Microfeature Catalog [%s] ===", sessionSummary)))
			features, err := engine.DiscoverLibraryFeatures(fileSystem, resolvedLib)
			if err != nil || len(features) == 0 {
				fmt.Println(mutedStyle.Render("  No microfeatures found in local cache (use 'ctrldown config sync' or 'ctrldown config set library_path <dir>')."))
			} else {
				for _, f := range features {
					ver := f.Version
					if ver == "" {
						ver = "0.1.0"
					}
					tier := f.RequiredTier
					if tier == "" {
						tier = "free"
					}

					entitled := auth.IsEntitled(creds, f.ID, tier)
					var statusBadge string
					var nameRendered string
					if entitled {
						statusBadge = unlockedStyle.Render(fmt.Sprintf("🔓 Unlocked (%s)", strings.ToUpper(tier)))
						nameRendered = itemStyle.Render(f.ID)
					} else {
						statusBadge = lockedStyle.Render(fmt.Sprintf("🔒 Locked (Requires %s)", strings.ToUpper(tier)))
						nameRendered = lockedStyle.Render(f.ID)
					}

					fmt.Printf("  • %-14s v%-7s [%-17s] %-28s %s\n",
						nameRendered,
						ver,
						f.Category,
						statusBadge,
						f.Title,
					)
					if f.Description != "" {
						fmt.Printf("      %s\n", mutedStyle.Render(f.Description))
					}
					if f.Repository != "" {
						fmt.Printf("      %s %s\n", mutedStyle.Render("Repo:"), mutedStyle.Render(f.Repository))
					}
				}
			}

			// 2. Boilerplate Templates & Mixins
			fmt.Println()
			fmt.Println(titleStyle.Render(fmt.Sprintf("=== Boilerplate Registry (%s) ===", resolvedBp)))
			bpConf, bpErr := config.LoadBoilerplateConfig(fileSystem, resolvedBp)
			if bpErr != nil {
				fmt.Println(mutedStyle.Render("  No boilerplate registry found (use 'ctrldown config set boilerplate_path <dir>' or 'ctrldown config sync')."))
				return nil
			}

			var categories []string
			for cat := range bpConf.Templates {
				categories = append(categories, cat)
			}
			sort.Strings(categories)

			if len(categories) > 0 {
				fmt.Println(lipgloss.NewStyle().Bold(true).Render("  Templates:"))
				for _, cat := range categories {
					tplMap := bpConf.Templates[cat]
					var ids []string
					for id := range tplMap {
						ids = append(ids, id)
					}
					sort.Strings(ids)
					for _, id := range ids {
						tpl := tplMap[id]
						fmt.Printf("    • %-22s [%-10s / %-10s] %s\n",
							itemStyle.Render(tpl.ID),
							cat,
							tpl.Language,
							tpl.Title,
						)
					}
				}
			}

			var langs []string
			for lang := range bpConf.Mixins {
				langs = append(langs, lang)
			}
			sort.Strings(langs)

			if len(langs) > 0 {
				fmt.Println(lipgloss.NewStyle().Bold(true).Render("\n  Mixins:"))
				for _, lang := range langs {
					mixinMap := bpConf.Mixins[lang]
					var ids []string
					for id := range mixinMap {
						ids = append(ids, id)
					}
					sort.Strings(ids)
					for _, id := range ids {
						m := mixinMap[id]
						fmt.Printf("    • %-22s [%-10s] %s\n",
							itemStyle.Render(m.ID),
							lang,
							m.Title,
						)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&libraryPath, "library", "", "Optional Ctrldown microfeatures parent directory")
	cmd.Flags().StringVar(&boilerplatePath, "boilerplate", "", "Optional Ctrldown boilerplates path")

	return cmd
}
