package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"ctrldown/cli/internal/config"
)

// InstallComponent orchestrates copying the template, applying mixins (with AST logic), and running hooks.
func InstallComponent(
	fs afero.Fs,
	boilerplatePath string,
	targetPath string,
	template config.TemplateDef,
	mixins []config.MixinDef,
	hooks map[string][]string,
) error {

	// 1. Copy base template
	baseSource := filepath.Join(boilerplatePath, template.Path)
	err := BootstrapFeature(fs, baseSource, targetPath) // Reusing the recursive copy logic from init
	if err != nil {
		return fmt.Errorf("failed to copy base template: %w", err)
	}

	// 2. Apply Mixins
	for _, mixin := range mixins {
		mixinSource := filepath.Join(boilerplatePath, mixin.Path)
		
		err := afero.Walk(fs, mixinSource, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			relPath, err := filepath.Rel(mixinSource, path)
			if err != nil {
				return err
			}

			destFile := filepath.Join(targetPath, relPath)

			// If it's a .mixin file, we trigger AST injection instead of raw copy
			if strings.HasSuffix(info.Name(), ".mixin") {
				// Base file is the destFile without the .mixin extension
				baseFile := strings.TrimSuffix(destFile, ".mixin")
				
				// Apply AST Logic (we only implement Go AST for now via injector.go)
				if template.Language == "go" {
					if err := InjectGoAST(fs, baseFile, path); err != nil {
						return fmt.Errorf("failed to inject AST for %s: %w", baseFile, err)
					}
				}
				return nil
			}

			// Normal file override (mixin > base template)
			destDir := filepath.Dir(destFile)
			if err := fs.MkdirAll(destDir, 0755); err != nil {
				return err
			}
			return copyFile(fs, path, destFile)
		})

		if err != nil {
			return fmt.Errorf("failed to apply mixin %s: %w", mixin.Title, err)
		}
	}

	// 3. Run Post Hooks
	hookKey := fmt.Sprintf("post_generate_%s", template.Language)
	if commands, exists := hooks[hookKey]; exists {
		for _, cmdStr := range commands {
			if err := runCommand(targetPath, cmdStr); err != nil {
				return fmt.Errorf("hook execution failed (%s): %w", cmdStr, err)
			}
		}
	}

	return nil
}

func runCommand(dir string, cmdStr string) error {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return nil
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
