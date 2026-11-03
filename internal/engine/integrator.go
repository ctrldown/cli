package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/afero"
	"ctrldown/cli/internal/config"
)

// FeatureManifest represents the feature.toml configuration inside a Ctrldown microfeature repository.
type FeatureManifest struct {
	ID                string                 `toml:"id" json:"id"`
	Version           string                 `toml:"version,omitempty" json:"version,omitempty"`
	Title             string                 `toml:"title" json:"title"`
	Description       string                 `toml:"description" json:"description"`
	Category          string                 `toml:"category" json:"category"`
	Repository        string                 `toml:"repository,omitempty" json:"repository,omitempty"`
	RequiredTier      string                 `toml:"required_tier,omitempty" json:"required_tier,omitempty"`
	Locked            bool                   `toml:"-" json:"locked,omitempty"`
	Requires          []string               `toml:"requires,omitempty" json:"requires,omitempty"`
	Slices            map[string]string      `toml:"slices,omitempty" json:"slices,omitempty"`
	Targets           map[string]string      `toml:"targets,omitempty" json:"targets,omitempty"`
	GoInject          []GoInjectRule         `toml:"go_inject,omitempty" json:"go_inject,omitempty"`
	WorkspacePackages []WorkspacePackageRule `toml:"workspace_packages,omitempty" json:"workspace_packages,omitempty"`
	Worker            *WorkerRule            `toml:"worker,omitempty" json:"worker,omitempty"`
	Path              string                 `toml:"-" json:"-"`
}

