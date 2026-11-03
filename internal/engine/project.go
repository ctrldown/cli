package engine

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
	"ctrldown/cli/internal/config"
)

// ProjectOptions holds the configuration choices for scaffolding a monolithic project.
type ProjectOptions struct {
	Title                 string
	DestPath              string
	BoilerplatePath       string
	LibraryPath           string
	Database              string   // "postgres", "sqlite", or "none"
	Auth                  string   // "ory" or "none"
	FrontendDomains       []string // e.g., ["landing", "dashboard", "ctrlweb"]
	IncludeSharedUI       bool
	Features              []string // Optional features from ctrldown/features to pre-install
	WithCtrlwebExampleApp bool     // Optional minimal Next.js consumer app when ctrlweb is enabled
}

// BootstrapProject scaffolds an Etkilibagis-style monolithic monorepo (Turborepo + pnpm + Go Echo v4 backend).
func BootstrapProject(fs afero.Fs, opts ProjectOptions) (*config.CtrlConfig, error) {
	if opts.Title == "" {
		return nil, fmt.Errorf("project title is required")
	}
	if opts.DestPath == "" {
		opts.DestPath = "."
	}
	if opts.Database == "" {
		opts.Database = "postgres"
	}
	if opts.Auth == "" {
		opts.Auth = "none"
	}

	// 1. Create root monorepo configuration files
	if err := writeMonorepoRoot(fs, opts); err != nil {
		return nil, fmt.Errorf("failed to scaffold monorepo root: %w", err)
	}

	// 2. Scaffold Go Echo Backend in domains/api
	if err := writeGoEchoBackend(fs, opts); err != nil {
		return nil, fmt.Errorf("failed to scaffold Go Echo backend: %w", err)
	}

	// 3. Scaffold shared workspace packages (packages/config/* and optional packages/ui)
	packages, err := writeSharedPackages(fs, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to scaffold shared packages: %w", err)
	}

	// 4. Scaffold selected frontend domains (domains/landing, domains/dashboard, etc.)
	hasCtrlwebDomain := false
	domains := []config.DomainEntry{
		{
			Name: "api",
			Type: "go-echo",
			Path: "domains/api",
		},
	}
	for _, domainName := range opts.FrontendDomains {
		if domainName == "ctrlweb" {
			hasCtrlwebDomain = true
			continue
		}
		if err := writeFrontendDomain(fs, opts.DestPath, domainName, opts.IncludeSharedUI); err != nil {
			return nil, fmt.Errorf("failed to scaffold domain %s: %w", domainName, err)
		}
		domains = append(domains, config.DomainEntry{
			Name: domainName,
			Type: "nextjs",
			Path: filepath.ToSlash(filepath.Join("domains", domainName)),
		})
	}

	// 5. Scaffold environments/docker infrastructure
	if err := writeDockerEnvironment(fs, opts); err != nil {
		return nil, fmt.Errorf("failed to scaffold docker environment: %w", err)
	}

	// 6. Generate and save ctrl.yaml
	ctrlConf := &config.CtrlConfig{
		BoilerplatePath: opts.BoilerplatePath,
		LibraryPath:     opts.LibraryPath,
		Project: config.ProjectConfig{
			Variant:      "Project",
			Title:        opts.Title,
			Architecture: "monolith-turborepo-echo",
			CreatedAt:    time.Now().Format(time.RFC3339),
		},
		Backend: &config.BackendConfig{
			Framework: "echo",
			Path:      "domains/api",
			Module:    "api",
			Database:  opts.Database,
			Auth:      opts.Auth,
			Modules:   []string{"health"},
		},
		Domains:  domains,
		Packages: packages,
	}

	if err := config.SaveCtrlConfig(fs, opts.DestPath, ctrlConf); err != nil {
		return nil, fmt.Errorf("failed to write ctrl.yaml: %w", err)
	}

	// 7. If ctrlweb was selected as a frontend domain, run the full Ctrlweb integration pipeline
	if hasCtrlwebDomain && !containsString(opts.Features, "ctrlweb") {
		opts.Features = append(opts.Features, "ctrlweb")
	}

	// 8. Pre-install any requested features from the library
	for _, featName := range opts.Features {
		if featName == "" {
			continue
		}
		if err := IntegrateFeatureToMonolith(fs, IntegrateFeatureOptions{
			ProjectDir:     opts.DestPath,
			FeatureName:    featName,
			LibraryDir:     opts.LibraryPath,
			WithExampleApp: opts.WithCtrlwebExampleApp,
		}); err != nil {
			return nil, fmt.Errorf("failed to pre-install feature %q: %w", featName, err)
		}
	}

	if len(opts.Features) > 0 {
		if updated, err := config.LoadCtrlConfig(fs, opts.DestPath); err == nil {
			ctrlConf = updated
		}
	}

	return ctrlConf, nil
}

