package engine_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/engine"
	"ctrldown/cli/internal/fs"
)

func TestBootstrapProjectAndIntegrateEchoFeature(t *testing.T) {
	memFS := fs.NewMemory()

	// 1. Create a new monolithic project (Etkilibagis standard)
	projectDir := "/workspace/my-monolith"
	conf, err := engine.BootstrapProject(memFS, engine.ProjectOptions{
		Title:           "my-monolith",
		DestPath:        projectDir,
		Database:        "postgres",
		Auth:            "ory",
		FrontendDomains: []string{"landing", "dashboard"},
		IncludeSharedUI: true,
	})
	if err != nil {
		t.Fatalf("BootstrapProject failed: %v", err)
	}

	if conf.Project.Variant != "Project" || conf.Project.Architecture != "monolith-turborepo-echo" {
		t.Fatalf("unexpected project config: %+v", conf.Project)
	}
	if conf.Backend == nil || conf.Backend.Framework != "echo" {
		t.Fatalf("expected Echo backend config, got: %+v", conf.Backend)
	}

	expectedFiles := []string{
		"package.json",
		"pnpm-workspace.yaml",
		"turbo.json",
		"ctrl.yaml",
		"domains/api/go.mod",
		"domains/api/cmd/api/main.go",
		"domains/api/internal/server/server.go",
		"domains/api/internal/server/routes.go",
		"domains/api/internal/health/handler.go",
		"domains/landing/package.json",
		"domains/dashboard/package.json",
		"packages/ui/package.json",
		"environments/docker/compose.yaml",
	}

	for _, rel := range expectedFiles {
		full := filepath.Join(projectDir, rel)
		exists, err := afero.Exists(memFS, full)
		if err != nil || !exists {
			t.Fatalf("expected file %s to exist", full)
		}
	}

	// 2. Integrate a new Go Echo module ("blog") with AutoMigrate into the monolith
	err = engine.IntegrateFeatureToMonolith(memFS, engine.IntegrateFeatureOptions{
		ProjectDir:      projectDir,
		FeatureName:     "blog",
		ScaffoldEchoMod: true,
		WithAutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("IntegrateFeatureToMonolith failed: %v", err)
	}

	// Verify routes.go has valid Go AST and includes blog.RegisterRoutes
	routesPath := filepath.Join(projectDir, "domains", "api", "internal", "server", "routes.go")
	routesBytes, err := afero.ReadFile(memFS, routesPath)
	if err != nil {
		t.Fatalf("failed to read routes.go: %v", err)
	}
	if _, err := decorator.Parse(routesBytes); err != nil {
		t.Fatalf("modified routes.go has invalid Go syntax: %v\n%s", err, string(routesBytes))
	}
	routesContent := string(routesBytes)
	if !strings.Contains(routesContent, `"api/internal/blog"`) {
		t.Errorf("expected routes.go to import api/internal/blog, got:\n%s", routesContent)
	}
	if !strings.Contains(routesContent, "blog.RegisterRoutes") {
		t.Errorf("expected routes.go to call blog.RegisterRoutes, got:\n%s", routesContent)
	}

	// Verify main.go has valid Go AST and includes blog.AutoMigrate before server.New()
	mainPath := filepath.Join(projectDir, "domains", "api", "cmd", "api", "main.go")
	mainBytes, err := afero.ReadFile(memFS, mainPath)
	if err != nil {
		t.Fatalf("failed to read main.go: %v", err)
	}
	if _, err := decorator.Parse(mainBytes); err != nil {
		t.Fatalf("modified main.go has invalid Go syntax: %v\n%s", err, string(mainBytes))
	}
	mainContent := string(mainBytes)
	if !strings.Contains(mainContent, `"api/internal/blog"`) {
		t.Errorf("expected main.go to import api/internal/blog, got:\n%s", mainContent)
	}
	if !strings.Contains(mainContent, "blog.AutoMigrate(database.DB)") {
		t.Errorf("expected main.go to call blog.AutoMigrate(database.DB), got:\n%s", mainContent)
	}

	// Verify ctrl.yaml now lists both "health" and "blog"
	updatedConf, err := config.LoadCtrlConfig(memFS, projectDir)
	if err != nil {
		t.Fatalf("LoadCtrlConfig failed: %v", err)
	}
	if len(updatedConf.Backend.Modules) != 2 {
		t.Fatalf("expected 2 backend modules in ctrl.yaml, got: %v", updatedConf.Backend.Modules)
	}
}

func TestExtractAndReintegratePackage(t *testing.T) {
	memFS := fs.NewMemory()

	// 1. Setup a source monolith with a package "@workspace/bloub"
	srcProject := "/workspace/etkilibagis-sim"
	_, err := engine.BootstrapProject(memFS, engine.ProjectOptions{
		Title:           "etkilibagis",
		DestPath:        srcProject,
		Database:        "postgres",
		Auth:            "ory",
		FrontendDomains: []string{"dashboard"},
		IncludeSharedUI: true,
	})
	if err != nil {
		t.Fatalf("BootstrapProject failed: %v", err)
	}

	bloubDir := filepath.Join(srcProject, "packages", "bloub")
	_ = memFS.MkdirAll(filepath.Join(bloubDir, "src"), 0755)
	_ = afero.WriteFile(memFS, filepath.Join(bloubDir, "package.json"), []byte(`{"name": "@workspace/bloub", "version": "0.1.0"}`), 0644)
	_ = afero.WriteFile(memFS, filepath.Join(bloubDir, "src", "index.ts"), []byte(`export const BloubBot = () => null;`), 0644)

	// 2. InspectProject should discover @workspace/bloub
	inspected, err := engine.InspectProject(memFS, srcProject)
	if err != nil {
		t.Fatalf("InspectProject failed: %v", err)
	}
	foundBloub := false
	for _, p := range inspected.Packages {
		if p.Name == "@workspace/bloub" {
			foundBloub = true
			break
		}
	}
	if !foundBloub {
		t.Fatalf("expected InspectProject to discover @workspace/bloub, got: %+v", inspected.Packages)
	}

	// 3. Extract @workspace/bloub into a standalone library folder
	extractedDir := "/workspace/library/bloub"
	err = engine.ExtractFeature(memFS, engine.ExtractOptions{
		ProjectDir:  srcProject,
		FeatureName: "bloub",
		Title:       "Bloub Avatar Engine",
		Description: "Interactive SVG bot avatar",
		Category:    "package",
		SlicePaths:  map[string]string{"package": "packages/bloub"},
		OutputDir:   extractedDir,
	})
	if err != nil {
		t.Fatalf("ExtractFeature failed: %v", err)
	}

	if exists, _ := afero.Exists(memFS, filepath.Join(extractedDir, "feature.toml")); !exists {
		t.Fatalf("expected feature.toml to be generated in %s", extractedDir)
	}

	// 4. Integrate extracted bloub into another monolithic project and link to dashboard
	destProject := "/workspace/target-monolith"
	_, err = engine.BootstrapProject(memFS, engine.ProjectOptions{
		Title:           "target-monolith",
		DestPath:        destProject,
		FrontendDomains: []string{"dashboard"},
		IncludeSharedUI: true,
	})
	if err != nil {
		t.Fatalf("BootstrapProject failed: %v", err)
	}

	err = engine.IntegrateFeatureToMonolith(memFS, engine.IntegrateFeatureOptions{
		ProjectDir:   destProject,
		FeatureName:  "bloub",
		SourceDir:    extractedDir,
		TargetDomain: "dashboard",
	})
	if err != nil {
		t.Fatalf("IntegrateFeatureToMonolith failed: %v", err)
	}

	// Check that packages/bloub/src/index.ts exists in target monolith
	targetPkgFile := filepath.Join(destProject, "packages", "bloub", "src", "index.ts")
	if exists, _ := afero.Exists(memFS, targetPkgFile); !exists {
		t.Fatalf("expected integrated package file %s to exist", targetPkgFile)
	}

	// Check that domains/dashboard/package.json now includes "@workspace/bloub": "workspace:*"
	dashPkgBytes, err := afero.ReadFile(memFS, filepath.Join(destProject, "domains", "dashboard", "package.json"))
	if err != nil {
		t.Fatalf("failed to read dashboard package.json: %v", err)
	}
	if !strings.Contains(string(dashPkgBytes), `"@workspace/bloub": "workspace:*"`) {
		t.Fatalf("expected dashboard package.json to include @workspace/bloub dependency, got:\n%s", string(dashPkgBytes))
	}
}

func TestCompositeAuthAndBloubExtractionAndPreInstall(t *testing.T) {
	memFS := fs.NewMemory()

	// 1. Setup a simulated Etkilibagis source repository with bloub and auth/identity
	srcDir := "/workspace/etkilibagis"
	_ = memFS.MkdirAll(filepath.Join(srcDir, "packages", "bloub", "src"), 0755)
	_ = afero.WriteFile(memFS, filepath.Join(srcDir, "packages", "bloub", "package.json"), []byte(`{"name":"@workspace/bloub","version":"0.1.0"}`), 0644)
	_ = afero.WriteFile(memFS, filepath.Join(srcDir, "packages", "bloub", "src", "bot.ts"), []byte(`export const BloubBot = () => null;`), 0644)

	_ = memFS.MkdirAll(filepath.Join(srcDir, "domains", "api", "internal", "auth"), 0755)
	_ = afero.WriteFile(memFS, filepath.Join(srcDir, "domains", "api", "internal", "auth", "ory.go"), []byte("package auth\nfunc NewCheckerFromEnv() interface{} { return nil }\n"), 0644)

	_ = memFS.MkdirAll(filepath.Join(srcDir, "domains", "api", "internal", "identity"), 0755)
	_ = afero.WriteFile(memFS, filepath.Join(srcDir, "domains", "api", "internal", "identity", "invite_mailer.go"), []byte(`package identity
import "os"
import "strings"
func send() string {
	_ = "Etkili Bağış yönetim daveti"
	return "Etkili Bağış yönetim paneline davet edildiniz."
}
`), 0644)

	_ = memFS.MkdirAll(filepath.Join(srcDir, "domains", "dashboard", "src", "components", "auth"), 0755)
	_ = afero.WriteFile(memFS, filepath.Join(srcDir, "domains", "dashboard", "src", "components", "auth", "auth-experience.tsx"), []byte(`export function AuthExperience() { return null; }`), 0644)

	// 2. Build composite extract options and extract both "bloub" and "auth" into /workspace/features
	libDir := "/workspace/features"
	bloubOpts, ok := engine.BuildCompositeExtractOptions(memFS, srcDir, "bloub", filepath.Join(libDir, "bloub"))
	if !ok {
		t.Fatalf("expected BuildCompositeExtractOptions to succeed for bloub")
	}
	if err := engine.ExtractFeature(memFS, bloubOpts); err != nil {
		t.Fatalf("ExtractFeature(bloub) failed: %v", err)
	}

	authOpts, ok := engine.BuildCompositeExtractOptions(memFS, srcDir, "auth", filepath.Join(libDir, "auth"))
	if !ok {
		t.Fatalf("expected BuildCompositeExtractOptions to succeed for auth")
	}
	if err := engine.ExtractFeature(memFS, authOpts); err != nil {
		t.Fatalf("ExtractFeature(auth) failed: %v", err)
	}

	// Verify de-branding occurred in extracted invite_mailer.go
	mailerBytes, err := afero.ReadFile(memFS, filepath.Join(libDir, "auth", "slices", "backend-identity", "invite_mailer.go"))
	if err != nil {
		t.Fatalf("failed to read extracted invite_mailer.go: %v", err)
	}
	if strings.Contains(string(mailerBytes), `"Etkili Bağış yönetim daveti"`) {
		t.Fatalf("expected hardcoded brand string to be debranded in invite_mailer.go, got:\n%s", string(mailerBytes))
	}
	if !strings.Contains(string(mailerBytes), "DASHBOARD_APP_NAME") {
		t.Fatalf("expected DASHBOARD_APP_NAME parameterization in invite_mailer.go, got:\n%s", string(mailerBytes))
	}

	// 3. Create a new monolithic project with Features: ["auth"] (which requires "bloub" automatically)
	newProj := "/workspace/new-monolith"
	conf, err := engine.BootstrapProject(memFS, engine.ProjectOptions{
		Title:           "new-monolith",
		DestPath:        newProj,
		LibraryPath:     libDir,
		FrontendDomains: []string{"dashboard"},
		IncludeSharedUI: true,
		Features:        []string{"auth"},
	})
	if err != nil {
		t.Fatalf("BootstrapProject with pre-installed auth feature failed: %v", err)
	}

	// Verify both "bloub" (dependency) and "auth" were installed!
	if len(conf.Features) != 2 || conf.Features[0] != "bloub" || conf.Features[1] != "auth" {
		t.Fatalf("expected features [bloub, auth] to be installed via dependency resolution, got: %v", conf.Features)
	}

	// Verify files exist in new-monolith
	mustExist := []string{
		"packages/bloub/src/bot.ts",
		"domains/api/internal/auth/ory.go",
		"domains/api/internal/identity/invite_mailer.go",
		"domains/dashboard/src/components/auth/auth-experience.tsx",
	}
	for _, rel := range mustExist {
		p := filepath.Join(newProj, rel)
		if exists, _ := afero.Exists(memFS, p); !exists {
			t.Fatalf("expected file %s to exist in target monolith", p)
		}
	}

	// Verify routes.go and main.go have valid Go syntax and include identity wiring
	routesBytes, _ := afero.ReadFile(memFS, filepath.Join(newProj, "domains", "api", "internal", "server", "routes.go"))
	if _, err := decorator.Parse(routesBytes); err != nil {
		t.Fatalf("routes.go invalid AST after auth injection: %v\n%s", err, string(routesBytes))
	}
	if !strings.Contains(string(routesBytes), "identity.RegisterRoutes") || !strings.Contains(string(routesBytes), `"api/internal/auth"`) {
		t.Fatalf("expected identity.RegisterRoutes and api/internal/auth import in routes.go, got:\n%s", string(routesBytes))
	}
}

func TestIntegrateCtrlwebWithExampleApp(t *testing.T) {
	memFS := fs.NewMemory()

	projDir := "/workspace/ctrlweb-monolith"
	conf, err := engine.BootstrapProject(memFS, engine.ProjectOptions{
		Title:                 "ctrlweb-monolith",
		DestPath:              projDir,
		FrontendDomains:       []string{"ctrlweb"},
		IncludeSharedUI:       true,
		WithCtrlwebExampleApp: true,
	})
	if err != nil {
		t.Fatalf("BootstrapProject with ctrlweb failed: %v", err)
	}

	// Verify expected Ctrlweb files across all 4 layers
	mustExist := []string{
		"packages/ui/src/ctrlweb-registry.tsx",
		"domains/api/internal/ctrlweb/handler.go",
		"domains/ctrlweb/package.json",
		"domains/ctrlweb/lib/ory-editor-access.ts",
		"domains/ctrlweb/components/workspace/bridge.tsx",
		"domains/ctrlweb-example/package.json",
		"domains/ctrlweb-example/src/lib/ctrlweb.ts",
		"domains/ctrlweb-example/src/components/CtrlwebRenderer.tsx",
		"domains/ctrlweb-example/src/app/[[...slug]]/page.tsx",
	}

	for _, rel := range mustExist {
		full := filepath.Join(projDir, rel)
		if exists, _ := afero.Exists(memFS, full); !exists {
			t.Fatalf("expected file %s to exist", full)
		}
	}

	// Verify Go Echo handler.go and routes.go both parse as valid Go AST
	handlerBytes, err := afero.ReadFile(memFS, filepath.Join(projDir, "domains", "api", "internal", "ctrlweb", "handler.go"))
	if err != nil {
		t.Fatalf("failed to read ctrlweb handler.go: %v", err)
	}
	if _, err := decorator.Parse(handlerBytes); err != nil {
		t.Fatalf("ctrlweb/handler.go has invalid Go syntax: %v", err)
	}

	routesBytes, err := afero.ReadFile(memFS, filepath.Join(projDir, "domains", "api", "internal", "server", "routes.go"))
	if err != nil {
		t.Fatalf("failed to read routes.go: %v", err)
	}
	if _, err := decorator.Parse(routesBytes); err != nil {
		t.Fatalf("routes.go has invalid Go syntax after ctrlweb injection: %v\n%s", err, string(routesBytes))
	}
	if !strings.Contains(string(routesBytes), `"api/internal/ctrlweb"`) || !strings.Contains(string(routesBytes), "ctrlweb.RegisterRoutes") {
		t.Fatalf("expected ctrlweb route wiring in routes.go, got:\n%s", string(routesBytes))
	}

	// Verify ctrl.yaml registered ctrlweb module and both ctrlweb & ctrlweb-example domains
	hasCtrlwebMod := false
	for _, m := range conf.Backend.Modules {
		if m == "ctrlweb" {
			hasCtrlwebMod = true
		}
	}
	if !hasCtrlwebMod {
		t.Fatalf("expected ctrlweb in Backend.Modules, got: %v", conf.Backend.Modules)
	}
}

func TestProjectStatusDriftAndUpgrade(t *testing.T) {
	memFS := fs.NewMemory()

	// 1. Create a feature in the library ("bloub" v1.0.0)
	libDir := "/workspace/features"
	bloubLibDir := filepath.Join(libDir, "bloub")
	_ = memFS.MkdirAll(filepath.Join(bloubLibDir, "slices", "package", "src"), 0755)
	_ = afero.WriteFile(memFS, filepath.Join(bloubLibDir, "slices", "package", "src", "index.ts"), []byte("export const v = 1;"), 0644)
	manifestV1 := `id = "bloub"
version = "1.0.0"
title = "Bloub"
description = "Avatar"
category = "package"

[targets]
"slices/package" = "packages/bloub"
`
	_ = afero.WriteFile(memFS, filepath.Join(bloubLibDir, "feature.toml"), []byte(manifestV1), 0644)

	// 2. Create a monolithic project with bloub pre-installed
	projDir := "/workspace/status-test"
	_, err := engine.BootstrapProject(memFS, engine.ProjectOptions{
		Title:       "status-test",
		DestPath:    projDir,
		LibraryPath: libDir,
		Features:    []string{"bloub"},
	})
	if err != nil {
		t.Fatalf("BootstrapProject failed: %v", err)
	}

	// Verify initial status is clean (v2, no drift, bloub up-to-date)
	rep1, err := engine.CheckProjectStatus(memFS, projDir, libDir)
	if err != nil {
		t.Fatalf("CheckProjectStatus failed: %v", err)
	}
	if rep1.IsSchemaOutdated || rep1.HasArchitectureDrift() {
		t.Fatalf("expected clean initial status, got outdated=%v drift=%v", rep1.IsSchemaOutdated, rep1.HasArchitectureDrift())
	}
	if len(rep1.FeatureStatuses) != 1 || rep1.FeatureStatuses[0].State != "up-to-date" {
		t.Fatalf("expected bloub to be up-to-date, got: %+v", rep1.FeatureStatuses)
	}

	// 3. Simulate legacy v1 ctrl.yaml, add an untracked Go Echo module ("payments"), and update library bloub to v1.1.0
	legacyYAML := `boilerplate_path: ""
project:
  variant: "Project"
  title: "status-test"
features:
  - bloub
feature_locks:
  bloub:
    version: "1.0.0"
    checksum: "` + rep1.FeatureStatuses[0].InstalledChecksum + `"
`
	_ = afero.WriteFile(memFS, filepath.Join(projDir, "ctrl.yaml"), []byte(legacyYAML), 0644)
	_ = memFS.MkdirAll(filepath.Join(projDir, "domains", "api", "internal", "payments"), 0755)
	_ = afero.WriteFile(memFS, filepath.Join(bloubLibDir, "slices", "package", "src", "index.ts"), []byte("export const v = 2;"), 0644)
	manifestV2 := strings.ReplaceAll(manifestV1, `version = "1.0.0"`, `version = "1.1.0"`)
	_ = afero.WriteFile(memFS, filepath.Join(bloubLibDir, "feature.toml"), []byte(manifestV2), 0644)

	rep2, err := engine.CheckProjectStatus(memFS, projDir, libDir)
	if err != nil {
		t.Fatalf("CheckProjectStatus (after drift) failed: %v", err)
	}
	if !rep2.IsSchemaOutdated {
		t.Errorf("expected IsSchemaOutdated=true for legacy v1 ctrl.yaml")
	}
	if !rep2.HasArchitectureDrift() || len(rep2.UntrackedBackendModules) == 0 {
		t.Errorf("expected untracked backend modules to be detected, got: %+v", rep2.UntrackedBackendModules)
	}
	if len(rep2.FeatureStatuses) != 1 || rep2.FeatureStatuses[0].State != "outdated" {
		t.Errorf("expected bloub feature state to be 'outdated', got: %+v", rep2.FeatureStatuses)
	}

	// 4. Run UpgradeProject with upgradeFeatures=true and verify everything is resolved!
	rep3, err := engine.UpgradeProject(memFS, projDir, libDir, true)
	if err != nil {
		t.Fatalf("UpgradeProject failed: %v", err)
	}
	if rep3.IsSchemaOutdated || rep3.SchemaVersion != config.CurrentSchemaVersion {
		t.Errorf("expected upgraded schema version %d, got %d", config.CurrentSchemaVersion, rep3.SchemaVersion)
	}
	if rep3.HasArchitectureDrift() {
		t.Errorf("expected zero architecture drift after UpgradeProject, got untracked mods=%v", rep3.UntrackedBackendModules)
	}
	if len(rep3.FeatureStatuses) != 1 || rep3.FeatureStatuses[0].State != "up-to-date" || rep3.FeatureStatuses[0].InstalledVersion != "1.1.0" {
		t.Errorf("expected bloub to be upgraded to v1.1.0 and up-to-date, got: %+v", rep3.FeatureStatuses)
	}
}