// WorkerRule defines how a backend-worker microfeature is wired (either "embedded" in domains/api or "standalone" in domains/<id>).
type WorkerRule struct {
	Mode       string `toml:"mode" json:"mode"`               // "embedded" or "standalone"
	Entrypoint string `toml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Schedule   string `toml:"schedule,omitempty" json:"schedule,omitempty"`
}

// GoInjectRule defines AST wiring into a monolithic Go Echo backend.
type GoInjectRule struct {
	Module       string   `toml:"module" json:"module"`
	Import       string   `toml:"import" json:"import"`
	ExtraImports []string `toml:"extra_imports,omitempty" json:"extra_imports,omitempty"`
	Migrate      string   `toml:"migrate,omitempty" json:"migrate,omitempty"`
	Route        string   `toml:"route,omitempty" json:"route,omitempty"`
}

// WorkspacePackageRule defines a monorepo package registered by a feature.
type WorkspacePackageRule struct {
	Name        string   `toml:"name" json:"name"`
	Path        string   `toml:"path" json:"path"`
	LinkDomains []string `toml:"link_domains,omitempty" json:"link_domains,omitempty"`
}

// ExtractOptions defines how a package or full-stack feature is extracted from a monolith.
type ExtractOptions struct {
	ProjectDir        string
	FeatureName       string
	Title             string
	Description       string
	Category          string                 // "package", "backend-module", or "fullstack-feature"
	SlicePaths        map[string]string      // role -> relative path in project
	TargetPaths       map[string]string      // optional role -> target relative path in monolith
	Requires          []string               // optional feature dependencies
	GoInject          []GoInjectRule         // optional Go Echo AST wiring rules
	WorkspacePackages []WorkspacePackageRule // optional workspace packages to register
	Debrand           bool                   // replace hardcoded brand strings with configurable defaults
	OutputDir         string
}

// IntegrateFeatureOptions defines how a feature or Echo module is integrated into a monolithic project.
type IntegrateFeatureOptions struct {
	ProjectDir      string
	FeatureName     string
	SourceDir       string // Optional path to an extracted feature repository/directory
	LibraryDir      string // Optional path to the ctrldown/features library directory
	ScaffoldEchoMod bool   // If true and SourceDir is empty, scaffolds a new Go Echo module
	WithAutoMigrate bool   // Whether the Go Echo module includes GORM AutoMigrate
	WithExampleApp  bool   // When integrating ctrlweb, optionally scaffolds a minimal Next.js consumer app
	ExampleAppName  string // Name of the minimal Next.js example domain (default: "ctrlweb-example")
	TargetDomain    string // Optional frontend domain (e.g. "dashboard" or "landing") to link workspace package
}

// LoadFeatureManifest reads and parses feature.toml from a feature directory.
func LoadFeatureManifest(fs afero.Fs, featureDir string) (*FeatureManifest, error) {
	manifestPath := filepath.Join(featureDir, "feature.toml")
	data, err := afero.ReadFile(fs, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", manifestPath, err)
	}

	var manifest FeatureManifest
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", manifestPath, err)
	}
	manifest.Path = featureDir
	return &manifest, nil
}

// DiscoverLibraryFeatures scans a library directory (e.g. ctrldown/features) for feature.toml manifests.
func DiscoverLibraryFeatures(fs afero.Fs, libraryDir string) ([]FeatureManifest, error) {
	if !dirExists(fs, libraryDir) {
		return nil, nil
	}

	entries, err := afero.ReadDir(fs, libraryDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read feature library %s: %w", libraryDir, err)
	}

	var features []FeatureManifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fDir := filepath.Join(libraryDir, entry.Name())
		if !hasFile(fs, filepath.Join(fDir, "feature.toml")) {
			continue
		}
		m, err := LoadFeatureManifest(fs, fDir)
		if err != nil {
			continue
		}
		if m.ID == "" {
			m.ID = entry.Name()
		}
		features = append(features, *m)
	}

	sort.Slice(features, func(i, j int) bool {
		return features[i].ID < features[j].ID
	})
	return features, nil
}

// BuildCompositeExtractOptions returns a rich multi-slice extraction recipe for known composite features
// like "bloub" and "auth" (Ory Kratos/Keto + Identity + Dashboard Auth/Access) when present in projectDir.
func BuildCompositeExtractOptions(fs afero.Fs, projectDir string, featureName string, outputDir string) (ExtractOptions, bool) {
	switch strings.ToLower(featureName) {
	case "bloub", "@workspace/bloub":
		if !dirExists(fs, filepath.Join(projectDir, "packages", "bloub")) {
			return ExtractOptions{}, false
		}
		return ExtractOptions{
			ProjectDir:  projectDir,
			FeatureName: "bloub",
			Title:       "Bloub Interactive SVG Avatar & Studio",
			Description: "Zero-dependency pure-SVG animated bot avatar, gaze tracker, and animation studio library",
			Category:    "package",
			SlicePaths: map[string]string{
				"package": "packages/bloub",
			},
			TargetPaths: map[string]string{
				"package": "packages/bloub",
			},
			WorkspacePackages: []WorkspacePackageRule{
				{
					Name:        "@workspace/bloub",
					Path:        "packages/bloub",
					LinkDomains: []string{"dashboard"},
				},
			},
			Debrand:   true,
			OutputDir: outputDir,
		}, true

	case "auth", "identity", "ory-auth":
		if !dirExists(fs, filepath.Join(projectDir, "domains", "api", "internal", "auth")) {
			return ExtractOptions{}, false
		}
		slices := map[string]string{
			"backend-auth": "domains/api/internal/auth",
		}
		targets := map[string]string{
			"backend-auth": "domains/api/internal/auth",
		}

		optionalMappings := []struct {
			role string
			rel  string
		}{
			{"backend-identity", "domains/api/internal/identity"},
			{"ory", "domains/api/ory"},
			{"docker-ory", "environments/docker/ory"},
			{"dashboard-components-auth", "domains/dashboard/src/components/auth"},
			{"dashboard-lib-ory", "domains/dashboard/src/lib/ory-access.ts"},
			{"dashboard-features-access", "domains/dashboard/src/features/access"},
			{"dashboard-app-auth", "domains/dashboard/src/app/auth"},
			{"dashboard-app-access", "domains/dashboard/src/app/dashboard/access"},
			{"dashboard-api-access", "domains/dashboard/src/app/api/access"},
		}
		for _, m := range optionalMappings {
			full := filepath.Join(projectDir, filepath.FromSlash(m.rel))
			if dirExists(fs, full) || hasFile(fs, full) {
				slices[m.role] = m.rel
				targets[m.role] = m.rel
			}
		}

		return ExtractOptions{
			ProjectDir:  projectDir,
			FeatureName: "auth",
			Title:       "Ory Kratos & Keto Full-Stack Auth & RBAC",
			Description: "Full-stack Ory Kratos session auth, Keto Zanzibar RBAC, SMTP user invitations, and Next.js Dashboard auth/access UI",
			Category:    "fullstack-feature",
			Requires:    []string{"bloub"},
			SlicePaths:  slices,
			TargetPaths: targets,
			GoInject: []GoInjectRule{
				{
					Module: "auth",
					Import: "api/internal/auth",
				},
				{
					Module:       "identity",
					Import:       "api/internal/identity",
					ExtraImports: []string{"api/internal/auth"},
					Migrate:      "_ = identity.AutoMigrate(database.DB)",
					Route:        "identity.RegisterRoutes(e, identity.NewHandler(identity.NewClientFromEnv(), auth.NewCheckerFromEnv(), identity.NewKetoClientFromEnv(), identity.NewGORMInvitationRepository(database.DB)).WithNotifications(database.DB))",
				},
			},
			Debrand:   true,
			OutputDir: outputDir,
		}, true
	}

	return ExtractOptions{}, false
}

// InspectProject analyzes an existing directory (such as Etkilibagis) and builds a CtrlConfig representation.
func InspectProject(fs afero.Fs, projectDir string) (*config.CtrlConfig, error) {
	exists, err := afero.DirExists(fs, projectDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check project directory: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("project directory does not exist: %s", projectDir)
	}

	title := detectProjectTitle(fs, projectDir)
	architecture := "standalone"

	if hasFile(fs, filepath.Join(projectDir, "turbo.json")) || hasFile(fs, filepath.Join(projectDir, "pnpm-workspace.yaml")) {
		architecture = "monolith-turborepo"
	}

	domains, backendConf, err := discoverDomains(fs, projectDir)
	if err != nil {
		return nil, err
	}
	if backendConf != nil && backendConf.Framework == "echo" && architecture == "monolith-turborepo" {
		architecture = "monolith-turborepo-echo"
	}

	packages, err := discoverPackages(fs, projectDir)
	if err != nil {
		return nil, err
	}

	conf := &config.CtrlConfig{
		Project: config.ProjectConfig{
			Variant:      "Project",
			Title:        title,
			Architecture: architecture,
			CreatedAt:    time.Now().Format(time.RFC3339),
		},
		Backend:  backendConf,
		Domains:  domains,
		Packages: packages,
	}

	return conf, nil
}

// AdoptProject inspects an existing project and writes its ctrl.yaml configuration file.
func AdoptProject(fs afero.Fs, projectDir string, boilerplatePath string) (*config.CtrlConfig, error) {
	conf, err := InspectProject(fs, projectDir)
	if err != nil {
		return nil, err
	}
	conf.BoilerplatePath = boilerplatePath

	if err := config.SaveCtrlConfig(fs, projectDir, conf); err != nil {
		return nil, err
	}
	return conf, nil
}

// ExtractFeature copies modular slices (package, backend module, dashboard feature) out of a monolith
// and writes a declarative feature.toml manifest and README.md for the Ctrldown library.
func ExtractFeature(fs afero.Fs, opts ExtractOptions) error {
	if opts.FeatureName == "" {
		return fmt.Errorf("feature name is required")
	}
	if len(opts.SlicePaths) == 0 {
		return fmt.Errorf("at least one source slice path is required")
	}
	if opts.Title == "" {
		opts.Title = opts.FeatureName
	}

	if err := fs.MkdirAll(opts.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", opts.OutputDir, err)
	}

	roles := make([]string, 0, len(opts.SlicePaths))
	for role := range opts.SlicePaths {
		roles = append(roles, role)
	}
	sort.Strings(roles)

	slicesMap := make(map[string]string, len(roles))
	targetsMap := make(map[string]string, len(roles))

	for _, role := range roles {
		relSrc := opts.SlicePaths[role]
		srcPath := filepath.Join(opts.ProjectDir, filepath.FromSlash(relSrc))
		sliceRel := filepath.ToSlash(filepath.Join("slices", role))
		dstSubdir := filepath.Join(opts.OutputDir, "slices", role)

		if err := copyFileOrDir(fs, srcPath, dstSubdir); err != nil {
			return fmt.Errorf("failed to copy %s slice from %s: %w", role, relSrc, err)
		}
		slicesMap[role] = sliceRel

		if targetRel, ok := opts.TargetPaths[role]; ok && targetRel != "" {
			targetsMap[sliceRel] = targetRel
		} else {
			targetsMap[sliceRel] = relSrc
		}
	}

	if opts.Debrand {
		_ = debrandExtractedFeature(fs, opts.OutputDir)
	}

	wsPackages := opts.WorkspacePackages
	if (opts.Category == "package" || opts.Category == "frontend-package") && len(wsPackages) == 0 {
		if targetRel, ok := targetsMap["slices/package"]; ok {
			wsPackages = []WorkspacePackageRule{
				{
					Name: "@workspace/" + opts.FeatureName,
					Path: filepath.ToSlash(targetRel),
				},
			}
		}
	}

	manifest := FeatureManifest{
		ID:                opts.FeatureName,
		Version:           "1.0.0",
		Title:             opts.Title,
		Description:       opts.Description,
		Category:          opts.Category,
		Repository:        fmt.Sprintf("git@github.com:ctrldown/%s.git", opts.FeatureName),
		RequiredTier:      "free",
		Requires:          opts.Requires,
		Slices:            slicesMap,
		Targets:           targetsMap,
		GoInject:          opts.GoInject,
		WorkspacePackages: wsPackages,
	}

	tomlBytes, err := toml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal feature.toml: %w", err)
	}

	manifestPath := filepath.Join(opts.OutputDir, "feature.toml")
	if err := afero.WriteFile(fs, manifestPath, tomlBytes, 0644); err != nil {
		return fmt.Errorf("failed to write feature.toml: %w", err)
	}

	readmePath := filepath.Join(opts.OutputDir, "README.md")
	if !hasFile(fs, readmePath) {
		readmeContent := fmt.Sprintf("# %s (`%s`)\n\n%s\n\n## Integration\n\n```bash\nctrldown integrate feature --name %s\n```\n",
			opts.Title, opts.FeatureName, opts.Description, opts.FeatureName)
		_ = afero.WriteFile(fs, readmePath, []byte(readmeContent), 0644)
	}

	return nil
}

func debrandExtractedFeature(fs afero.Fs, featureDir string) error {
	return afero.Walk(fs, featureDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		name := info.Name()
		if name == "invite_mailer.go" {
			data, err := afero.ReadFile(fs, path)
			if err != nil {
				return nil
			}
			content := string(data)
			content = strings.ReplaceAll(content, `"Etkili Bağış yönetim daveti"`, `getInviteSubject()`)
			content = strings.ReplaceAll(content, `"Etkili Bağış yönetim paneline davet edildiniz."`, `getInviteHeading()`)
			if !strings.Contains(content, "func getInviteSubject()") {
				helperCode := `
func getInviteAppName() string {
	if name := strings.TrimSpace(os.Getenv("DASHBOARD_APP_NAME")); name != "" {
		return name
	}
	return "Ctrldown"
}

func getInviteSubject() string {
	return getInviteAppName() + " yönetim daveti"
}

func getInviteHeading() string {
	return getInviteAppName() + " yönetim paneline davet edildiniz."
}
`
				content += helperCode
			}
			_ = afero.WriteFile(fs, path, []byte(content), 0644)
		} else if name == "invite_test.go" {
			data, err := afero.ReadFile(fs, path)
			if err != nil {
				return nil
			}
			content := strings.ReplaceAll(string(data), "Etkili Bağış yönetim paneline davet edildiniz.", "Ctrldown yönetim paneline davet edildiniz.")
			_ = afero.WriteFile(fs, path, []byte(content), 0644)
		} else if strings.HasSuffix(name, ".tsx") || strings.HasSuffix(name, ".ts") {
			data, err := afero.ReadFile(fs, path)
			if err != nil {
				return nil
			}
			content := string(data)
			if strings.Contains(content, "| Etkili Bağış") {
				updated := strings.ReplaceAll(content, "| Etkili Bağış", "| Dashboard")
				_ = afero.WriteFile(fs, path, []byte(updated), 0644)
			}
		}
		return nil
	})
}

func copyFileOrDir(fs afero.Fs, srcPath, dstPath string) error {
	info, err := fs.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("source path does not exist (%s): %w", srcPath, err)
	}
	if info.IsDir() {
		return BootstrapFeature(fs, srcPath, dstPath)
	}
	if err := fs.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	return copyFile(fs, srcPath, dstPath)
}

// IntegrateFeatureToMonolith injects either a library feature (with feature.toml), an extracted slice folder,
// or a new Go Echo module into the target monolithic project and updates Go AST & ctrl.yaml.
func IntegrateFeatureToMonolith(fs afero.Fs, opts IntegrateFeatureOptions) error {
	if opts.FeatureName == "" && opts.SourceDir != "" {
		if m, err := LoadFeatureManifest(fs, opts.SourceDir); err == nil && m.ID != "" {
			opts.FeatureName = m.ID
		} else {
			opts.FeatureName = filepath.Base(opts.SourceDir)
		}
	}
	if opts.FeatureName == "" {
		return fmt.Errorf("feature name is required")
	}
	if opts.ProjectDir == "" {
		opts.ProjectDir = "."
	}

	if strings.EqualFold(opts.FeatureName, "ctrlweb") {
		return IntegrateCtrlweb(fs, CtrlwebIntegrationOptions{
			ProjectDir:         opts.ProjectDir,
			LibraryDir:         opts.LibraryDir,
			WithBackendService: true,
			WithBuilderDomain:  true,
			WithExampleApp:     opts.WithExampleApp,
			ExampleAppName:     opts.ExampleAppName,
		})
	}

	// Resolve SourceDir from LibraryDir if SourceDir is empty and not scaffolding a bare module
	if opts.SourceDir == "" && opts.LibraryDir != "" && !opts.ScaffoldEchoMod {
		candidate := filepath.Join(opts.LibraryDir, opts.FeatureName)
		if dirExists(fs, candidate) {
			opts.SourceDir = candidate
		}
	}

	// Load or inspect ctrl.yaml
	ctrlConf, err := config.LoadCtrlConfig(fs, opts.ProjectDir)
	if err != nil {
		ctrlConf, err = InspectProject(fs, opts.ProjectDir)
		if err != nil {
			return fmt.Errorf("failed to load or inspect project at %s: %w", opts.ProjectDir, err)
		}
	}

	// Check if SourceDir has a feature.toml manifest with multi-target / go_inject rules
	if opts.SourceDir != "" && hasFile(fs, filepath.Join(opts.SourceDir, "feature.toml")) {
		manifest, err := LoadFeatureManifest(fs, opts.SourceDir)
		if err == nil && len(manifest.Targets) > 0 {
			return applyManifestFeature(fs, opts, ctrlConf, manifest)
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

	// 1. Copy slices from SourceDir or scaffold a new Go Echo module
	hasBackendSlice := false
	hasPackageSlice := false

	if opts.SourceDir != "" {
		backendSliceDir := filepath.Join(opts.SourceDir, "slices", "backend")
		packageSliceDir := filepath.Join(opts.SourceDir, "slices", "package")
		dashboardSliceDir := filepath.Join(opts.SourceDir, "slices", "dashboard")

		if dirExists(fs, backendSliceDir) {
			destBackend := filepath.Join(opts.ProjectDir, apiPath, "internal", opts.FeatureName)
			if err := BootstrapFeature(fs, backendSliceDir, destBackend); err != nil {
				return err
			}
			hasBackendSlice = true
		}

		if dirExists(fs, packageSliceDir) {
			destPkg := filepath.Join(opts.ProjectDir, "packages", "features", opts.FeatureName)
			if err := BootstrapFeature(fs, packageSliceDir, destPkg); err != nil {
				return err
			}
			hasPackageSlice = true
		}

		if dirExists(fs, dashboardSliceDir) {
			destDash := filepath.Join(opts.ProjectDir, "domains", "dashboard", "src", "features", opts.FeatureName)
			if err := BootstrapFeature(fs, dashboardSliceDir, destDash); err != nil {
				return err
			}
		}

		// If SourceDir doesn't use slices/, check whether it's a Go package or JS package directly
		if !hasBackendSlice && !hasPackageSlice {
			if hasFile(fs, filepath.Join(opts.SourceDir, "package.json")) {
				destPkg := filepath.Join(opts.ProjectDir, "packages", "features", opts.FeatureName)
				if err := BootstrapFeature(fs, opts.SourceDir, destPkg); err != nil {
					return err
				}
				hasPackageSlice = true
			} else {
				destBackend := filepath.Join(opts.ProjectDir, apiPath, "internal", opts.FeatureName)
				if err := BootstrapFeature(fs, opts.SourceDir, destBackend); err != nil {
					return err
				}
				hasBackendSlice = true
			}
		}
	} else if opts.ScaffoldEchoMod {
		if err := scaffoldGoEchoModule(fs, filepath.Join(opts.ProjectDir, apiPath, "internal", opts.FeatureName), opts.FeatureName, opts.WithAutoMigrate); err != nil {
			return fmt.Errorf("failed to scaffold Go Echo module: %w", err)
		}
		hasBackendSlice = true
	}

	// 2. Wire Go Echo AST in routes.go and main.go
	if hasBackendSlice {
		importPath := fmt.Sprintf("%s/internal/%s", apiModule, opts.FeatureName)
		routesFile := filepath.Join(opts.ProjectDir, apiPath, "internal", "server", "routes.go")
		if hasFile(fs, routesFile) {
			routeStmt := fmt.Sprintf("%s.RegisterRoutes(e, %s.NewHandler(%s.NewService(%s.NewRepository())))",
				opts.FeatureName, opts.FeatureName, opts.FeatureName, opts.FeatureName)
			if err := InjectGoEchoRouteAST(fs, routesFile, importPath, routeStmt); err != nil {
				return fmt.Errorf("failed to inject route into %s: %w", routesFile, err)
			}
		}

		if opts.WithAutoMigrate {
			mainFile := filepath.Join(opts.ProjectDir, apiPath, "cmd", "api", "main.go")
			if hasFile(fs, mainFile) {
				migrateStmt := fmt.Sprintf("_ = %s.AutoMigrate(database.DB)", opts.FeatureName)
				if err := InjectGoEchoMigrateAST(fs, mainFile, importPath, migrateStmt); err != nil {
					return fmt.Errorf("failed to inject AutoMigrate into %s: %w", mainFile, err)
				}
			}
		}

		if ctrlConf.Backend == nil {
			ctrlConf.Backend = &config.BackendConfig{
				Framework: "echo",
				Path:      apiPath,
				Module:    apiModule,
			}
		}
		if !containsString(ctrlConf.Backend.Modules, opts.FeatureName) {
			ctrlConf.Backend.Modules = append(ctrlConf.Backend.Modules, opts.FeatureName)
		}
	}

	// 3. Wire Workspace Package into ctrl.yaml & optional target domain package.json
	if hasPackageSlice {
		pkgName := fmt.Sprintf("@workspace/%s", opts.FeatureName)
		pkgRelPath := filepath.ToSlash(filepath.Join("packages", "features", opts.FeatureName))

		registerPackageInConfig(ctrlConf, pkgName, pkgRelPath)

		if opts.TargetDomain != "" {
			domainPkgJSON := filepath.Join(opts.ProjectDir, "domains", opts.TargetDomain, "package.json")
			if hasFile(fs, domainPkgJSON) {
				if err := addWorkspaceDependency(fs, domainPkgJSON, pkgName); err != nil {
					return fmt.Errorf("failed to link %s into %s: %w", pkgName, domainPkgJSON, err)
				}
			}
		}
	}

	if !containsString(ctrlConf.Features, opts.FeatureName) {
		ctrlConf.Features = append(ctrlConf.Features, opts.FeatureName)
	}
	RecordFeatureLock(fs, ctrlConf, opts.FeatureName, "1.0.0", opts.SourceDir)

	return config.SaveCtrlConfig(fs, opts.ProjectDir, ctrlConf)
}

func applyManifestFeature(
	fs afero.Fs,
	opts IntegrateFeatureOptions,
	ctrlConf *config.CtrlConfig,
	manifest *FeatureManifest,
) error {
	// 1. Install required feature dependencies first if available in the library
	libDir := opts.LibraryDir
	if libDir == "" && opts.SourceDir != "" {
		libDir = filepath.Dir(opts.SourceDir)
	}
	for _, reqID := range manifest.Requires {
		if containsString(ctrlConf.Features, reqID) {
			continue
		}
		reqDir := filepath.Join(libDir, reqID)
		if dirExists(fs, reqDir) {
			if err := IntegrateFeatureToMonolith(fs, IntegrateFeatureOptions{
				ProjectDir:   opts.ProjectDir,
				FeatureName:  reqID,
				SourceDir:    reqDir,
				LibraryDir:   libDir,
				TargetDomain: opts.TargetDomain,
			}); err != nil {
				return fmt.Errorf("failed to install required feature %q for %q: %w", reqID, manifest.ID, err)
			}
			// Reload ctrlConf after installing dependency
			if updated, err := config.LoadCtrlConfig(fs, opts.ProjectDir); err == nil {
				ctrlConf = updated
			}
		}
	}

	// 2. Copy all target slices into the monolithic project
	sliceKeys := make([]string, 0, len(manifest.Targets))
	for k := range manifest.Targets {
		sliceKeys = append(sliceKeys, k)
	}
	sort.Strings(sliceKeys)

	for _, sliceRel := range sliceKeys {
		targetRel := manifest.Targets[sliceRel]
		srcPath := filepath.Join(opts.SourceDir, filepath.FromSlash(sliceRel))
		dstPath := filepath.Join(opts.ProjectDir, filepath.FromSlash(targetRel))
		if err := copyFileOrDir(fs, srcPath, dstPath); err != nil {
			return fmt.Errorf("failed to copy slice %s to %s: %w", sliceRel, targetRel, err)
		}
	}

	// 3. Apply Go Echo AST injection rules
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

	routesFile := filepath.Join(opts.ProjectDir, apiPath, "internal", "server", "routes.go")
	mainFile := filepath.Join(opts.ProjectDir, apiPath, "cmd", "api", "main.go")

	for _, rule := range manifest.GoInject {
		impPath := rule.Import
		if impPath == "" && rule.Module != "" {
			impPath = fmt.Sprintf("%s/internal/%s", apiModule, rule.Module)
		}

		if rule.Route != "" && hasFile(fs, routesFile) {
			imports := append([]string{impPath}, rule.ExtraImports...)
			if err := InjectGoEchoRouteASTWithImports(fs, routesFile, imports, rule.Route); err != nil {
				return fmt.Errorf("failed to inject route for module %s: %w", rule.Module, err)
			}
		}

		if rule.Migrate != "" && hasFile(fs, mainFile) {
			if err := InjectGoEchoMigrateAST(fs, mainFile, impPath, rule.Migrate); err != nil {
				return fmt.Errorf("failed to inject AutoMigrate for module %s: %w", rule.Module, err)
			}
		}

		if rule.Module != "" {
			if ctrlConf.Backend == nil {
				ctrlConf.Backend = &config.BackendConfig{
					Framework: "echo",
					Path:      apiPath,
					Module:    apiModule,
				}
			}
			if !containsString(ctrlConf.Backend.Modules, rule.Module) {
				ctrlConf.Backend.Modules = append(ctrlConf.Backend.Modules, rule.Module)
			}
		}
	}

	// 4. Register workspace packages and link into frontend domains
	for _, wp := range manifest.WorkspacePackages {
		registerPackageInConfig(ctrlConf, wp.Name, wp.Path)

		domainsToLink := wp.LinkDomains
		if opts.TargetDomain != "" && !containsString(domainsToLink, opts.TargetDomain) {
			domainsToLink = append(domainsToLink, opts.TargetDomain)
		}
		for _, dom := range domainsToLink {
			domainPkgJSON := filepath.Join(opts.ProjectDir, "domains", dom, "package.json")
			if hasFile(fs, domainPkgJSON) {
				if err := addWorkspaceDependency(fs, domainPkgJSON, wp.Name); err != nil {
					return fmt.Errorf("failed to link %s into %s: %w", wp.Name, domainPkgJSON, err)
				}
			}
		}
	}

	featureID := manifest.ID
	if featureID == "" {
		featureID = opts.FeatureName
	}
	if !containsString(ctrlConf.Features, featureID) {
		ctrlConf.Features = append(ctrlConf.Features, featureID)
	}
	RecordFeatureLock(fs, ctrlConf, featureID, manifest.Version, opts.SourceDir)

	return config.SaveCtrlConfig(fs, opts.ProjectDir, ctrlConf)
}

func registerPackageInConfig(ctrlConf *config.CtrlConfig, pkgName, pkgRelPath string) {
	for _, p := range ctrlConf.Packages {
		if p.Name == pkgName || p.Path == pkgRelPath {
			return
		}
	}
	ctrlConf.Packages = append(ctrlConf.Packages, config.PackageEntry{
		Name: pkgName,
		Path: pkgRelPath,
	})
}

func scaffoldGoEchoModule(fs afero.Fs, modDir string, modName string, withMigrate bool) error {
	if err := fs.MkdirAll(modDir, 0755); err != nil {
		return err
	}

	migrateBlock := ""
	gormImport := ""
	if withMigrate {
		gormImport = "\n\t\"gorm.io/gorm\""
		migrateBlock = fmt.Sprintf(`
type Record struct {
	ID uint `+"`gorm:\"primaryKey\" json:\"id\"`"+`
}

func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	return db.AutoMigrate(&Record{})
}
`)
	}

	code := fmt.Sprintf(`package %s

import (
	"net/http"

	"github.com/labstack/echo/v4"%s
)
%s
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

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"module": "%s",
		"status": "active",
	})
}

func RegisterRoutes(e *echo.Echo, h *Handler) {
	group := e.Group("/api/v1/%s")
	group.GET("", h.List)
}
`, modName, gormImport, migrateBlock, modName, modName)

	return afero.WriteFile(fs, filepath.Join(modDir, "handler.go"), []byte(code), 0644)
}

