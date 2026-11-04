package graph

import (
	"testing"

	"ctrldown/cli/internal/config"
	"github.com/spf13/afero"
)

func TestBuildWorkspaceGraph(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Setup mock workspace
	_ = afero.WriteFile(fs, "/workspace/packages/storage/package.json", []byte(`{
		"name": "@nexus/storage",
		"version": "1.0.0"
	}`), 0644)

	_ = afero.WriteFile(fs, "/workspace/packages/auth/package.json", []byte(`{
		"name": "@nexus/auth",
		"version": "1.0.0"
	}`), 0644)

	_ = afero.WriteFile(fs, "/workspace/domains/console/package.json", []byte(`{
		"name": "@nexus/console",
		"dependencies": {
			"@nexus/storage": "workspace:*"
		}
	}`), 0644)

	_ = afero.WriteFile(fs, "/workspace/domains/edge/package.json", []byte(`{
		"name": "@nexus/edge",
		"dependencies": {
			"@nexus/storage": "workspace:*",
			"@nexus/auth": "workspace:*"
		}
	}`), 0644)

	ctrlCfg := &config.CtrlConfig{
		Features: []string{"ctrlweb"},
		Backend: &config.BackendConfig{
			Path: "domains/api",
		},
	}

	g, err := BuildWorkspaceGraph(fs, "/workspace", ctrlCfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(g.Nodes) < 3 {
		t.Fatalf("expected at least 3 nodes (2 packages + 1 feature), got %d", len(g.Nodes))
	}

	var storageNode *PackageNode
	for i := range g.Nodes {
		if g.Nodes[i].Name == "@nexus/storage" {
			storageNode = &g.Nodes[i]
			break
		}
	}

	if storageNode == nil {
		t.Fatalf("@nexus/storage node not found")
	}

	if len(storageNode.Consumers) != 2 {
		t.Fatalf("expected @nexus/storage to have 2 consumers (console & edge), got %d", len(storageNode.Consumers))
	}
}
