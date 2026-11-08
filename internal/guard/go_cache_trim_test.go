package guard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// goCacheImport is the go_cache module's import path, which a caller of its deletes imports.
const goCacheImport = "github.com/system-inc/cohere/go_cache"

// goCacheDeletes are go_cache's functions that remove from a cache.
var goCacheDeletes = map[string]bool{"Trim": true, "RemoveOldest": true, "Remove": true}

// trimCaller is the one directory whose code may delete from a live cache: cohere-dev's, which holds every
// token of the machine's pool while it trims.
const trimCaller = "command/cohere-dev"

// One trim, one path, under the pool's lock (#cpxc2d7). The launcher also trimmed the default cache, holding
// nothing, and on 2026-10-06 a trim holding the wrong pool deleted 83 GB from under running gates
// (#d0x1fhp). So no code of cohere's outside cohere-dev calls go_cache's deletes. Tests are exempt: they trim
// caches of their own. The go_cache module itself never imports its own path, so its own calls are not read.
func TestOnlyCohereDevTrims(test *testing.T) {
	test.Parallel()
	root, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	callers := []string{}
	files := token.NewFileSet()
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		native, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		relative := filepath.ToSlash(native)
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
		imports, parseImportsError := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		if parseImportsError != nil {
			return parseImportsError
		}
		name := ""
		for _, spec := range imports.Imports {
			importPath, unquoteError := strconv.Unquote(spec.Path.Value)
			if unquoteError != nil {
				return fmt.Errorf("reading an import path in %s: %w", path, unquoteError)
			}
			if importPath == goCacheImport {
				name = "go_cache"
				if spec.Name != nil {
					name = spec.Name.Name
				}
			}
		}
		if name == "" {
			return nil
		}
		parsed, parseError := parser.ParseFile(files, path, nil, 0)
		if parseError != nil {
			return parseError
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok && goCacheDeletes[selector.Sel.Name] {
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
			"%s deletes through go_cache outside %s: only cohere-dev's trim may delete from a live Go cache, "+
				"holding the pool's lock; ask it (cohere-dev trim-cache) rather than trim beside the builds",
			caller,
			trimCaller,
		)
	}
}
