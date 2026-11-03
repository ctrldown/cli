package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"ctrldown/cli/internal/config"
)

// CtrlwebIntegrationOptions configures how ctrlweb is integrated into a new or existing project.
type CtrlwebIntegrationOptions struct {
	ProjectDir         string
	LibraryDir         string
	WithBackendService bool   // Scaffolds domains/api/internal/ctrlweb and injects Echo routes
	WithBuilderDomain  bool   // Scaffolds domains/ctrlweb visual builder domain shell + @workspace/ui bridge
	WithExampleApp     bool   // Scaffolds an ultra-minimal Next.js 16 example consumer app
	ExampleAppName     string // Defaults to "ctrlweb-example"
}

// IntegrateCtrlweb wires the ctrlweb ecosystem (Go Echo delivery service, domains/ctrlweb builder shell,
// @workspace/ui component registry bridge, and optional ultra-minimal Next.js example consumer app)
// into a new or existing project without modifying any external source repositories.
func IntegrateCtrlweb(fs afero.Fs, opts CtrlwebIntegrationOptions) error {
	if opts.ProjectDir == "" {
		opts.ProjectDir = "."
	}
	if opts.ExampleAppName == "" {
		opts.ExampleAppName = "ctrlweb-example"
	}

	ctrlConf, err := config.LoadCtrlConfig(fs, opts.ProjectDir)
	if err != nil {
		ctrlConf, err = InspectProject(fs, opts.ProjectDir)
		if err != nil {
			return fmt.Errorf("failed to load or inspect project at %s: %w", opts.ProjectDir, err)
		}
	}

	apiPath := "domains/api"
	apiModule := "api"
	if ctrlConf.Backend != nil {
		if ctrlConf.Backend.Path != "" {
			apiPath = ctrlConf.Backend.Path
		}
		if ctrlConf.Backend.Module != "" {
			apiModule = ctrlConf.Backend.Module
		}
	}

	// 1. Ensure @workspace/ui exists and contains the composable ctrlweb registry bridge
	if err := ensureWorkspaceUICtrlwebBridge(fs, opts.ProjectDir, ctrlConf); err != nil {
		return fmt.Errorf("failed to configure @workspace/ui bridge for ctrlweb: %w", err)
	}

	// 2. Scaffold dedicated Go Echo backend delivery service (domains/api/internal/ctrlweb)
	if opts.WithBackendService {
		backendModDir := filepath.Join(opts.ProjectDir, filepath.FromSlash(apiPath), "internal", "ctrlweb")
		if err := writeCtrlwebEchoBackend(fs, backendModDir); err != nil {
			return fmt.Errorf("failed to write ctrlweb Go Echo backend service: %w", err)
		}

		// Inject route registration into domains/api/internal/server/routes.go via AST
		routesFile := filepath.Join(opts.ProjectDir, filepath.FromSlash(apiPath), "internal", "server", "routes.go")
		if hasFile(fs, routesFile) {
			importPath := fmt.Sprintf("%s/internal/ctrlweb", apiModule)
			routeStmt := `if ctrlwebHandler, err := ctrlweb.NewHandlerFromEnv(); err == nil { ctrlweb.RegisterRoutes(e, ctrlwebHandler) }`
			if err := InjectGoEchoRouteAST(fs, routesFile, importPath, routeStmt); err != nil {
				return fmt.Errorf("failed to inject ctrlweb routes into %s: %w", routesFile, err)
			}
		}

		if ctrlConf.Backend == nil {
			ctrlConf.Backend = &config.BackendConfig{
				Framework: "echo",
				Path:      apiPath,
				Module:    apiModule,
			}
		}
		if !containsString(ctrlConf.Backend.Modules, "ctrlweb") {
			ctrlConf.Backend.Modules = append(ctrlConf.Backend.Modules, "ctrlweb")
		}

		// Append CTRLWEB_DB_DATABASE env variable to environments/docker/.env.example if present
		appendCtrlwebDockerEnv(fs, opts.ProjectDir, ctrlConf.Project.Title)
	}

	// 3. Scaffold domains/ctrlweb external/monorepo builder domain with Ory Kratos/Keto guard & @workspace/ui bridge
	if opts.WithBuilderDomain {
		if err := writeCtrlwebBuilderDomain(fs, opts.ProjectDir); err != nil {
			return fmt.Errorf("failed to scaffold domains/ctrlweb builder app: %w", err)
		}
		registerDomainInConfig(ctrlConf, config.DomainEntry{
			Name: "ctrlweb",
			Type: "nextjs-visual-builder",
			Path: "domains/ctrlweb",
		})
	}

	// 4. Optional: Scaffold ultra-minimal Next.js 16 example consumer app
	if opts.WithExampleApp {
		if err := ScaffoldCtrlwebExampleApp(fs, opts.ProjectDir, opts.ExampleAppName); err != nil {
			return fmt.Errorf("failed to scaffold ctrlweb minimal Next.js example app: %w", err)
		}
		registerDomainInConfig(ctrlConf, config.DomainEntry{
			Name: opts.ExampleAppName,
			Type: "nextjs-ctrlweb-consumer",
			Path: filepath.ToSlash(filepath.Join("domains", opts.ExampleAppName)),
		})
	}

	if !containsString(ctrlConf.Features, "ctrlweb") {
		ctrlConf.Features = append(ctrlConf.Features, "ctrlweb")
	}

	var libCtrlwebDir string
	if opts.LibraryDir != "" {
		libCtrlwebDir = filepath.Join(opts.LibraryDir, "ctrlweb")
	}
	RecordFeatureLock(fs, ctrlConf, "ctrlweb", "1.0.0", libCtrlwebDir)

	return config.SaveCtrlConfig(fs, opts.ProjectDir, ctrlConf)
}

