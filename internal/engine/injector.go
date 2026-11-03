package engine

import (
	"fmt"
	"go/token"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"
)

// InjectGoAST reads a base Go file and a mixin Go file, merges the imports and AST declarations
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

	// Merge imports and declarations separately so import declarations stay at the top of the file
	for _, decl := range mixinAST.Decls {
		if genDecl, ok := decl.(*dst.GenDecl); ok && genDecl.Tok == token.IMPORT {
			for _, spec := range genDecl.Specs {
				if impSpec, ok := spec.(*dst.ImportSpec); ok && impSpec.Path != nil {
					rawPath, err := strconv.Unquote(impSpec.Path.Value)
					if err == nil {
						ensureImport(baseAST, rawPath)
					}
				}
			}
			continue
		}
		baseAST.Decls = append(baseAST.Decls, decl)
	}

	return writeASTFile(fs, baseFilePath, baseAST)
}

// InjectGoEchoRouteAST injects a module's import and RegisterRoutes statement into internal/server/routes.go.
func InjectGoEchoRouteAST(fs afero.Fs, routesFilePath string, importPath string, stmtCode string) error {
	var imports []string
	if importPath != "" {
		imports = append(imports, importPath)
	}
	return injectStmtIntoFunc(fs, routesFilePath, "RegisterRoutes", imports, stmtCode, false)
}

// InjectGoEchoRouteASTWithImports injects multiple imports and a RegisterRoutes statement into internal/server/routes.go.
func InjectGoEchoRouteASTWithImports(fs afero.Fs, routesFilePath string, imports []string, stmtCode string) error {
	return injectStmtIntoFunc(fs, routesFilePath, "RegisterRoutes", imports, stmtCode, false)
}

// InjectGoEchoMigrateAST injects a module's import and AutoMigrate call into cmd/api/main.go before server startup.
func InjectGoEchoMigrateAST(fs afero.Fs, mainFilePath string, importPath string, stmtCode string) error {
	var imports []string
	if importPath != "" {
		imports = append(imports, importPath)
	}
	return injectStmtIntoFunc(fs, mainFilePath, "main", imports, stmtCode, true)
}

func injectStmtIntoFunc(
	fs afero.Fs,
	filePath string,
	funcName string,
	importPaths []string,
	stmtCode string,
	beforeServerNew bool,
) error {
	fileBytes, err := afero.ReadFile(fs, filePath)
	if err != nil {
		return fmt.Errorf("could not read Go file %s: %w", filePath, err)
	}

	// Avoid duplicate injection if statement already exists in the file
	if strings.Contains(string(fileBytes), strings.TrimSpace(stmtCode)) {
		return nil
	}

	fileAST, err := decorator.Parse(fileBytes)
	if err != nil {
		return fmt.Errorf("could not parse Go AST %s: %w", filePath, err)
	}

	for _, imp := range importPaths {
		if imp != "" {
			ensureImport(fileAST, imp)
		}
	}

	stmt, err := parseSingleStmt(stmtCode)
	if err != nil {
		return fmt.Errorf("could not parse statement %q: %w", stmtCode, err)
	}

	injected := false
	for _, decl := range fileAST.Decls {
		fn, ok := decl.(*dst.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != funcName || fn.Body == nil {
			continue
		}

		if beforeServerNew {
			insertIdx := len(fn.Body.List)
			for i, existing := range fn.Body.List {
				if isServerNewAssign(existing) {
					insertIdx = i
					break
				}
			}
			newList := make([]dst.Stmt, 0, len(fn.Body.List)+1)
			newList = append(newList, fn.Body.List[:insertIdx]...)
			newList = append(newList, stmt)
			newList = append(newList, fn.Body.List[insertIdx:]...)
			fn.Body.List = newList
		} else {
			fn.Body.List = append(fn.Body.List, stmt)
		}
		injected = true
		break
	}

	if !injected {
		return fmt.Errorf("target function %q not found in %s", funcName, filePath)
	}

	return writeASTFile(fs, filePath, fileAST)
}

func ensureImport(fileAST *dst.File, importPath string) {
	quoted := strconv.Quote(importPath)

	var firstImportDecl *dst.GenDecl
	for _, decl := range fileAST.Decls {
		gen, ok := decl.(*dst.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		if firstImportDecl == nil {
			firstImportDecl = gen
		}
		for _, spec := range gen.Specs {
			imp, ok := spec.(*dst.ImportSpec)
			if ok && imp.Path != nil && imp.Path.Value == quoted {
				return // Already imported
			}
		}
	}

	newSpec := &dst.ImportSpec{
		Path: &dst.BasicLit{
			Kind:  token.STRING,
			Value: quoted,
		},
	}

	if firstImportDecl != nil {
		firstImportDecl.Specs = append(firstImportDecl.Specs, newSpec)
		return
	}

	importDecl := &dst.GenDecl{
		Tok:   token.IMPORT,
		Specs: []dst.Spec{newSpec},
	}
	fileAST.Decls = append([]dst.Decl{importDecl}, fileAST.Decls...)
}

func parseSingleStmt(stmtCode string) (dst.Stmt, error) {
	wrapperSrc := fmt.Sprintf("package p\nfunc _() {\n\t%s\n}\n", stmtCode)
	parsed, err := decorator.Parse(wrapperSrc)
	if err != nil {
		return nil, err
	}
	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok && fn.Body != nil && len(fn.Body.List) > 0 {
			return dst.Clone(fn.Body.List[0]).(dst.Stmt), nil
		}
	}
	return nil, fmt.Errorf("no statement found in %q", stmtCode)
}

func isServerNewAssign(stmt dst.Stmt) bool {
	assign, ok := stmt.(*dst.AssignStmt)
	if !ok || len(assign.Rhs) == 0 {
		return false
	}
	call, ok := assign.Rhs[0].(*dst.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*dst.Ident)
	return ok && ident.Name == "server" && sel.Sel != nil && sel.Sel.Name == "New"
}

func writeASTFile(fs afero.Fs, filePath string, fileAST *dst.File) error {
	f, err := fs.Create(filePath)
	if err != nil {
		return fmt.Errorf("could not open file for writing %s: %w", filePath, err)
	}
	defer f.Close()

	if err := decorator.Fprint(f, fileAST); err != nil {
		return fmt.Errorf("could not write AST to file %s: %w", filePath, err)
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

