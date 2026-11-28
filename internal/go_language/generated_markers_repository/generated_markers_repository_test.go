package generated_markers_repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_language/generated_markers"
)

// staticcheckGeneratedRun is generated.Analyzer's Run as this test binary initializes it, which is staticcheck's own:
// only check replaces it, and this package never imports check. A test binary that linked check would find check's
// Run here and compare the scanner with itself, which staticcheckJudgments refuses.
var staticcheckGeneratedRun = generated.Analyzer.Run

// staticcheckPackage is the package staticcheck's own Run is declared in, which begins the name the runtime gives it.
const staticcheckPackage = "honnef.co/go/tools/analysis/facts/generated."

// staticcheckJudgments runs staticcheck's own generated.Analyzer Run over the files at paths, each parsed to its
// package clause, and generated_markers.ReadGeneratedMarkers over the same, answering both results. It and
// expectSameJudgments copy generated_markers_test.go's own, on purpose: exporting them from generated_markers would put
// test code in a production package for one caller.
func staticcheckJudgments(test *testing.T, paths []string) (map[string]generated.Generator,
	map[string]generated.Generator) {
	test.Helper()
	// A function compares only to nil, so the Run is known by the name the runtime gives the code it points at.
	if name := runtime.FuncForPC(reflect.ValueOf(staticcheckGeneratedRun).Pointer()).Name(); !strings.HasPrefix(name,
		staticcheckPackage) {
		test.Fatalf("generated.Analyzer's Run is %s, not staticcheck's: something in this test binary links check, "+
			"which replaces it, so the scanner would be compared with itself", name)
	}
	fileSet := token.NewFileSet()
	files := make([]*ast.File, 0, len(paths))
	for _, path := range paths {
		file, parseError := parser.ParseFile(fileSet, path, nil, parser.PackageClauseOnly)
		if parseError != nil {
			// A file whose package clause does not parse is in no pass, so neither Run ever judges it.
			continue
		}
		files = append(files, file)
	}
	pass := &analysis.Pass{Fset: fileSet, Files: files}
	staticcheckResult, staticcheckError := staticcheckGeneratedRun(pass)
	if staticcheckError != nil {
		test.Fatalf("staticcheck's Run: %v", staticcheckError)
	}
	staticcheckGenerators, staticcheckTyped := staticcheckResult.(map[string]generated.Generator)
	if !staticcheckTyped {
		test.Fatalf("staticcheck's result is %T, want map[string]generated.Generator", staticcheckResult)
	}
	return staticcheckGenerators, generated_markers.ReadGeneratedMarkers(pass)
}

// expectSameJudgments fails test for every file the two results judge apart.
func expectSameJudgments(test *testing.T, staticcheckGenerators map[string]generated.Generator,
	ourGenerators map[string]generated.Generator) {
	test.Helper()
	for path, generator := range staticcheckGenerators {
		ours, marked := ourGenerators[path]
		if !marked || ours != generator {
			test.Errorf("%s: staticcheck judges generator %v, ReadGeneratedMarkers %v (marked %t)", path, generator,
				ours, marked)
		}
	}
	for path, generator := range ourGenerators {
		if _, marked := staticcheckGenerators[path]; !marked {
			test.Errorf("%s: ReadGeneratedMarkers judges generator %v, staticcheck judges no marker", path, generator)
		}
	}
}

// Every Go file of the repository, testdata's included, is judged by generated_markers.ReadGeneratedMarkers as
// staticcheck judges it.
func TestReadGeneratedMarkersJudgesTheRepositoryAsStaticcheck(test *testing.T) {
	test.Parallel()
	// This package sits three directories below the module root.
	root, rootError := filepath.Abs(filepath.Join("..", "..", ".."))
	if rootError != nil {
		test.Fatalf("resolving the module root: %v", rootError)
	}
	var paths []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() {
			// TypeScript is a submodule of upstream's tree, and node_modules npm's.
			if path != root && (strings.HasPrefix(entry.Name(), ".") || slices.Contains([]string{"TypeScript",
				"node_modules"}, entry.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(entry.Name()) == ".go" {
			paths = append(paths, path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatalf("walking %s: %v", root, walkError)
	}
	staticcheckGenerators, ourGenerators := staticcheckJudgments(test, paths)
	if len(staticcheckGenerators) == 0 {
		test.Fatalf("staticcheck judged none of %d files generated, so the walk tested nothing", len(paths))
	}
	expectSameJudgments(test, staticcheckGenerators, ourGenerators)
}
