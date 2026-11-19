package build_reads

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// useWalkSources are standard-library packages whose syntax holds every kind of expression the walk places, checked
// from source with every package they import: a parser, a printer, reflection, templates, encodings and http.
var useWalkSources = []string{"go/parser", "go/printer", "reflect", "text/template", "encoding/json", "net/http"}

// A walk that lets each context go once its expression is reached reads exactly what one that keeps every context
// reads (#954dadc): over the standard library's packages named in useWalkSources and every one they import, checked
// from source, both walks collect the same uses, selections and closure-only reason, and the walk that lets them go
// ends holding a small share of the contexts it set. The second holds only if no expression's context is read twice,
// since a context let go and read again would read empty and record otherwise.
func TestAUseWalkThatLetsContextsGoReadsAsOneThatKeepsThem(test *testing.T) {
	test.Parallel()

	sources := checkedStandardLibrary(test, useWalkSources)
	set := 0
	held := 0
	for own, source := range sources {
		lettingGo := typeReads{uses: map[ObjectUse]bool{}, selections: map[selectionIdentity]Selection{}}
		keeping := typeReads{uses: map[ObjectUse]bool{}, selections: map[selectionIdentity]Selection{}}
		walked := walkUsesKeeping(&lettingGo, own, source.info, source.files, false, true)
		kept := walkUsesKeeping(&keeping, own, source.info, source.files, true, true)
		if !reflect.DeepEqual(lettingGo, keeping) {
			test.Errorf("%s: the walk letting its contexts go collected %d uses and %d selections, the walk keeping "+
				"them %d and %d", own.Path(), len(lettingGo.uses), len(lettingGo.selections), len(keeping.uses),
				len(keeping.selections))
		}
		set += len(kept.contexts)
		held += len(walked.contexts)
	}
	test.Logf("%d packages checked, %d contexts set, %d held at the walks' ends", len(sources), set, held)
	if len(sources) < 100 || set == 0 {
		test.Fatalf("checked %d packages setting %d contexts, too few to read", len(sources), set)
	}
	if held*20 > set {
		test.Errorf("the walk letting its contexts go ended holding %d of the %d it set, want under a twentieth", held,
			set)
	}
}

// checkedSource is one package checked from source: its syntax and its type information, every map the walks read.
type checkedSource struct {
	files []*ast.File
	info  *types.Info
}

// checkedStandardLibrary is each standard-library package of paths, and every one they import, checked from source.
func checkedStandardLibrary(test *testing.T, paths []string) map[*types.Package]checkedSource {
	test.Helper()

	// The test binary is built with -trimpath, so it knows no `GOROOT` of its own: go env names it.
	goRoot, goEnvironmentError := exec.Command("go", "env", "GOROOT").Output()
	if goEnvironmentError != nil {
		test.Fatalf("reading GOROOT with go env: %v", goEnvironmentError)
	}
	context := build.Default
	context.GOROOT, context.CgoEnabled = strings.TrimSpace(string(goRoot)), false
	fileSet := token.NewFileSet()
	checked := map[string]*types.Package{}
	sources := map[*types.Package]checkedSource{}
	var check func(path string, directory string) (*types.Package, error)
	check = func(path string, directory string) (*types.Package, error) {
		if path == "unsafe" {
			return types.Unsafe, nil
		}
		// A path resolves from the importing package's directory, as go/build resolves std's vendored packages.
		listed, importError := context.Import(path, directory, 0)
		if importError != nil {
			return nil, importError
		}
		if done, isDone := checked[listed.ImportPath]; isDone {
			return done, nil
		}
		var files []*ast.File
		for _, name := range listed.GoFiles {
			file, parseError := parser.ParseFile(fileSet, filepath.Join(listed.Dir, name), nil,
				parser.SkipObjectResolution)
			if parseError != nil {
				return nil, parseError
			}
			files = append(files, file)
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{},
			Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
			Implicits: map[ast.Node]types.Object{}, Instances: map[*ast.Ident]types.Instance{}}
		configuration := types.Config{Importer: importerFrom(check), Sizes: types.SizesFor("gc", context.GOARCH),
			FakeImportC: true}
		checkedTypes, checkError := configuration.Check(listed.ImportPath, fileSet, files, info)
		if checkError != nil {
			return nil, checkError
		}
		checked[listed.ImportPath] = checkedTypes
		sources[checkedTypes] = checkedSource{files: files, info: info}
		return checkedTypes, nil
	}
	for _, path := range paths {
		if _, checkError := check(path, ""); checkError != nil {
			test.Fatalf("checking %s from source: %v", path, checkError)
		}
	}
	return sources
}

// importerFrom is a types.ImporterFrom of a function, which resolves an import from the importing package's directory.
type importerFrom func(path string, directory string) (*types.Package, error)

// Import is ImportFrom from no directory.
func (importing importerFrom) Import(path string) (*types.Package, error) { return importing(path, "") }

// ImportFrom is the package at path, as the package in directory imports it.
func (importing importerFrom) ImportFrom(path string, directory string, _ types.ImportMode) (*types.Package, error) {
	return importing(path, directory)
}
