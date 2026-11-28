// Package concurrency_tests says which of cohere's tests run code at once, by the words the house gives them: the
// tests the land gate runs again under -race (command/cohere-dev's race_gated.go). It reads the source alone. The guard
// that every such test's package is on the -race list walks the whole module with it from internal/testing/guard,
// whose tests read the whole tree by design, and command/cohere-dev's tests read single packages with it, so neither
// package reruns for a change it doesn't read (#0zdyk51).
package concurrency_tests

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

// RaceGatedDirectories are the directories the -race list names in source, command/cohere-dev's race_gated.go, in
// order: each entry's `directory` field of the one `var raceGated = []raceEntry{...}`. It refuses, by what it found, a
// source with no such list or two, a list spec naming more than raceGated, a list that is no literal, an entry that is
// no literal, and an entry whose directory is no string literal, so the guard reading it never checks the module
// against less than the list says. command/cohere-dev's own test holds what it reads equal to the list the gate runs.
// source is the file's text, or nil to read file itself.
func RaceGatedDirectories(file string, source []byte) ([]string, error) {
	// The parser reads file only when handed an untyped nil: a nil slice would parse as an empty source.
	var parsed *ast.File
	var parseError error
	if source == nil {
		parsed, parseError = parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	} else {
		parsed, parseError = parser.ParseFile(token.NewFileSet(), file, source, parser.SkipObjectResolution)
	}
	if parseError != nil {
		return nil, parseError
	}
	var list *ast.CompositeLit
	for _, declaration := range parsed.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue || !slices.ContainsFunc(value.Names, func(name *ast.Ident) bool {
				return name.Name == "raceGated"
			}) {
				continue
			}
			if len(value.Names) != 1 || len(value.Values) != 1 {
				return nil, fmt.Errorf("%s declares raceGated beside another name or with no single value", file)
			}
			if list != nil {
				return nil, fmt.Errorf("%s declares raceGated twice", file)
			}
			literal, isLiteral := value.Values[0].(*ast.CompositeLit)
			if !isLiteral {
				return nil, fmt.Errorf("%s's raceGated is no list literal", file)
			}
			list = literal
		}
	}
	if list == nil {
		return nil, fmt.Errorf("%s declares no raceGated", file)
	}
	directories := []string{}
	for _, element := range list.Elts {
		entry, isEntry := element.(*ast.CompositeLit)
		if !isEntry {
			return nil, fmt.Errorf("%s's raceGated holds an entry that is no literal", file)
		}
		directory, directoryError := entryDirectory(entry)
		if directoryError != nil {
			return nil, fmt.Errorf("%s's raceGated: %w", file, directoryError)
		}
		directories = append(directories, directory)
	}
	if len(directories) == 0 {
		return nil, fmt.Errorf("%s's raceGated names no directory", file)
	}
	return directories, nil
}

// errorNoDirectory is an entry of the -race list with no `directory` written as a string literal.
var errorNoDirectory = errors.New("an entry has no directory written as a string literal")

// entryDirectory is the string literal entry's `directory` field holds, or errNoDirectory.
func entryDirectory(entry *ast.CompositeLit) (string, error) {
	for _, field := range entry.Elts {
		keyed, isKeyed := field.(*ast.KeyValueExpr)
		if !isKeyed {
			continue
		}
		key, isIdentifier := keyed.Key.(*ast.Ident)
		if !isIdentifier || key.Name != "directory" {
			continue
		}
		literal, isLiteral := keyed.Value.(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			return "", errorNoDirectory
		}
		return strconv.Unquote(literal.Value)
	}
	return "", errorNoDirectory
}
