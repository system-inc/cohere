package guard

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
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

// notYetMoved is each directory whose code still reads staticcheck's intermediate representation, with why. The
// duplicates report's hash moves onto the engine's own in parallel with #kk4864m, and staticcheck_ir is what it reads
// buildir through until then; both leave this list in the merge that moves it. The guard fails for a directory listed
// here that no longer imports any of them, so the list cannot outlive its reason.
var notYetMoved = map[string]string{
	"internal/go_engine/canonical_function_hash": "hashes staticcheck_ir's functions for `cohere --duplicates`",
	"internal/go_engine/rules/staticcheck_ir":    "hands canonical_function_hash the functions buildir built",
}

// One program form (#kk4864m): every rule reads the engine's own intermediate representation, so no code of cohere's
// imports staticcheck's, and the run never builds buildir beside ours. Tests are exempt, since the parity harness
// runs staticcheck's originals, which build on go/ir, and the representation's differential tests compare ours with
// it. third_party holds staticcheck itself.
func TestNoCodeImportsStaticchecksIntermediateRepresentation(test *testing.T) {
	test.Parallel()
	root, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	importers, readError := staticcheckIntermediateRepresentationImporters(root)
	if readError != nil {
		test.Fatal(readError)
	}
	importing := map[string]bool{}
	for _, importer := range importers {
		directory := filepath.ToSlash(filepath.Dir(importer.file))
		importing[directory] = true
		if notYetMoved[directory] != "" {
			continue
		}
		test.Errorf("%s imports %s, staticcheck's intermediate representation: read the engine's own, "+
			"internal/go_engine/intermediate_representation, through dispatch.IntermediateRepresentation or "+
			"build_intermediate_representation", importer.file, importer.importPath)
	}
	for directory, reason := range notYetMoved {
		if !importing[directory] {
			test.Errorf("%s is listed as not yet moved (%s), and imports none of staticcheck's intermediate "+
				"representation: take it off notYetMoved", directory, reason)
		}
	}
}

// staticcheckIntermediateRepresentationImport is one import of staticcheck's intermediate representation: the file,
// relative to the module's root, and the path it imports.
type staticcheckIntermediateRepresentationImport struct {
	file       string
	importPath string
}

// staticcheckIntermediateRepresentationImporters is every import of staticcheck's intermediate representation by a
// Go file under root that is not a test, outside third_party, the vendored TypeScript, hidden directories and
// fixtures, read from each file's imports alone.
func staticcheckIntermediateRepresentationImporters(
	root string,
) ([]staticcheckIntermediateRepresentationImport, error) {
	importers := []staticcheckIntermediateRepresentationImport{}
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
			if relative != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" ||
				relative == "TypeScript" || relative == "third_party") {
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
	return importers, walkError
}