// SeedCtrlwebFeatureLibrary writes the standalone ctrlweb feature definition into ctrldown/features/ctrlweb
// so it appears in the Ctrldown Feature Library alongside bloub and auth.
func SeedCtrlwebFeatureLibrary(fs afero.Fs, libraryDir string) error {
	featureDir := filepath.Join(libraryDir, "ctrlweb")
	backendSliceDir := filepath.Join(featureDir, "slices", "backend-ctrlweb")
	builderSliceDir := filepath.Join(featureDir, "slices", "domain-ctrlweb")
	orySliceDir := filepath.Join(featureDir, "slices", "ory")

	for _, d := range []string{backendSliceDir, builderSliceDir, orySliceDir} {
		if err := fs.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	if err := writeCtrlwebEchoBackend(fs, backendSliceDir); err != nil {
		return err
	}
	if err := writeCtrlwebBuilderFiles(fs, builderSliceDir); err != nil {
		return err
	}

	ycodeRolesRTS := `Ycode:site#edit@Role:platform/site-manager#members
Ycode:site#edit@Role:platform/editor#members
Ycode:site#publish@Role:platform/site-manager#members
Ycode:site#publish@Role:platform/publisher#members
Ycode:site#structure@Role:platform/site-manager#members
Ycode:site#settings@Role:platform/site-manager#members
`
	if err := afero.WriteFile(fs, filepath.Join(orySliceDir, "ycode-roles.rts"), []byte(ycodeRolesRTS), 0644); err != nil {
		return err
	}

	manifest := `id = 'ctrlweb'
version = '1.0.0'
title = 'Ctrlweb Visual Builder & Delivery Pipeline'
description = 'Visual web builder domain, dedicated Go Echo read-only delivery service, @workspace/ui component registry bridge, and optional minimal Next.js consumer app'
category = 'fullstack-feature'
repository = 'git@github.com:ctrldown/ctrlweb.git'
required_tier = 'pro'

[slices]
backend-ctrlweb = 'slices/backend-ctrlweb'
domain-ctrlweb = 'slices/domain-ctrlweb'
ory = 'slices/ory'

[targets]
'slices/backend-ctrlweb' = 'domains/api/internal/ctrlweb'
'slices/domain-ctrlweb' = 'domains/ctrlweb'
'slices/ory' = 'domains/api/ory'

[[go_inject]]
module = 'ctrlweb'
import = 'api/internal/ctrlweb'
route = 'if ctrlwebHandler, err := ctrlweb.NewHandlerFromEnv(); err == nil { ctrlweb.RegisterRoutes(e, ctrlwebHandler) }'
`
	if err := afero.WriteFile(fs, filepath.Join(featureDir, "feature.toml"), []byte(manifest), 0644); err != nil {
		return err
	}

	readme := "# Ctrlweb Visual Builder & Delivery Pipeline (`ctrlweb`)\n\n" +
		"Adapts the Ctrlweb (YCode-based) visual canvas builder into any Ctrldown project with:\n" +
		"1. **Dedicated Go Echo Backend Service (`domains/api/internal/ctrlweb`)**: Serves published layer trees, reusable components, and assets from the Ctrlweb PostgreSQL database (`GET /api/v1/ctrlweb/pages/:slug` and `GET /api/v1/ctrlweb/pages/:folder/:slug`).\n" +
		"2. **`@workspace/ui` Component Bridge**: Declarative registry mapping `workspace*` canvas layers to shared React UI components.\n" +
		"3. **Visual Builder Domain (`domains/ctrlweb`)**: Configured with Ory Kratos/Keto capability guards (`Ycode:site#edit|publish|structure|settings`) and `@workspace/ui` transpilation.\n" +
		"4. **Optional Ultra-Minimal Next.js Example App (`--with-example-app`)**: A 5-file Next.js 16 consumer app that renders published Ctrlweb pages dynamically.\n\n" +
		"## Usage\n\n```bash\nctrldown integrate ctrlweb --with-example-app\n```\n"
	return afero.WriteFile(fs, filepath.Join(featureDir, "README.md"), []byte(readme), 0644)
}

func writeCtrlwebEchoBackend(fs afero.Fs, targetDir string) error {
	if err := fs.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	handlerGo := `package ctrlweb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"github.com/labstack/echo/v4"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var validSlugPattern = regexp.MustCompile(` + "`^[a-z0-9]+(?:-[a-z0-9]+)*$`" + `)

// Handler serves published Ctrlweb pages, reusable components, and assets from the Ctrlweb PostgreSQL database.
type Handler struct {
	db            *gorm.DB
	defaultFolder string
}

type publishedPageRow struct {
	ID     string          ` + "`gorm:\"column:id\"`" + `
	Name   string          ` + "`gorm:\"column:name\"`" + `
	Layers json.RawMessage ` + "`gorm:\"column:layers\"`" + `
}

type publishedComponentRow struct {
	ID        string          ` + "`gorm:\"column:id\" json:\"id\"`" + `
	Layers    json.RawMessage ` + "`gorm:\"column:layers\" json:\"layers\"`" + `
	Variants  json.RawMessage ` + "`gorm:\"column:variants\" json:\"variants,omitempty\"`" + `
	Variables json.RawMessage ` + "`gorm:\"column:variables\" json:\"variables,omitempty\"`" + `
}

type publishedAssetRow struct {
	ID        string  ` + "`gorm:\"column:id\" json:\"id\"`" + `
	PublicURL *string ` + "`gorm:\"column:public_url\" json:\"public_url,omitempty\"`" + `
	Content   *string ` + "`gorm:\"column:content\" json:\"content,omitempty\"`" + `
}

// NewHandler creates a new Ctrlweb delivery handler backed by a GORM DB connection.
func NewHandler(db *gorm.DB, defaultFolder string) *Handler {
	return &Handler{
		db:            db,
		defaultFolder: defaultFolder,
	}
}

// NewHandlerFromEnv opens a dedicated read-only connection to the Ctrlweb PostgreSQL database.
func NewHandlerFromEnv() (*Handler, error) {
	dsn := dsnFromEnv()
	if dsn == "" {
		return nil, fmt.Errorf("ctrlweb database is not configured")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect ctrlweb database: %w", err)
	}

	defaultFolder := os.Getenv("CTRLWEB_DEFAULT_FOLDER")
	return NewHandler(db, defaultFolder), nil
}

func dsnFromEnv() string {
	if uri := os.Getenv("CTRLWEB_DATABASE_URL"); uri != "" {
		return uri
	}
	if uri := os.Getenv("YCODE_DATABASE_URL"); uri != "" {
		return uri
	}
	host := os.Getenv("DB_HOST")
	if host == "" {
		return ""
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("DB_USER")
	if user == "" {
		user = "postgres"
	}
	pass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("CTRLWEB_DB_DATABASE")
	if dbName == "" {
		dbName = "ctrlweb_dev"
	}
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable", host, user, pass, dbName, port)
}

// RegisterRoutes mounts the published page endpoints onto the Echo router.
func RegisterRoutes(e *echo.Echo, h *Handler) {
	if h == nil {
		return
	}
	group := e.Group("/api/v1/ctrlweb")
	group.GET("/pages/:slug", h.PublishedRootOrDefaultPage)
	group.GET("/pages/:folder/:slug", h.PublishedFolderPage)
	group.GET("/:folder/:slug", h.PublishedFolderPage)
}

// PublishedRootOrDefaultPage serves a published page from CTRLWEB_DEFAULT_FOLDER (if set) or the root folder.
func (h *Handler) PublishedRootOrDefaultPage(c echo.Context) error {
	slug := c.Param("slug")
	return h.servePublishedPage(c, h.defaultFolder, slug)
}

// PublishedFolderPage serves a published page nested under a specific published folder slug.
func (h *Handler) PublishedFolderPage(c echo.Context) error {
	folder := c.Param("folder")
	slug := c.Param("slug")
	return h.servePublishedPage(c, folder, slug)
}

func (h *Handler) servePublishedPage(c echo.Context, folderSlug, pageSlug string) error {
	if h == nil || h.db == nil || !validSlugPattern.MatchString(pageSlug) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "page not found"})
	}
	if folderSlug != "" && !validSlugPattern.MatchString(folderSlug) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "folder not found"})
	}

	ctx := c.Request().Context()
	var row publishedPageRow
	var tx *gorm.DB

	if folderSlug == "" {
		tx = h.db.WithContext(ctx).Raw(` + "`" + `
			SELECT p.id, p.name, pl.layers
			FROM pages p
			JOIN page_layers pl ON pl.page_id = p.id
				AND pl.is_published = true AND pl.deleted_at IS NULL
			WHERE p.slug = ? AND p.page_folder_id IS NULL
				AND p.is_published = true AND p.deleted_at IS NULL
				AND p.is_dynamic = false
			LIMIT 1
		` + "`" + `, pageSlug).Scan(&row)
	} else {
		tx = h.db.WithContext(ctx).Raw(` + "`" + `
			SELECT p.id, p.name, pl.layers
			FROM pages p
			JOIN page_folders f ON f.id = p.page_folder_id
				AND f.is_published = true AND f.deleted_at IS NULL
				AND f.page_folder_id IS NULL AND f.slug = ?
			JOIN page_layers pl ON pl.page_id = p.id
				AND pl.is_published = true AND pl.deleted_at IS NULL
			WHERE p.slug = ? AND p.is_published = true AND p.deleted_at IS NULL
				AND p.is_dynamic = false
			LIMIT 1
		` + "`" + `, folderSlug, pageSlug).Scan(&row)
	}

	if tx.Error != nil || row.ID == "" {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "published page not found"})
	}

	var components []publishedComponentRow
	_ = h.db.WithContext(ctx).Raw(` + "`" + `
		SELECT id, layers, variants, variables
		FROM components
		WHERE is_published = true AND deleted_at IS NULL
	` + "`" + `).Scan(&components)

	var assets []publishedAssetRow
	_ = h.db.WithContext(ctx).Raw(` + "`" + `
		SELECT id, public_url, content
		FROM assets
		WHERE is_published = true AND deleted_at IS NULL
	` + "`" + `).Scan(&assets)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{
			"id":         row.ID,
			"folder":     folderSlug,
			"slug":       pageSlug,
			"title":      row.Name,
			"layers":     row.Layers,
			"components": components,
			"assets":     assets,
		},
	})
}
`
	return afero.WriteFile(fs, filepath.Join(targetDir, "handler.go"), []byte(handlerGo), 0644)
}

func ensureWorkspaceUICtrlwebBridge(fs afero.Fs, projectDir string, ctrlConf *config.CtrlConfig) error {
	uiSrcDir := filepath.Join(projectDir, "packages", "ui", "src")
	if err := fs.MkdirAll(uiSrcDir, 0755); err != nil {
		return err
	}

	pkgJSONPath := filepath.Join(projectDir, "packages", "ui", "package.json")
	if !hasFile(fs, pkgJSONPath) {
		uiPkgJSON := `{
  "name": "@workspace/ui",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "exports": {
    ".": "./src/index.ts",
    "./ctrlweb-registry": "./src/ctrlweb-registry.tsx"
  },
  "devDependencies": {
    "@workspace/typescript-config": "workspace:*"
  }
}
`
		if err := afero.WriteFile(fs, pkgJSONPath, []byte(uiPkgJSON), 0644); err != nil {
			return err
		}
	} else {
		// Ensure "./ctrlweb-registry" export is present in packages/ui/package.json
		mixinJSON := `{
  "exports": {
    "./ctrlweb-registry": "./src/ctrlweb-registry.tsx"
  }
}`
		tmpMixin := pkgJSONPath + ".ctrlweb.mixin"
		if err := afero.WriteFile(fs, tmpMixin, []byte(mixinJSON), 0644); err == nil {
			_ = MergeJSON(fs, pkgJSONPath, tmpMixin)
			_ = fs.Remove(tmpMixin)
		}
	}

	registryFile := filepath.Join(uiSrcDir, "ctrlweb-registry.tsx")
	if !hasFile(fs, registryFile) {
		registryCode := `import * as React from "react";

export interface CtrlwebCustomLayerProps {
  layer: {
    id: string;
    name: string;
    classes?: string | string[];
    settings?: Record<string, unknown>;
    variables?: Record<string, unknown>;
  };
  children?: React.ReactNode;
}

export function DefaultSectionHeading({ layer }: CtrlwebCustomLayerProps) {
  const settings = (layer.settings ?? {}) as Record<string, unknown>;
  const title = typeof settings.title === "string" ? settings.title : "Section Heading";
  const subtitle = typeof settings.subtitle === "string" ? settings.subtitle : "";

  return (
    <section className="py-12 px-6">
      <h2 className="text-3xl font-bold tracking-tight">{title}</h2>
      {subtitle ? <p className="mt-2 text-base opacity-80">{subtitle}</p> : null}
    </section>
  );
}

/**
 * Declarative registry mapping Ctrlweb custom "workspace*" canvas layer names
 * to shared @workspace/ui React components.
 */
export const workspaceCtrlwebRegistry: Record<string, React.ComponentType<CtrlwebCustomLayerProps>> = {
  workspaceSectionHeading: DefaultSectionHeading,
};
`
		if err := afero.WriteFile(fs, registryFile, []byte(registryCode), 0644); err != nil {
			return err
		}
	}

	registerPackageInConfig(ctrlConf, "@workspace/ui", "packages/ui")
	return nil
}

func writeCtrlwebBuilderDomain(fs afero.Fs, projectDir string) error {
	builderDir := filepath.Join(projectDir, "domains", "ctrlweb")
	return writeCtrlwebBuilderFiles(fs, builderDir)
}

func writeCtrlwebBuilderFiles(fs afero.Fs, builderDir string) error {
	dirs := []string{
		filepath.Join(builderDir, "lib"),
		filepath.Join(builderDir, "components", "workspace"),
		filepath.Join(builderDir, "app", "(builder)", "ctrlweb"),
	}
	for _, d := range dirs {
		if err := fs.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	pkgJSON := `{
  "name": "ctrlweb",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "next dev --turbopack -p 3003",
    "build": "next build",
    "start": "next start -p 3003",
    "lint": "next lint"
  },
  "dependencies": {
    "@workspace/ui": "workspace:*",
    "knex": "^3.1.0",
    "next": "^16.2.0",
    "pg": "^8.16.0",
    "react": "^19.2.0",
    "react-dom": "^19.2.0",
    "zustand": "^5.0.0"
  },
  "devDependencies": {
    "@workspace/typescript-config": "workspace:*",
    "typescript": "^5.8.0"
  }
}
`

	nextConfig := `import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  transpilePackages: ["@workspace/ui"],
};

export default nextConfig;
`

	oryPolicy := `export type YcodeRelation = "edit" | "publish" | "structure" | "settings" | "members";

/**
 * Maps Ctrlweb builder paths and HTTP methods to Ory Keto Ycode:site relations.
 */
export function requiredYcodeRelation(pathname: string, method: string): YcodeRelation {
  if (pathname.startsWith("/ctrlweb/api/publish")) {
    return "publish";
  }
  if (pathname.startsWith("/ctrlweb/api/settings") || pathname.startsWith("/ctrlweb/api/webhooks")) {
    return "settings";
  }
  if (pathname.startsWith("/ctrlweb/api/pages") || pathname.startsWith("/ctrlweb/api/components")) {
    return method === "GET" ? "edit" : "structure";
  }
  return "edit";
}
`

	oryEditorAccess := `import { requiredYcodeRelation, type YcodeRelation } from "./ycode-access-policy";

export interface EditorAccessResult {
  allowed: boolean;
  status: 200 | 401 | 403 | 503;
  identityId?: string;
  requiredRelation?: YcodeRelation;
}

export async function checkEditorAccess(
  cookieHeader: string | null,
  pathname: string,
  method: string
): Promise<EditorAccessResult> {
  const kratosUrl = process.env.ORY_KRATOS_PUBLIC_URL || "http://localhost:4433";
  const ketoReadUrl = process.env.ORY_KETO_READ_URL || "http://localhost:4466";

  if (!cookieHeader) {
    return { allowed: false, status: 401 };
  }

  try {
    const whoamiRes = await fetch(` + "`${kratosUrl}/sessions/whoami`" + `, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    });
    if (whoamiRes.status === 401) {
      return { allowed: false, status: 401 };
    }
    if (!whoamiRes.ok) {
      return { allowed: false, status: 503 };
    }

    const session = (await whoamiRes.json()) as { identity?: { id?: string } };
    const identityId = session.identity?.id;
    if (!identityId) {
      return { allowed: false, status: 401 };
    }

    const relation = requiredYcodeRelation(pathname, method);
    const params = new URLSearchParams({
      namespace: "Ycode",
      object: "site",
      relation,
      "subject_set.namespace": "User",
      "subject_set.object": identityId,
      "subject_set.relation": "",
    });

    const ketoRes = await fetch(` + "`${ketoReadUrl}/relation-tuples/check?${params.toString()}`" + `, {
      cache: "no-store",
    });
    if (!ketoRes.ok) {
      return { allowed: false, status: 503, identityId, requiredRelation: relation };
    }

    const body = (await ketoRes.json()) as { allowed?: boolean };
    return {
      allowed: Boolean(body.allowed),
      status: body.allowed ? 200 : 403,
      identityId,
      requiredRelation: relation,
    };
  } catch {
    return { allowed: false, status: 503 };
  }
}
`

	workspaceBridge := `import { workspaceCtrlwebRegistry } from "@workspace/ui/ctrlweb-registry";

export { workspaceCtrlwebRegistry };
`

	files := map[string]string{
		filepath.Join(builderDir, "package.json"):                          pkgJSON,
		filepath.Join(builderDir, "next.config.ts"):                        nextConfig,
		filepath.Join(builderDir, "lib", "ycode-access-policy.ts"):         oryPolicy,
		filepath.Join(builderDir, "lib", "ory-editor-access.ts"):           oryEditorAccess,
		filepath.Join(builderDir, "components", "workspace", "bridge.tsx"): workspaceBridge,
	}

	for path, content := range files {
		if err := afero.WriteFile(fs, path, []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

// ScaffoldCtrlwebExampleApp creates an ultra-minimal Next.js 16 consumer app (5 core files)
// that fetches published pages from the Go Echo ctrlweb API and renders them with @workspace/ui.
func ScaffoldCtrlwebExampleApp(fs afero.Fs, projectDir string, appName string) error {
	if appName == "" {
		appName = "ctrlweb-example"
	}
	appDir := filepath.Join(projectDir, "domains", appName)
	dirs := []string{
		filepath.Join(appDir, "src", "lib"),
		filepath.Join(appDir, "src", "components"),
		filepath.Join(appDir, "src", "app", "[[...slug]]"),
	}
	for _, d := range dirs {
		if err := fs.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	pkgJSON := fmt.Sprintf(`{
  "name": "%s",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "next dev --turbopack -p 3006",
    "build": "next build",
    "start": "next start -p 3006"
  },
  "dependencies": {
    "@workspace/ui": "workspace:*",
    "next": "^16.2.0",
    "react": "^19.2.0",
    "react-dom": "^19.2.0"
  },
  "devDependencies": {
    "@workspace/typescript-config": "workspace:*",
    "typescript": "^5.8.0"
  }
}
`, appName)

	tsConfig := `{
  "extends": "@workspace/typescript-config/base.json",
  "compilerOptions": {
    "jsx": "preserve",
    "plugins": [{ "name": "next" }],
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["next-env.d.ts", "**/*.ts", "**/*.tsx", ".next/types/**/*.ts"],
  "exclude": ["node_modules"]
}
`

	ctrlwebClientTS := `export interface CtrlwebLayer {
  id: string;
  name: string;
  tag?: string;
  classes?: string | string[];
  text?: string;
  attributes?: Record<string, string>;
  settings?: Record<string, unknown>;
  variables?: Record<string, unknown>;
  componentId?: string;
  children?: CtrlwebLayer[];
}

export interface CtrlwebComponent {
  id: string;
  layers: CtrlwebLayer[];
}

export interface CtrlwebAsset {
  id: string;
  public_url?: string;
  content?: string;
}

export interface CtrlwebPublishedPage {
  id: string;
  folder?: string;
  slug: string;
  title: string;
  layers: CtrlwebLayer[];
  components: CtrlwebComponent[];
  assets: CtrlwebAsset[];
}

const API_BASE = process.env.CTRLWEB_API_URL || process.env.NEXT_PUBLIC_API_URL || "http://localhost:8085";

export async function fetchPublishedCtrlwebPage(slugParts: string[]): Promise<CtrlwebPublishedPage | null> {
  const clean = slugParts.filter(Boolean);
  const endpoint =
    clean.length >= 2
      ? ` + "`${API_BASE}/api/v1/ctrlweb/pages/${encodeURIComponent(clean[0])}/${encodeURIComponent(clean[1])}`" + `
      : ` + "`${API_BASE}/api/v1/ctrlweb/pages/${encodeURIComponent(clean[0] || \"home\")}`" + `;

  try {
    const res = await fetch(endpoint, { cache: "no-store" });
    if (!res.ok) {
      return null;
    }
    const body = (await res.json()) as { data?: CtrlwebPublishedPage };
    return body.data ?? null;
  } catch {
    return null;
  }
}
`

	rendererTSX := `import * as React from "react";
import { workspaceCtrlwebRegistry } from "@workspace/ui/ctrlweb-registry";
import type { CtrlwebAsset, CtrlwebComponent, CtrlwebLayer } from "../lib/ctrlweb";

export interface CtrlwebRendererProps {
  layers: CtrlwebLayer[];
  components?: CtrlwebComponent[];
  assets?: CtrlwebAsset[];
}

function normalizeClasses(classes?: string | string[]): string {
  if (!classes) return "";
  return Array.isArray(classes) ? classes.join(" ") : classes;
}

export function CtrlwebRenderer({ layers, components = [] }: CtrlwebRendererProps) {
  const componentMap = React.useMemo(() => {
    const map = new Map<string, CtrlwebComponent>();
    for (const comp of components) {
      map.set(comp.id, comp);
    }
    return map;
  }, [components]);

  const renderLayer = (layer: CtrlwebLayer, seenComponents: Set<string>): React.ReactNode => {
    if (layer.name === "htmlEmbed" || layer.tag === "script") {
      return null;
    }

    // 1. Expand reusable Ctrlweb component instance with cycle guard
    if (layer.componentId && !seenComponents.has(layer.componentId)) {
      const comp = componentMap.get(layer.componentId);
      if (comp) {
        const nextSeen = new Set(seenComponents);
        nextSeen.add(layer.componentId);
        return (
          <React.Fragment key={layer.id}>
            {comp.layers.map((child) => renderLayer(child, nextSeen))}
          </React.Fragment>
        );
      }
    }

    // 2. Delegate custom workspace* layers to @workspace/ui registry
    const CustomComponent = workspaceCtrlwebRegistry[layer.name];
    if (CustomComponent) {
      return (
        <CustomComponent key={layer.id} layer={layer}>
          {layer.children?.map((child) => renderLayer(child, seenComponents))}
        </CustomComponent>
      );
    }

    // 3. Standard semantic HTML / Tailwind layer rendering
    const Tag = (layer.tag || "div") as React.ElementType;
    const className = normalizeClasses(layer.classes);

    return (
      <Tag key={layer.id} className={className || undefined} {...(layer.attributes ?? {})}>
        {layer.text}
        {layer.children?.map((child) => renderLayer(child, seenComponents))}
      </Tag>
    );
  };

  return <>{layers.map((layer) => renderLayer(layer, new Set()))}</>;
}
`

	layoutTSX := `import * as React from "react";

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
`

	pageTSX := `import { notFound } from "next/navigation";
import { fetchPublishedCtrlwebPage } from "../../lib/ctrlweb";
import { CtrlwebRenderer } from "../../components/CtrlwebRenderer";

interface PageProps {
  params: Promise<{ slug?: string[] }>;
}

export default async function CtrlwebCatchAllPage({ params }: PageProps) {
  const { slug = ["home"] } = await params;
  const page = await fetchPublishedCtrlwebPage(slug);

  if (!page) {
    notFound();
  }

  return (
    <main>
      <CtrlwebRenderer
        layers={page.layers}
        components={page.components}
        assets={page.assets}
      />
    </main>
  );
}
`

	files := map[string]string{
		filepath.Join(appDir, "package.json"):                             pkgJSON,
		filepath.Join(appDir, "tsconfig.json"):                            tsConfig,
		filepath.Join(appDir, "src", "lib", "ctrlweb.ts"):                 ctrlwebClientTS,
		filepath.Join(appDir, "src", "components", "CtrlwebRenderer.tsx"): rendererTSX,
		filepath.Join(appDir, "src", "app", "layout.tsx"):                 layoutTSX,
		filepath.Join(appDir, "src", "app", "[[...slug]]", "page.tsx"):    pageTSX,
	}

	for path, content := range files {
		if err := afero.WriteFile(fs, path, []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

func appendCtrlwebDockerEnv(fs afero.Fs, projectDir string, projectTitle string) {
	if projectTitle == "" {
		projectTitle = "ctrldown"
	}
	envFiles := []string{
		filepath.Join(projectDir, "environments", "docker", ".env.example"),
		filepath.Join(projectDir, "environments", "docker", ".env"),
	}
	for _, envPath := range envFiles {
		if !hasFile(fs, envPath) {
			continue
		}
		data, err := afero.ReadFile(fs, envPath)
		if err != nil || strings.Contains(string(data), "CTRLWEB_DB_DATABASE") {
			continue
		}
		addition := fmt.Sprintf("\n# Ctrlweb Visual Builder Database\nCTRLWEB_DB_DATABASE=%s_ctrlweb_dev\n", projectTitle)
		_ = afero.WriteFile(fs, envPath, append(data, []byte(addition)...), 0644)
	}
}

func registerDomainInConfig(ctrlConf *config.CtrlConfig, entry config.DomainEntry) {
	for _, d := range ctrlConf.Domains {
		if d.Name == entry.Name || d.Path == entry.Path {
			return
		}
	}
	ctrlConf.Domains = append(ctrlConf.Domains, entry)
}
