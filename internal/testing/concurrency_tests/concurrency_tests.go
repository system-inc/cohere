// Package concurrency_tests says which of cohere's tests run code at once, by the words the house gives them: the
// tests the land gate runs again under -race (command/cohere-dev's race_gated.go). It reads the source alone. The guard
// that every such test's package is on the -race list walks the whole module with it from internal/testing/guard,
// whose tests read the whole tree by design, and command/cohere-dev's tests read single packages with it, so neither
// package reruns for a change it doesn't read (#0zdyk51).
package concurrency_tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Name is a test whose name says it runs code at once: side by side, as extra builds and graphs run, and beside the
// load, as the lint and a run's passes do, are cohere's words for it (#2xddz1z).
var Name = regexp.MustCompile(`^Test\w*(Concurrent|AtOnce|InParallel|Simultaneous|SideBySide|BesideTheLoad)`)

// File is a test file whose name says its tests run code at once.
var File = regexp.MustCompile(`(_concurrency_test\.go|^parallel_\w*_test\.go|_parallel_test\.go)$`)

// InFile is every top-level test in the test file at path that runs code at once: each whose name says so (Name), and
// every one when the file's name says its tests do (File).
func InFile(path string) ([]string, error) {
	parsed, parseError := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if parseError != nil {
		return nil, parseError
	}
	wholeFile := File.MatchString(filepath.Base(path))
	tests := []string{}
	for _, declaration := range parsed.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") {
			continue
		}
		if wholeFile || Name.MatchString(function.Name.Name) {
			tests = append(tests, function.Name.Name)
		}
	}
	return tests, nil
}

// Directories is every directory under root, by its path from root with slashes, holding a test that runs code at
// once, with the first such test or file. It leaves out testdata, nested modules, the TypeScript checkout and hidden
// directories, none of which the gate tests as cohere's module.
func Directories(root string) (map[string]string, error) {
	found := map[string]string{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && (name == "testdata" || name == "TypeScript" || strings.HasPrefix(name, ".") ||
				strings.HasPrefix(name, "_") || holdsModule(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, "_test.go") {
			return nil
		}
		relative, relativeError := filepath.Rel(root, filepath.Dir(path))
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if _, seen := found[relative]; seen {
			return nil
		}
		if File.MatchString(name) {
			found[relative] = name
			return nil
		}
		tests, testsError := InFile(path)
		if testsError != nil {
			return testsError
		}
		if len(tests) > 0 {
			found[relative] = tests[0]
		}
		return nil
	})
	if walkError != nil {
		return nil, walkError
	}
	return found, nil
}

// holdsModule is whether directory holds a go.mod of its own, which makes it a module apart from cohere's.
func holdsModule(directory string) bool {
	_, statError := os.Stat(filepath.Join(directory, "go.mod"))
	return statError == nil
}
