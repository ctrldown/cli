package cli

import (
	"context"
	"encoding/json"
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

type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

func readRootScripts(fs afero.Fs, rootDir string) map[string]string {
	data, err := afero.ReadFile(fs, filepath.Join(rootDir, "package.json"))
	if err != nil {
		return nil
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	return pkg.Scripts
}

// discoverServices inspects ctrl.yaml and the filesystem to configure all runnable services.
func discoverServices(fs afero.Fs, rootDir string, cfg *config.CtrlConfig, noDocker bool) []supervisor.ServiceConfig {
	var list []supervisor.ServiceConfig
	rootScripts := readRootScripts(fs, rootDir)

	// 1. Column 1: Infrastructure (Docker Compose)
	if !noDocker {
		hasDockerUp := false
		var dockerUpCmd, dockerDownCmd string
		if rootScripts != nil {
			if _, ok := rootScripts["docker:up"]; ok {
				hasDockerUp = true
				dockerUpCmd = "pnpm run docker:up"
				if _, okDown := rootScripts["docker:down"]; okDown {
					dockerDownCmd = "pnpm run docker:down"
				} else {
					dockerDownCmd = "docker compose stop"
				}
			}
		}

		var composeRelPath string
		var envRelPath string

		candidates := []struct {
			compose string
			env     string
		}{
			{"environments/docker/compose.yaml", "environments/docker/.env"},
			{"environments/docker/compose.yml", "environments/docker/.env"},
			{"environments/docker/docker-compose.yml", "environments/docker/.env"},
			{"environments/docker/docker-compose.yaml", "environments/docker/.env"},
			{"docker-compose.yml", ".env"},
			{"docker-compose.yaml", ".env"},
			{"compose.yaml", ".env"},
			{"compose.yml", ".env"},
		}

		for _, c := range candidates {
			if exists, _ := afero.Exists(fs, filepath.Join(rootDir, c.compose)); exists {
				composeRelPath = c.compose
				if hasEnv, _ := afero.Exists(fs, filepath.Join(rootDir, c.env)); hasEnv {
					envRelPath = c.env
				}
				break
			}
		}

		// Detect DB ports (Postgres 5432, MySQL 3306, Redis 6379, Mongo 27017, Ory Kratos 4433)
		probePort := 5432
		if composeRelPath != "" {
			if content, err := afero.ReadFile(fs, filepath.Join(rootDir, composeRelPath)); err == nil {
				text := string(content)
				if strings.Contains(text, "5432") {
					probePort = 5432
				} else if strings.Contains(text, "3306") {
					probePort = 3306
				} else if strings.Contains(text, "6379") {
					probePort = 6379
				} else if strings.Contains(text, "27017") {
					probePort = 27017
				}
			}
		}

		if hasDockerUp {
			list = append(list, supervisor.ServiceConfig{
				Name:          "docker-infra",
				Category:      supervisor.CategoryInfra,
				Dir:           rootDir,
				Command:       dockerUpCmd,
				StopCommand:   dockerDownCmd,
				Port:          probePort,
				AutoRestart:   false,
				OneShotDaemon: true,
			})
		} else if composeRelPath != "" {
			upCmd := fmt.Sprintf("docker compose -f %s up -d", composeRelPath)
			stopCmd := fmt.Sprintf("docker compose -f %s stop", composeRelPath)
			if envRelPath != "" {
				upCmd = fmt.Sprintf("docker compose --env-file %s -f %s up -d", envRelPath, composeRelPath)
				stopCmd = fmt.Sprintf("docker compose --env-file %s -f %s down", envRelPath, composeRelPath)
			}

			list = append(list, supervisor.ServiceConfig{
				Name:          "docker-infra",
				Category:      supervisor.CategoryInfra,
				Dir:           rootDir,
				Command:       upCmd,
				StopCommand:   stopCmd,
				Port:          probePort,
				AutoRestart:   false,
				OneShotDaemon: true,
			})
		} else if cfg.Backend != nil && cfg.Backend.Database != "" {
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

	registeredPaths := make(map[string]bool)

	// Helper to resolve Go backend command and directory
	resolveGoCmd := func(subDir string) (cmd string, runDir string) {
		if rootScripts != nil {
			if _, ok := rootScripts["api"]; ok {
				return "pnpm run api", rootDir
			}
		}
		if exists, _ := afero.Exists(fs, filepath.Join(subDir, ".air.toml")); exists {
			return "air", subDir
		}
		if exists, _ := afero.Exists(fs, filepath.Join(subDir, "cmd", "api", "main.go")); exists {
			return "go run cmd/api/main.go", subDir
		}
		if exists, _ := afero.Exists(fs, filepath.Join(subDir, "cmd", "main.go")); exists {
			return "go run cmd/main.go", subDir
		}
		if exists, _ := afero.Exists(fs, filepath.Join(subDir, "main.go")); exists {
			return "go run main.go", subDir
		}
		return "go run .", subDir
	}

	// 2. Column 2: Backend APIs and Runtimes
	if cfg.Backend != nil && cfg.Backend.Path != "" {
		backendDir := filepath.Join(rootDir, cfg.Backend.Path)
		cmdStr, workDir := resolveGoCmd(backendDir)

		list = append(list, supervisor.ServiceConfig{
			Name:        cfg.Backend.Path,
			Category:    supervisor.CategoryBackend,
			Dir:         workDir,
			Command:     cmdStr,
			Port:        8080,
			AutoRestart: true,
		})
		registeredPaths[cfg.Backend.Path] = true
		registeredPaths[filepath.Clean(cfg.Backend.Path)] = true
		registeredPaths["api"] = true
	}

	frontendPort := 3000
	for _, d := range cfg.Domains {
		cleanPath := filepath.Clean(d.Path)
		if registeredPaths[d.Path] || registeredPaths[cleanPath] || (registeredPaths["api"] && d.Name == "api") {
			continue
		}
		registeredPaths[d.Path] = true
		registeredPaths[cleanPath] = true

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

		case dType == "go-echo-api" || dType == "go-echo" || dType == "echo" || dType == "api":
			cmdStr, workDir := resolveGoCmd(domainDir)
			list = append(list, supervisor.ServiceConfig{
				Name:        d.Name,
				Category:    supervisor.CategoryBackend,
				Dir:         workDir,
				Command:     cmdStr,
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
			curPort := frontendPort
			frontendPort++
			list = append(list, supervisor.ServiceConfig{
				Name:        d.Name,
				Category:    supervisor.CategoryFrontend,
				Dir:         domainDir,
				Command:     "pnpm run dev",
				Port:        curPort,
				AutoRestart: true,
			})

		default:
			// Fallback: Check if package.json has a "dev" script
			pkgJSON := filepath.Join(domainDir, "package.json")
			if exists, _ := afero.Exists(fs, pkgJSON); exists {
				curPort := frontendPort
				frontendPort++
				list = append(list, supervisor.ServiceConfig{
					Name:        d.Name,
					Category:    supervisor.CategoryFrontend,
					Dir:         domainDir,
					Command:     "pnpm run dev",
					Port:        curPort,
					AutoRestart: true,
				})
			}
		}
	}

	// Fallback if no frontend apps discovered but root has a "dev" script
	hasFrontend := false
	for _, s := range list {
		if s.Category == supervisor.CategoryFrontend {
			hasFrontend = true
			break
		}
	}
	if !hasFrontend && rootScripts != nil {
		if _, ok := rootScripts["dev"]; ok {
			list = append(list, supervisor.ServiceConfig{
				Name:        "frontend",
				Category:    supervisor.CategoryFrontend,
				Dir:         rootDir,
				Command:     "pnpm run dev",
				Port:        3000,
				AutoRestart: true,
			})
		}
	}

	return list
}
