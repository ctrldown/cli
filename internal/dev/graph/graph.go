package graph

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"ctrldown/cli/internal/config"
	"github.com/spf13/afero"
)

// ConsumerLink describes a domain or service consuming a monorepo package.
type ConsumerLink struct {
	DomainName string
	DomainPath string
	LinkType   string // e.g. "workspace:*", "direct import", "Echo RouteGroup"
}

// PackageNode represents a package in packages/ or features/ and its consumers.
type PackageNode struct {
	Name      string
	Path      string
	Kind      string // "TypeScript Package", "Go Module", "Microfeature"
	Version   string
	Consumers []ConsumerLink
	IsDirty   bool
}

// WorkspaceGraph contains the full dependency tree of packages to domains.
type WorkspaceGraph struct {
	RootPath string
	Nodes    []PackageNode
}

type packageJSON struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PeerDependencies map[string]string `json:"peerDependencies"`
}

// BuildWorkspaceGraph inspects ctrl.yaml, packages/, and domains/ to map dependencies.
func BuildWorkspaceGraph(fs afero.Fs, rootDir string, ctrlCfg *config.CtrlConfig) (*WorkspaceGraph, error) {
	wg := &WorkspaceGraph{
		RootPath: rootDir,
	}

	// 1. Scan packages/
	packageMap := make(map[string]*PackageNode)
	packagesDir := filepath.Join(rootDir, "packages")
	if entries, err := afero.ReadDir(fs, packagesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			pkgDir := filepath.Join(packagesDir, entry.Name())
			pkgJSONPath := filepath.Join(pkgDir, "package.json")
			if exists, _ := afero.Exists(fs, pkgJSONPath); exists {
				data, err := afero.ReadFile(fs, pkgJSONPath)
				if err == nil {
					var pj packageJSON
					if err := json.Unmarshal(data, &pj); err == nil && pj.Name != "" {
						relPath, _ := filepath.Rel(rootDir, pkgDir)
						node := &PackageNode{
							Name:    pj.Name,
							Path:    relPath,
							Kind:    "TypeScript Library",
							Version: pj.Version,
						}
						packageMap[pj.Name] = node
					}
				}
			}
		}
	}

	// 2. Scan domains/ to see which packages each domain imports
	domainsDir := filepath.Join(rootDir, "domains")
	if entries, err := afero.ReadDir(fs, domainsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			domainDir := filepath.Join(domainsDir, entry.Name())
			relDomainPath, _ := filepath.Rel(rootDir, domainDir)

			// Check domain package.json
			pkgJSONPath := filepath.Join(domainDir, "package.json")
			if exists, _ := afero.Exists(fs, pkgJSONPath); exists {
				data, err := afero.ReadFile(fs, pkgJSONPath)
				if err == nil {
					var dpj packageJSON
					if err := json.Unmarshal(data, &dpj); err == nil {
						allDeps := make(map[string]string)
						for k, v := range dpj.Dependencies {
							allDeps[k] = v
						}
						for k, v := range dpj.DevDependencies {
							allDeps[k] = v
						}
						for k, v := range dpj.PeerDependencies {
							allDeps[k] = v
						}

						for pkgName, node := range packageMap {
							if verSpec, ok := allDeps[pkgName]; ok {
								linkType := "TypeScript Direct"
								if strings.HasPrefix(verSpec, "workspace:") {
									linkType = "Turborepo workspace:*"
								}
								node.Consumers = append(node.Consumers, ConsumerLink{
									DomainName: entry.Name(),
									DomainPath: relDomainPath,
									LinkType:   linkType,
								})
							}
						}
					}
				}
			}
		}
	}

	// 3. Scan Microfeatures from ctrl.yaml features and feature_locks
	if ctrlCfg != nil {
		for _, feat := range ctrlCfg.Features {
			tier := "Free"
			if lock, ok := ctrlCfg.FeatureLocks[feat]; ok && lock.Version != "" {
				tier = "v" + lock.Version
			}
			featNode := PackageNode{
				Name:    "Feature: " + feat,
				Path:    "features/" + feat,
				Kind:    "Microfeature (" + tier + ")",
				Version: tier,
			}

			// In monolith architecture, microfeatures link to backend Go Echo API
			if ctrlCfg.Backend != nil && ctrlCfg.Backend.Path != "" {
				featNode.Consumers = append(featNode.Consumers, ConsumerLink{
					DomainName: ctrlCfg.Backend.Path,
					DomainPath: ctrlCfg.Backend.Path,
					LinkType:   "Echo RouteGroup (/v1/" + feat + ")",
				})
			}
			wg.Nodes = append(wg.Nodes, featNode)
		}
	}

	// Collect all nodes into graph
	for _, node := range packageMap {
		wg.Nodes = append(wg.Nodes, *node)
	}

	return wg, nil
}