func writeMonorepoRoot(fs afero.Fs, opts ProjectOptions) error {
	if err := fs.MkdirAll(opts.DestPath, 0755); err != nil {
		return err
	}

	pkgJSON := fmt.Sprintf(`{
  "name": "%s",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "build": "turbo build",
    "dev": "turbo dev",
    "api": "cd domains/api && go run cmd/api/main.go",
    "docker:up": "docker compose --env-file environments/docker/.env -f environments/docker/compose.yaml up -d",
    "docker:down": "docker compose --env-file environments/docker/.env -f environments/docker/compose.yaml down",
    "lint": "turbo lint",
    "typecheck": "turbo typecheck"
  },
  "devDependencies": {
    "@workspace/eslint-config": "workspace:*",
    "@workspace/typescript-config": "workspace:*",
    "prettier": "^3.8.3",
    "turbo": "^2.9.18",
    "typescript": "^5.8.0"
  },
  "packageManager": "pnpm@10.33.4",
  "engines": {
    "node": ">=20"
  }
}
`, opts.Title)

	pnpmWorkspace := `packages:
  - "domains/*"
  - "!domains/api"
  - "packages/*"
  - "packages/config/*"
  - "packages/features/*"
`

	turboJSON := `{
  "$schema": "https://turbo.build/schema.json",
  "ui": "tui",
  "globalEnv": [
    "PORT",
    "DB_USER",
    "DB_PASSWORD",
    "DB_HOST",
    "DB_PORT",
    "DB_DATABASE",
    "DATABASE_URI"
  ],
  "tasks": {
    "build": {
      "dependsOn": ["^build"],
      "inputs": ["$TURBO_DEFAULT$", ".env*"],
      "outputs": [".next/**", "!.next/cache/**"]
    },
    "lint": {
      "dependsOn": ["^lint"]
    },
    "typecheck": {
      "dependsOn": ["^typecheck"]
    },
    "dev": {
      "cache": false,
      "persistent": true
    }
  }
}
`

	gitignore := `# Dependencies
node_modules
.pnp
.pnp.js

# Build & Turbo
.next/
out/
dist/
.turbo

# Environment
.env
.env*.local

# Go binaries
domains/api/bin/
domains/api/tmp/
*.exe
`

	files := map[string]string{
		"package.json":        pkgJSON,
		"pnpm-workspace.yaml": pnpmWorkspace,
		"turbo.json":          turboJSON,
		".gitignore":          gitignore,
	}

	for name, content := range files {
		if err := afero.WriteFile(fs, filepath.Join(opts.DestPath, name), []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

func writeGoEchoBackend(fs afero.Fs, opts ProjectOptions) error {
	apiRoot := filepath.Join(opts.DestPath, "domains", "api")
	dirs := []string{
		filepath.Join(apiRoot, "cmd", "api"),
		filepath.Join(apiRoot, "internal", "database"),
		filepath.Join(apiRoot, "internal", "server"),
		filepath.Join(apiRoot, "internal", "health"),
	}
	for _, d := range dirs {
		if err := fs.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	goMod := `module api

go 1.26.4

require (
	github.com/joho/godotenv v1.5.1
	github.com/labstack/echo/v4 v4.15.4
	gorm.io/driver/postgres v1.6.0
	gorm.io/gorm v1.31.2
)
`

	mainGo := `package main

import (
	"api/internal/database"
	"api/internal/server"
	"log"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	if err := database.Connect(); err != nil {
		log.Printf("warning: database connection skipped or failed: %v", err)
	}

	// CTRLDOWN_INJECT_MIGRATE

	// CTRLDOWN_INJECT_WORKERS

	e := server.New()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}

	e.Logger.Fatal(e.Start(":" + port))
}
`

	dbGo := `package database

import (
	"fmt"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

// Connect initializes the shared GORM database instance from environment variables.
func Connect() error {
	dsn := os.Getenv("DATABASE_URI")
	if dsn == "" {
		host := os.Getenv("DB_HOST")
		if host == "" {
			return nil
		}
		user := os.Getenv("DB_USER")
		pass := os.Getenv("DB_PASSWORD")
		name := os.Getenv("DB_DATABASE")
		port := os.Getenv("DB_PORT")
		if port == "" {
			port = "5432"
		}
		dsn = fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable", host, user, pass, name, port)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to connect database: %w", err)
	}

	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(50)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	DB = db
	return nil
}
`

	serverGo := `package server

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// New creates and configures the Echo HTTP server instance.
func New() *echo.Echo {
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	RegisterRoutes(e)

	return e
}
`

	routesGo := `package server

import (
	"api/internal/health"

	"github.com/labstack/echo/v4"
)

// RegisterRoutes wires all domain module handlers into the Echo instance.
func RegisterRoutes(e *echo.Echo) {
	healthRepo := health.NewRepository()
	healthService := health.NewService(healthRepo)
	healthHandler := health.NewHandler(healthService)

	e.GET("/health", healthHandler.Check)

	// CTRLDOWN_INJECT_ROUTES
}
`

	healthGo := `package health

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Status() map[string]string {
	return map[string]string{"status": "ok"}
}

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Check(c echo.Context) error {
	return c.JSON(http.StatusOK, h.service.Status())
}
`

	files := map[string]string{
		filepath.Join(apiRoot, "go.mod"):                        goMod,
		filepath.Join(apiRoot, "cmd", "api", "main.go"):         mainGo,
		filepath.Join(apiRoot, "internal", "database", "db.go"): dbGo,
		filepath.Join(apiRoot, "internal", "server", "server.go"): serverGo,
		filepath.Join(apiRoot, "internal", "server", "routes.go"): routesGo,
		filepath.Join(apiRoot, "internal", "health", "handler.go"): healthGo,
	}

	for path, content := range files {
		if err := afero.WriteFile(fs, path, []byte(content), 0644); err != nil {
			return err
		}
	}

	return nil
}

func writeSharedPackages(fs afero.Fs, opts ProjectOptions) ([]config.PackageEntry, error) {
	var entries []config.PackageEntry

	// Ensure packages/features directory exists
	if err := fs.MkdirAll(filepath.Join(opts.DestPath, "packages", "features"), 0755); err != nil {
		return nil, err
	}

	// 1. @workspace/typescript-config
	tsConfigDir := filepath.Join(opts.DestPath, "packages", "config", "typescript-config")
	if err := fs.MkdirAll(tsConfigDir, 0755); err != nil {
		return nil, err
	}
	tsPkgJSON := `{
  "name": "@workspace/typescript-config",
  "version": "0.1.0",
  "private": true,
  "files": ["base.json", "nextjs.json"]
}
`
	tsBaseJSON := `{
  "$schema": "https://json.schemastore.org/tsconfig",
  "compilerOptions": {
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "target": "ES2022",
    "moduleResolution": "bundler"
  }
}
`
	if err := afero.WriteFile(fs, filepath.Join(tsConfigDir, "package.json"), []byte(tsPkgJSON), 0644); err != nil {
		return nil, err
	}
	if err := afero.WriteFile(fs, filepath.Join(tsConfigDir, "base.json"), []byte(tsBaseJSON), 0644); err != nil {
		return nil, err
	}
	entries = append(entries, config.PackageEntry{
		Name: "@workspace/typescript-config",
		Path: "packages/config/typescript-config",
	})

	// 2. @workspace/eslint-config
	eslintDir := filepath.Join(opts.DestPath, "packages", "config", "eslint-config")
	if err := fs.MkdirAll(eslintDir, 0755); err != nil {
		return nil, err
	}
	eslintPkgJSON := `{
  "name": "@workspace/eslint-config",
  "version": "0.1.0",
  "private": true
}
`
	if err := afero.WriteFile(fs, filepath.Join(eslintDir, "package.json"), []byte(eslintPkgJSON), 0644); err != nil {
		return nil, err
	}
	entries = append(entries, config.PackageEntry{
		Name: "@workspace/eslint-config",
		Path: "packages/config/eslint-config",
	})

	// 3. Optional @workspace/ui
	if opts.IncludeSharedUI {
		uiDir := filepath.Join(opts.DestPath, "packages", "ui", "src")
		if err := fs.MkdirAll(uiDir, 0755); err != nil {
			return nil, err
		}
		uiPkgJSON := `{
  "name": "@workspace/ui",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "exports": {
    ".": "./src/index.ts"
  },
  "devDependencies": {
    "@workspace/typescript-config": "workspace:*"
  }
}
`
		if err := afero.WriteFile(fs, filepath.Join(opts.DestPath, "packages", "ui", "package.json"), []byte(uiPkgJSON), 0644); err != nil {
			return nil, err
		}
		if err := afero.WriteFile(fs, filepath.Join(uiDir, "index.ts"), []byte("export {};\n"), 0644); err != nil {
			return nil, err
		}
		entries = append(entries, config.PackageEntry{
			Name: "@workspace/ui",
			Path: "packages/ui",
		})
	}

	return entries, nil
}

func writeFrontendDomain(fs afero.Fs, destPath, domainName string, includeSharedUI bool) error {
	domainDir := filepath.Join(destPath, "domains", domainName)
	appDir := filepath.Join(domainDir, "src", "app")
	featuresDir := filepath.Join(domainDir, "src", "features")

	for _, d := range []string{appDir, featuresDir} {
		if err := fs.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	uiDep := ""
	if includeSharedUI {
		uiDep = `"@workspace/ui": "workspace:*",` + "\n    "
	}

	pkgJSON := fmt.Sprintf(`{
  "name": "%s",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "next lint"
  },
  "dependencies": {
    %s"next": "^16.2.0",
    "react": "^19.2.0",
    "react-dom": "^19.2.0"
  },
  "devDependencies": {
    "@workspace/typescript-config": "workspace:*",
    "typescript": "^5.8.0"
  }
}
`, domainName, uiDep)

	pageTSX := fmt.Sprintf(`export default function Page() {
  return (
    <main>
      <h1>%s domain</h1>
    </main>
  );
}
`, domainName)

	if err := afero.WriteFile(fs, filepath.Join(domainDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		return err
	}
	if err := afero.WriteFile(fs, filepath.Join(appDir, "page.tsx"), []byte(pageTSX), 0644); err != nil {
		return err
	}
	return nil
}

func writeDockerEnvironment(fs afero.Fs, opts ProjectOptions) error {
	dockerDir := filepath.Join(opts.DestPath, "environments", "docker")
	if err := fs.MkdirAll(dockerDir, 0755); err != nil {
		return err
	}

	composeYAML := fmt.Sprintf(`services:
  postgres:
    image: postgres:16-alpine
    container_name: %s_db
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${DB_USER:-postgres}
      POSTGRES_PASSWORD: ${DB_PASSWORD:-postgres}
      POSTGRES_DB: ${DB_DATABASE:-%s}
    ports:
      - "${DB_PORT:-5432}:5432"
`, opts.Title, opts.Title)

	envExample := fmt.Sprintf(`PORT=8085
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_DATABASE=%s
`, opts.Title)

	if err := afero.WriteFile(fs, filepath.Join(dockerDir, "compose.yaml"), []byte(composeYAML), 0644); err != nil {
		return err
	}
	if err := afero.WriteFile(fs, filepath.Join(dockerDir, ".env.example"), []byte(envExample), 0644); err != nil {
		return err
	}
	if err := afero.WriteFile(fs, filepath.Join(dockerDir, ".env"), []byte(envExample), 0644); err != nil {
		return err
	}
	return nil
}
