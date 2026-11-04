package tui

import (
	"strings"
	"testing"

	"ctrldown/cli/internal/dev/graph"
	"ctrldown/cli/internal/dev/supervisor"
)

func TestTUIModelRender(t *testing.T) {
	sup := supervisor.NewSupervisor([]supervisor.ServiceConfig{
		{
			Name:     "postgres",
			Category: supervisor.CategoryInfra,
			Port:     5432,
		},
		{
			Name:     "api",
			Category: supervisor.CategoryBackend,
			Port:     8080,
		},
		{
			Name:     "console",
			Category: supervisor.CategoryFrontend,
			Port:     5173,
		},
	})

	g := &graph.WorkspaceGraph{
		Nodes: []graph.PackageNode{
			{
				Name: "@nexus/storage",
				Consumers: []graph.ConsumerLink{
					{DomainName: "edge", LinkType: "workspace:*"},
				},
			},
		},
	}

	model := NewModel(sup, g, "nexus-test")
	model.width = 120
	model.height = 36

	view := model.View()
	if !strings.Contains(view, "ctrldown dev") {
		t.Fatalf("expected view to contain 'ctrldown dev'")
	}
	if !strings.Contains(view, "INFRASTRUCTURE") {
		t.Fatalf("expected view to contain 'INFRASTRUCTURE'")
	}
	if !strings.Contains(view, "BACKEND") {
		t.Fatalf("expected view to contain 'BACKEND'")
	}
	if !strings.Contains(view, "FRONTEND") {
		t.Fatalf("expected view to contain 'FRONTEND'")
	}

	// Switch to Graph tab
	model.activeTab = TabGraph
	graphView := model.View()
	if !strings.Contains(graphView, "MONOREPO PACKAGES & AST LINKAGES MAP") {
		t.Fatalf("expected graph view header")
	}
	if !strings.Contains(graphView, "@nexus/storage") {
		t.Fatalf("expected graph view to contain @nexus/storage")
	}
}
