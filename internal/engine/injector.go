package engine

import (
	"fmt"
	"strings"

	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
)

// InjectGoAST reads a base Go file and a mixin Go file, merges the AST declarations
// from the mixin into the base, and writes the base file back.
func InjectGoAST(fs afero.Fs, baseFilePath string, mixinFilePath string) error {
	// Read base file
	baseFileBytes, err := afero.ReadFile(fs, baseFilePath)
	if err != nil {
		return fmt.Errorf("could not read base file %s: %w", baseFilePath, err)
	}

	// Read mixin file
	mixinFileBytes, err := afero.ReadFile(fs, mixinFilePath)
	if err != nil {
		return fmt.Errorf("could not read mixin file %s: %w", mixinFilePath, err)
	}

	// Parse base AST
	baseAST, err := decorator.Parse(baseFileBytes)
	if err != nil {
		return fmt.Errorf("could not parse base AST %s: %w", baseFilePath, err)
	}

	// Parse mixin AST
	mixinAST, err := decorator.Parse(mixinFileBytes)
	if err != nil {
		return fmt.Errorf("could not parse mixin AST %s: %w", mixinFilePath, err)
	}

	// Simple Merge Strategy: Append all declarations from mixin to base.
	// In a more advanced implementation, we would check for comments like // CTRLDOWN_INJECT_DB
	// or handle specific imports via dst.ImportSpec.
	// For now, we append the declarations.
	baseAST.Decls = append(baseAST.Decls, mixinAST.Decls...)

	// Open base file for writing
	f, err := fs.Create(baseFilePath)
	if err != nil {
		return fmt.Errorf("could not open base file for writing %s: %w", baseFilePath, err)
	}
	defer f.Close()

	// Restore modified AST back to base file
	if err := decorator.Fprint(f, baseAST); err != nil {
		return fmt.Errorf("could not write AST to base file %s: %w", baseFilePath, err)
	}

	return nil
}

// InjectTSImport reads a mixin TS/TSX file, extracts all import statements,
// and prepends them to the base TS/TSX file.
func InjectTSImport(fs afero.Fs, baseFilePath string, mixinFilePath string) error {
	baseBytes, err := afero.ReadFile(fs, baseFilePath)
	if err != nil {
		return fmt.Errorf("could not read base TS file %s: %w", baseFilePath, err)
	}

	mixinBytes, err := afero.ReadFile(fs, mixinFilePath)
	if err != nil {
		return fmt.Errorf("could not read mixin TS file %s: %w", mixinFilePath, err)
	}

	mixinContent := string(mixinBytes)
	lines := strings.Split(mixinContent, "\n")
	
	var imports []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "import ") {
			imports = append(imports, line)
		}
	}

	if len(imports) == 0 {
		return nil // Nothing to inject
	}

	importsStr := strings.Join(imports, "\n") + "\n"
	
	// Prepend imports to the base file
	newBaseContent := importsStr + string(baseBytes)

	if err := afero.WriteFile(fs, baseFilePath, []byte(newBaseContent), 0644); err != nil {
		return fmt.Errorf("could not write injected TS file %s: %w", baseFilePath, err)
	}

	return nil
}
