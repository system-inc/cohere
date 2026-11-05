package gocache

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// gocacheImport is this package's import path, which a caller of Trim imports.
const gocacheImport = "github.com/system-inc/cohere/internal/release/gocache"

// trimCaller is the one directory whose code may call Trim on a live cache: cohere-dev's, which holds every
// token of the machine's pool while it trims.
const trimCaller = "command/cohere-dev"

// One trim, one path, under the pool's lock (#cpxc2d7). The launcher also trimmed the default cache, holding
// nothing, and on 2026-10-06 a trim holding the wrong pool deleted 83 GB from under running gates
// (#d0x1fhp). So no code outside cohere-dev calls Trim. Tests are exempt: they trim caches of their own.
func TestOnlyCohereDevTrims(test *testing.T) {
	test.Parallel()
	root, absolutePathError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	callers := []string{}
	files := token.NewFileSet()
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			// The vendored compiler, caches and fixtures hold no caller of ours.
			if relative != "." &&
				(strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" || relative == "TypeScript") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") ||
			strings.HasPrefix(relative, trimCaller+"/") {
			return nil
		}
		imports, parseFileError := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		if parseFileError != nil {
			return parseFileError
		}
		name := ""
		for _, spec := range imports.Imports {
			if importPath, _ := strconv.Unquote(spec.Path.Value); importPath == gocacheImport {
				name = "gocache"
				if spec.Name != nil {
					name = spec.Name.Name
				}
			}
		}
		if name == "" {
			return nil
		}
		parsed, parseFileError := parser.ParseFile(files, path, nil, 0)
		if parseFileError != nil {
			return parseFileError
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok && selector.Sel.Name == "Trim" {
				if identifier, ok := selector.X.(*ast.Ident); ok && identifier.Name == name {
					callers = append(callers, files.Position(selector.Pos()).String())
				}
			}
			return true
		})
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	for _, caller := range callers {
		test.Errorf(
			"%s calls gocache.Trim outside %s: only cohere-dev's trim may delete from a live Go cache, "+
				"holding the pool's lock; ask it (cohere-dev trim-cache) rather than trim beside the builds",
			caller,
			trimCaller,
		)
	}
}