func addWorkspaceDependency(fs afero.Fs, pkgJSONPath string, depName string) error {
	mixinContent := fmt.Sprintf(`{
  "dependencies": {
    "%s": "workspace:*"
  }
}`, depName)

	tmpMixin := pkgJSONPath + ".tmp.mixin"
	if err := afero.WriteFile(fs, tmpMixin, []byte(mixinContent), 0644); err != nil {
		return err
	}
	defer func() { _ = fs.Remove(tmpMixin) }()

	return MergeJSON(fs, pkgJSONPath, tmpMixin)
}

func discoverDomains(fs afero.Fs, projectDir string) ([]config.DomainEntry, *config.BackendConfig, error) {
	domainsRoot := filepath.Join(projectDir, "domains")
	if !dirExists(fs, domainsRoot) {
		return nil, nil, nil
	}

	entries, err := afero.ReadDir(fs, domainsRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read domains directory: %w", err)
	}

	var domains []config.DomainEntry
	var backend *config.BackendConfig

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		domainPath := filepath.Join(domainsRoot, name)
		relPath := filepath.ToSlash(filepath.Join("domains", name))

		goModPath := filepath.Join(domainPath, "go.mod")
		if hasFile(fs, goModPath) {
			modName, framework, dbType := parseGoModInfo(fs, goModPath)
			modules := discoverGoInternalModules(fs, filepath.Join(domainPath, "internal"))

			domains = append(domains, config.DomainEntry{
				Name: name,
				Type: "go-" + framework,
				Path: relPath,
			})

			backend = &config.BackendConfig{
				Framework: framework,
				Path:      relPath,
				Module:    modName,
				Database:  dbType,
				Modules:   modules,
			}
			continue
		}

		pkgJSONPath := filepath.Join(domainPath, "package.json")
		if hasFile(fs, pkgJSONPath) {
			domainType := detectJSDomainType(fs, pkgJSONPath)
			domains = append(domains, config.DomainEntry{
				Name: name,
				Type: domainType,
				Path: relPath,
			})
		}
	}

	return domains, backend, nil
}

