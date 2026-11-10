package guard

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// staticcheckIntermediateRepresentation is the import paths of staticcheck's intermediate representation: go/ir, its
// helpers in irutil, and the buildir pass that builds it on every package.
var staticcheckIntermediateRepresentation = []string{
	"honnef.co/go/tools/go/ir",
	"honnef.co/go/tools/go/ir/irutil",
	"honnef.co/go/tools/internal/passes/buildir",
}

// One program form (#kk4864m): every rule reads the engine's own intermediate representation, so no code of cohere's
// imports staticcheck's, and the run never builds buildir beside ours. Tests are exempt, since the parity harness
// runs staticcheck's originals, which build on go/ir, and the representation's differential tests compare ours with
// it. The walk must read the module's code, so a walk that read too few files to hold it fails rather than finding
// nothing.
func TestNoCodeImportsStaticchecksIntermediateRepresentation(test *testing.T) {
	test.Parallel()
	root, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	importers, read, readError := staticcheckIntermediateRepresentationImporters(root)
	if readError != nil {
		test.Fatal(readError)
	}
	if read < 1000 {
		test.Fatalf("the walk read %d Go files under %s, too few to be the module's code", read, root)
	}
	for _, importer := range importers {
		test.Errorf("%s imports %s, staticcheck's intermediate representation: read the engine's own, "+
			"internal/go_engine/intermediate_representation, through dispatch.IntermediateRepresentation or "+
			"build_intermediate_representation", importer.file, importer.importPath)
	}
}

// staticcheckIntermediateRepresentationImport is one import of staticcheck's intermediate representation: the file,
// relative to the module's root, and the path it imports.
type staticcheckIntermediateRepresentationImport struct {
	file       string
	importPath string
}

// staticcheckIntermediateRepresentationImporters is every import of staticcheck's intermediate representation by a
// Go file under root that is not a test, outside the vendored TypeScript, hidden directories and fixtures, read from
// each file's imports alone, and how many files it read.
func staticcheckIntermediateRepresentationImporters(
	root string,
) (importers []staticcheckIntermediateRepresentationImport, read int, walkError error) {
	files := token.NewFileSet()
	walkError = filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		native, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		relative := filepath.ToSlash(native)
		if entry.IsDir() {
			if relative != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" ||
				relative == "TypeScript") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseError := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		if parseError != nil {
			return parseError
		}
		read++
		for _, spec := range parsed.Imports {
			importPath, unquoteError := strconv.Unquote(spec.Path.Value)
			if unquoteError != nil {
				return fmt.Errorf("reading an import path in %s: %w", path, unquoteError)
			}
			if slices.Contains(staticcheckIntermediateRepresentation, importPath) {
				importers = append(importers, staticcheckIntermediateRepresentationImport{
					file: relative, importPath: importPath,
				})
			}
		}
		return nil
	})
	return importers, read, walkError
}

// The walk finds each of staticcheck's import paths, one plant at a time: a module holding one file that imports only
// that path reads as one importer of it, so dropping any path from staticcheckIntermediateRepresentation leaves its
// plant unfound and fails here, where the module's own walk, finding nothing today, would stay green.
func TestTheImportWalkFindsEachOfStaticchecksPaths(test *testing.T) {
	test.Parallel()
	for _, importPath := range []string{
		"honnef.co/go/tools/go/ir",
		"honnef.co/go/tools/go/ir/irutil",
		"honnef.co/go/tools/internal/passes/buildir",
	} {
		test.Run(importPath, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			planted := "package plant\n\nimport _ " + strconv.Quote(importPath) + "\n"
			if writeError := os.WriteFile(filepath.Join(root, "plant.go"), []byte(planted), 0o600); writeError != nil {
				test.Fatal(writeError)
			}
			importers, read, readError := staticcheckIntermediateRepresentationImporters(root)
			if readError != nil {
				test.Fatal(readError)
			}
			want := []staticcheckIntermediateRepresentationImport{{file: "plant.go", importPath: importPath}}
			if read != 1 || !slices.Equal(importers, want) {
				test.Fatalf("a module importing only %s read %d files and found %v, want 1 file and %v", importPath,
					read, importers, want)
			}
		})
	}
}
