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
}