func discoverGoInternalModules(fs afero.Fs, internalDir string) []string {
	if !dirExists(fs, internalDir) {
		return nil
	}
	entries, err := afero.ReadDir(fs, internalDir)
	if err != nil {
		return nil
	}

	skip := map[string]bool{
		"server":   true,
		"database": true,
	}

	var modules []string
	for _, entry := range entries {
		if entry.IsDir() && !skip[entry.Name()] {
			modules = append(modules, entry.Name())
		}
	}
	sort.Strings(modules)
	return modules
}

func discoverPackages(fs afero.Fs, projectDir string) ([]config.PackageEntry, error) {
	packagesRoot := filepath.Join(projectDir, "packages")
	if !dirExists(fs, packagesRoot) {
		return nil, nil
	}

	var result []config.PackageEntry
	seen := make(map[string]bool)

	searchRoots := []string{
		packagesRoot,
		filepath.Join(packagesRoot, "config"),
		filepath.Join(packagesRoot, "features"),
	}

	for _, root := range searchRoots {
		if !dirExists(fs, root) {
			continue
		}
		entries, err := afero.ReadDir(fs, root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			pkgDir := filepath.Join(root, entry.Name())
			pkgJSON := filepath.Join(pkgDir, "package.json")
			if !hasFile(fs, pkgJSON) {
				continue
			}
			rel, err := filepath.Rel(projectDir, pkgDir)
			if err != nil {
				continue
			}
			relSlash := filepath.ToSlash(rel)
			if seen[relSlash] {
				continue
			}
			seen[relSlash] = true

			pkgName := readJSONName(fs, pkgJSON)
			if pkgName == "" {
				pkgName = entry.Name()
			}
			result = append(result, config.PackageEntry{
				Name: pkgName,
				Path: relSlash,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result, nil
}

func detectProjectTitle(fs afero.Fs, projectDir string) string {
	pkgJSON := filepath.Join(projectDir, "package.json")
	if name := readJSONName(fs, pkgJSON); name != "" {
		return name
	}
	abs, err := filepath.Abs(projectDir)
	if err == nil {
		return filepath.Base(abs)
	}
	return filepath.Base(projectDir)
}

func readJSONName(fs afero.Fs, jsonPath string) string {
	data, err := afero.ReadFile(fs, jsonPath)
	if err != nil {
		return ""
	}
	var parsed struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return ""
	}
	return parsed.Name
}

func detectJSDomainType(fs afero.Fs, pkgJSONPath string) string {
	data, err := afero.ReadFile(fs, pkgJSONPath)
	if err != nil {
		return "web"
	}
	content := string(data)
	if strings.Contains(content, `"next"`) {
		return "nextjs"
	}
	if strings.Contains(content, `"@rspack/core"`) {
		return "react-rspack"
	}
	return "node"
}

func parseGoModInfo(fs afero.Fs, goModPath string) (modName string, framework string, dbType string) {
	modName = "api"
	framework = "standard"
	dbType = "none"

	data, err := afero.ReadFile(fs, goModPath)
	if err != nil {
		return
	}
	content := string(data)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") {
			modName = strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
			break
		}
	}
	if strings.Contains(content, "github.com/labstack/echo") {
		framework = "echo"
	}
	if strings.Contains(content, "postgres") || strings.Contains(content, "pgx") {
		dbType = "postgres"
	} else if strings.Contains(content, "sqlite") {
		dbType = "sqlite"
	}
	return
}

func hasFile(fs afero.Fs, path string) bool {
	exists, err := afero.Exists(fs, path)
	return err == nil && exists
}

func dirExists(fs afero.Fs, path string) bool {
	exists, err := afero.DirExists(fs, path)
	return err == nil && exists
}

func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
