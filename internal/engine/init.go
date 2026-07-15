package engine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// BootstrapFeature copies the local boilerplate environment to the target path using Afero.
func BootstrapFeature(fileSystem afero.Fs, sourcePath, destPath string) error {
	// First check if source exists
	exists, err := afero.Exists(fileSystem, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to check source path: %w", err)
	}
	if !exists {
		return fmt.Errorf("boilerplate source path does not exist: %s", sourcePath)
	}

	// Recursively copy directories and files
	err = afero.Walk(fileSystem, sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path to construct destination path
		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		
		target := filepath.Join(destPath, relPath)

		if info.IsDir() {
			return fileSystem.MkdirAll(target, info.Mode())
		}

		// Copy file
		return copyFile(fileSystem, path, target)
	})

	if err != nil {
		return fmt.Errorf("failed to copy boilerplate files: %w", err)
	}

	return nil
}

func copyFile(fileSystem afero.Fs, src, dst string) error {
	sourceFile, err := fileSystem.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := fileSystem.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return nil
}
