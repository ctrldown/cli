package config_test

import (
	"testing"

	"github.com/spf13/afero"
	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/fs"
)

func TestLoadBoilerplateConfigFromRegistryJSON(t *testing.T) {
	memFS := fs.NewMemory()

	registryJSON := `{
  "environment": {
    "local": "environments/local",
    "prod": "environments/prod"
  },
  "hooks": {
    "post_generate_go": ["go mod tidy"]
  },
  "templates": {
    "base-go-gateway": {
      "id": "base-go-gateway",
      "title": "Go API Gateway",
      "description": "Go API Gateway with JWT Auth",
      "language": "go",
      "tier": "backend",
      "path": "templates/gateways/base-go-gateway"
    }
  },
  "mixins": {
    "db-postgres-sqlc": {
      "id": "db-postgres-sqlc",
      "title": "Postgres SQLC",
      "description": "PostgreSQL using sqlc",
      "language": "go",
      "group": "database",
      "path": "mixins/go/db-postgres-sqlc"
    }
  }
}`

	_ = memFS.MkdirAll("/boilerplates", 0755)
	_ = afero.WriteFile(memFS, "/boilerplates/registry.json", []byte(registryJSON), 0644)

	bpConf, err := config.LoadBoilerplateConfig(memFS, "/boilerplates")
	if err != nil {
		t.Fatalf("LoadBoilerplateConfig failed: %v", err)
	}

	gwMap, ok := bpConf.Templates["gateway"]
	if !ok {
		t.Fatalf("expected 'gateway' template category, got: %+v", bpConf.Templates)
	}
	if gwMap["base-go-gateway"].Title != "Go API Gateway" {
		t.Fatalf("unexpected template title: %+v", gwMap["base-go-gateway"])
	}

	goMixins, ok := bpConf.Mixins["go"]
	if !ok || goMixins["db-postgres-sqlc"].Title != "Postgres SQLC" {
		t.Fatalf("expected Go mixin 'db-postgres-sqlc', got: %+v", bpConf.Mixins)
	}
}

func TestGlobalConfigAndPathResolution(t *testing.T) {
	t.Setenv("CTRLDOWN_HOME", "/global-ctrldown")
	t.Setenv("CTRLDOWN_LIBRARY_PATH", "")
	t.Setenv("CTRLDOWN_BOILERPLATE_PATH", "")

	memFS := fs.NewMemory()

	// 1. Save global config with custom paths
	err := config.SaveGlobalConfig(memFS, &config.GlobalConfig{
		LibraryPath:     "/global-ctrldown/features",
		BoilerplatePath: "/global-ctrldown/boilerplates",
	})
	if err != nil {
		t.Fatalf("SaveGlobalConfig failed: %v", err)
	}

	loaded, err := config.LoadGlobalConfig(memFS)
	if err != nil {
		t.Fatalf("LoadGlobalConfig failed: %v", err)
	}
	if loaded.LibraryPath != "/global-ctrldown/features" {
		t.Fatalf("expected LibraryPath '/global-ctrldown/features', got %s", loaded.LibraryPath)
	}
	if loaded.BoilerplateRepo != config.DefaultBoilerplateRepo {
		t.Fatalf("expected default BoilerplateRepo, got %s", loaded.BoilerplateRepo)
	}

	// 2. Verify ResolveLibraryPath & ResolveBoilerplatePath use global config when no flag/env is set
	resolvedLib := config.ResolveLibraryPath(memFS, "", "/any/dir")
	if resolvedLib != "/global-ctrldown/features" {
		t.Fatalf("expected resolved library path '/global-ctrldown/features', got %s", resolvedLib)
	}

	resolvedBp := config.ResolveBoilerplatePath(memFS, "", "/any/dir")
	if resolvedBp != "/global-ctrldown/boilerplates" {
		t.Fatalf("expected resolved boilerplate path '/global-ctrldown/boilerplates', got %s", resolvedBp)
	}

	// 3. Explicit flag takes top priority
	if got := config.ResolveLibraryPath(memFS, "/explicit/features", "/any/dir"); got != "/explicit/features" {
		t.Fatalf("expected explicit flag path, got %s", got)
	}
}
