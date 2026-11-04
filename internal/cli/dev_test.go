package cli

import (
	"testing"

	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/dev/supervisor"
	"github.com/spf13/afero"
)

func TestDiscoverServicesFromCtrlConfig(t *testing.T) {
	fs := afero.NewMemMapFs()

	cfg := &config.CtrlConfig{
		Project: config.ProjectConfig{
			Title: "nexus",
		},
		Domains: []config.DomainEntry{
			{
				Name: "edge",
				Type: "cloudflare-worker",
				Path: "domains/edge",
			},
			{
				Name: "console",
				Type: "cloudflare-pages",
				Path: "domains/console",
			},
			{
				Name: "web",
				Type: "nextjs-app",
				Path: "domains/web",
			},
		},
	}

	svcs := discoverServices(fs, "/workspace", cfg, true)
	if len(svcs) != 3 {
		t.Fatalf("expected 3 discovered services, got %d", len(svcs))
	}

	var edgeSvc, consoleSvc, webSvc *supervisor.ServiceConfig
	for i := range svcs {
		switch svcs[i].Name {
		case "edge":
			edgeSvc = &svcs[i]
		case "console":
			consoleSvc = &svcs[i]
		case "web":
			webSvc = &svcs[i]
		}
	}

	if edgeSvc == nil || edgeSvc.Category != supervisor.CategoryBackend || edgeSvc.Port != 8787 {
		t.Fatalf("unexpected edge service config: %+v", edgeSvc)
	}
	if consoleSvc == nil || consoleSvc.Category != supervisor.CategoryFrontend || consoleSvc.Port != 5173 {
		t.Fatalf("unexpected console service config: %+v", consoleSvc)
	}
	if webSvc == nil || webSvc.Category != supervisor.CategoryFrontend || webSvc.Port != 3000 {
		t.Fatalf("unexpected web service config: %+v", webSvc)
	}

	// Test Docker discovery in environments/docker/compose.yaml
	_ = afero.WriteFile(fs, "/workspace/environments/docker/compose.yaml", []byte(`
services:
  postgres:
    ports:
      - "5432:5432"
`), 0644)
	_ = afero.WriteFile(fs, "/workspace/environments/docker/.env", []byte("POSTGRES_USER=test\n"), 0644)

	dockerSvcs := discoverServices(fs, "/workspace", cfg, false)
	if len(dockerSvcs) != 4 {
		t.Fatalf("expected 4 services including docker, got %d", len(dockerSvcs))
	}

	var dockerSvc *supervisor.ServiceConfig
	for i := range dockerSvcs {
		if dockerSvcs[i].Name == "docker-infra" {
			dockerSvc = &dockerSvcs[i]
			break
		}
	}
	if dockerSvc == nil {
		t.Fatalf("expected docker-infra service to be discovered")
	}
	if dockerSvc.Port != 5432 {
		t.Fatalf("expected docker-infra port to be 5432, got %d", dockerSvc.Port)
	}
	if dockerSvc.Category != supervisor.CategoryInfra {
		t.Fatalf("expected CategoryInfra for docker")
	}
}
