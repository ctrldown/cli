package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/dev/graph"
	"ctrldown/cli/internal/dev/supervisor"
	"ctrldown/cli/internal/dev/tui"
	"ctrldown/cli/internal/fs"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

// NewDevCmd creates the `ctrldown dev` command to spin up the entire development environment.
func NewDevCmd(fileSystem fs.FS) *cobra.Command {
	var (
		noDocker bool
		port     int
	)

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Start the local development fleet in a 3-column interactive bento TUI",
		Long: `Discovers all infrastructure, backend APIs, frontend applications, and package dependencies
from ctrl.yaml and runs them inside a coordinated supervisor with live 3-column bento terminal streaming.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := os.Getwd()
			if err != nil {
				return err
			}

			// 1. Locate ctrl.yaml in current or ancestor directories
			projectDir, err := findCtrlRoot(fileSystem, workDir)
			if err != nil {
				errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F43F5E")).Bold(true)
				fmt.Println(errStyle.Render("✖ Could not find 'ctrl.yaml' in current directory or any parent directories."))
				fmt.Println("  Run 'ctrldown create <project>' or 'ctrldown init' to initialize a project first.")
				return nil
			}

			// 2. Load ctrl.yaml
			ctrlCfg, err := config.LoadCtrlConfig(fileSystem, projectDir)
			if err != nil {
				return fmt.Errorf("failed to load project configuration: %w", err)
			}

			// 3. Auto-detect services across the 3 categories
			serviceConfigs := discoverServices(fileSystem, projectDir, ctrlCfg, noDocker)
			if len(serviceConfigs) == 0 {
				warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FCD34D")).Bold(true)
				fmt.Println(warnStyle.Render("⚠ No domains or services configured in ctrl.yaml."))
				fmt.Println("  Add domains to ctrl.yaml under the 'domains:' section.")
				return nil
			}

			// 4. Build Monorepo Package & AST Dependency Graph
			depGraph, _ := graph.BuildWorkspaceGraph(fileSystem, projectDir, ctrlCfg)

			// 5. Initialize Supervisor & Start Services
			sup := supervisor.NewSupervisor(serviceConfigs)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if err := sup.StartAll(ctx); err != nil {
				return fmt.Errorf("supervisor failed to start services: %w", err)
			}
			defer sup.StopAll()

			// 6. Launch 3-Column Bento TUI
			title := ctrlCfg.Project.Title
			if title == "" {
				title = filepath.Base(projectDir)
			}

			return tui.StartApp(sup, depGraph, title)
		},
	}

	cmd.Flags().BoolVar(&noDocker, "no-docker", false, "Skip starting Docker Compose infrastructure")
	cmd.Flags().IntVar(&port, "port", 0, "Override primary backend service listening port")

	return cmd
}

// findCtrlRoot walks up from startDir until ctrl.yaml is found.
func findCtrlRoot(fs afero.Fs, startDir string) (string, error) {
	curr := startDir
	for {
		target := filepath.Join(curr, "ctrl.yaml")
		exists, err := afero.Exists(fs, target)
		if err == nil && exists {
			return curr, nil
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return "", fmt.Errorf("ctrl.yaml not found")
}

// discoverServices inspects ctrl.yaml and the filesystem to configure all runnable services.
func discoverServices(fs afero.Fs, rootDir string, cfg *config.CtrlConfig, noDocker bool) []supervisor.ServiceConfig {
	var list []supervisor.ServiceConfig

	// 1. Column 1: Infrastructure (Docker Compose)
	if !noDocker {
		dockerFile := "docker-compose.yml"
		if exists, _ := afero.Exists(fs, filepath.Join(rootDir, "docker-compose.yaml")); exists {
			dockerFile = "docker-compose.yaml"
		}

		if exists, _ := afero.Exists(fs, filepath.Join(rootDir, dockerFile)); exists {
			list = append(list, supervisor.ServiceConfig{
				Name:        "docker-compose",
				Category:    supervisor.CategoryInfra,
				Dir:         rootDir,
				Command:     "docker compose up",
				AutoRestart: false,
			})
		} else if cfg.Backend != nil && cfg.Backend.Database != "" {
			// Mock/placeholder DB indicator if no docker-compose.yml
			list = append(list, supervisor.ServiceConfig{
				Name:        cfg.Backend.Database + "-db",
				Category:    supervisor.CategoryInfra,
				Dir:         rootDir,
				Command:     "echo " + cfg.Backend.Database + " container active",
				Port:        5432,
				AutoRestart: false,
			})
		}
	}

	// 2. Column 2: Backend APIs and Runtimes
	if cfg.Backend != nil && cfg.Backend.Path != "" {
		backendDir := filepath.Join(rootDir, cfg.Backend.Path)
		cmdStr := "go run ."
		if exists, _ := afero.Exists(fs, filepath.Join(backendDir, ".air.toml")); exists {
			cmdStr = "air"
		}

		list = append(list, supervisor.ServiceConfig{
			Name:        cfg.Backend.Path,
			Category:    supervisor.CategoryBackend,
			Dir:         backendDir,
			Command:     cmdStr,
			Port:        8080,
			AutoRestart: true,
		})
	}

	for _, d := range cfg.Domains {
		domainDir := filepath.Join(rootDir, d.Path)
		dType := strings.ToLower(d.Type)

		switch {
		case dType == "cloudflare-worker" || dType == "worker":
			list = append(list, supervisor.ServiceConfig{
				Name:        d.Name,
				Category:    supervisor.CategoryBackend,
				Dir:         domainDir,
				Command:     "pnpm run dev",
				Port:        8787,
				AutoRestart: true,
			})

		case dType == "go-echo-api" || dType == "api":
			list = append(list, supervisor.ServiceConfig{
				Name:        d.Name,
				Category:    supervisor.CategoryBackend,
				Dir:         domainDir,
				Command:     "go run .",
				Port:        8080,
				AutoRestart: true,
			})

		case dType == "cloudflare-pages" || dType == "pages" || dType == "vite":
			list = append(list, supervisor.ServiceConfig{
				Name:        d.Name,
				Category:    supervisor.CategoryFrontend,
				Dir:         domainDir,
				Command:     "pnpm run dev",
				Port:        5173,
				AutoRestart: true,
			})

		case dType == "nextjs-app" || dType == "nextjs" || dType == "frontend" || dType == "web":
			list = append(list, supervisor.ServiceConfig{
				Name:        d.Name,
				Category:    supervisor.CategoryFrontend,
				Dir:         domainDir,
				Command:     "pnpm run dev",
				Port:        3000,
				AutoRestart: true,
			})

		default:
			// Fallback: Check if package.json has a "dev" script
			pkgJSON := filepath.Join(domainDir, "package.json")
			if exists, _ := afero.Exists(fs, pkgJSON); exists {
				list = append(list, supervisor.ServiceConfig{
					Name:        d.Name,
					Category:    supervisor.CategoryFrontend,
					Dir:         domainDir,
					Command:     "pnpm run dev",
					Port:        3000,
					AutoRestart: true,
				})
			}
		}
	}

	return list
}
