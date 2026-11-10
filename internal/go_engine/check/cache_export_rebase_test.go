package check

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/gcexportdata"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// rebasedPath and rebasedDependencyPath are the package the rename cases write the export data of and the package it
// imports, whose declarations the export data carries since the first's API names them.
const (
	rebasedPath           = "example.com/rebased"
	rebasedDependencyPath = "example.com/rebased/dependency"
)

// rebasedSource and rebasedDependencySource are the two packages, with declarations on lines and columns other than
// the first, so a position that loses its line shows.
const (
	rebasedSource = "package rebased\n\nimport \"example.com/rebased/dependency\"\n\n" +
		"// Holder holds an item.\ntype Holder struct {\n\tItem  dependency.Item\n\tCount int\n}\n\n" +
		"func (holder *Holder) Name() string { return holder.Item.Name }\n\n" +
		"func Make(name string) Holder { return Holder{Item: dependency.Item{Name: name}} }\n\n" +
		"var Default = Make(\"default\")\n"
	rebasedDependencySource = "package dependency\n\n\n// Item is named.\ntype Item struct {\n\tName  string\n" +
		"\tLabel string\n}\n\nfunc (item Item) Labelled() string { return item.Label }\n"
)

// rebasedExportData is the export data of rebasedPath as a run would write it, from its source, its file at
// file and its dependency's at dependencyFile.
func rebasedExportData(test *testing.T, file string, dependencyFile string) []byte {
	test.Helper()
	fileSet := token.NewFileSet()
	checkedPackage := func(path string, name string, source string, importer types.Importer) *types.Package {
		parsed, parseError := parser.ParseFile(fileSet, name, source, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", path, parseError)
		}
		configuration := types.Config{Importer: importer}
		checked, checkError := configuration.Check(path, fileSet, []*ast.File{parsed}, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s: %v", path, checkError)
		}
		return checked
	}
	dependency := checkedPackage(rebasedDependencyPath, dependencyFile, rebasedDependencySource, nil)
	rebased := checkedPackage(
		rebasedPath,
		file,
		rebasedSource,
		importerFunction(func(path string) (*types.Package, error) {
			if path != rebasedDependencyPath {
				return nil, fmt.Errorf("no package %s", path)
			}
			return dependency, nil
		}),
	)
	var written bytes.Buffer
	if writeError := gcexportdata.Write(&written, fileSet, rebased); writeError != nil {
		test.Fatalf("writing the export data of %s: %v", rebasedPath, writeError)
	}
	return written.Bytes()
}

// readPositions is what export data of the package at path reads as: its export digest, and the position, as
// "file:line:column", of every object it declares and every one of its imports' objects it carries, each field,
// method, parameter and result included, named by its package path and the names that lead to it.
func readPositions(test *testing.T, exportData []byte, path string) (cache.Key, map[string]string) {
	test.Helper()
	fileSet := token.NewFileSet()
	read, readError := gcexportdata.Read(bytes.NewReader(exportData), fileSet, map[string]*types.Package{}, path)
	if readError != nil {
		test.Fatalf("reading the export data of %s: %v", path, readError)
	}
	positions := map[string]string{}
	record := func(name string, object types.Object) {
		positions[name] = fileSet.Position(object.Pos()).String()
	}
	recordSignature := func(name string, signature *types.Signature) {
		for index, parameter := range slices.Collect(signature.Params().Variables()) {
			record(fmt.Sprintf("%s(parameter %d)", name, index), parameter)
		}
		for index, result := range slices.Collect(signature.Results().Variables()) {
			record(fmt.Sprintf("%s(result %d)", name, index), result)
		}
	}
	for _, each := range append([]*types.Package{read}, read.Imports()...) {
		for _, name := range each.Scope().Names() {
			object := each.Scope().Lookup(name)
			named := each.Path() + "." + name
			record(named, object)
			switch declared := object.(type) {
			case *types.Func:
				recordSignature(named, declared.Signature())
			case *types.TypeName:
				if definedType, isDefined := declared.Type().(*types.Named); isDefined {
					for method := range definedType.Methods() {
						record(named+"."+method.Name(), method)
						recordSignature(named+"."+method.Name(), method.Signature())
					}
				}
				if structType, isStruct := declared.Type().Underlying().(*types.Struct); isStruct {
					for field := range structType.Fields() {
						record(named+"."+field.Name(), field)
					}
				}
			}
		}
	}
	return exportDigest(read), positions
}

// renamedOrFatal is exportData renamed by rename, failing the test on an error.
func renamedOrFatal(test *testing.T, exportData []byte, rename func(string) (string, error)) []byte {
	test.Helper()
	renamed, renameError := renameExportDataFiles(exportData, rebasedPath, rename)
	if renameError != nil {
		test.Fatalf("renaming the files of %s: %v", rebasedPath, renameError)
	}
	return renamed
}

// Export data renamed to the marker names every file by it, its dependency's carried declarations included, and
// renamed back to the same root it reads as the original did: the same API, every object at the same file, line and
// column. Renamed to another root instead, every position names the file at the same path below that root, on the
// same line and column.
func TestExportDataRenamedToTheMarkerAndBackReadsAsTheOriginal(test *testing.T) {
	test.Parallel()

	releaseRoot := filepath.Join(test.TempDir(), "release", "go")
	localRoot := filepath.Join(test.TempDir(), "local", "go")
	file := filepath.Join(releaseRoot, "src", "example.com", "rebased", "rebased.go")
	dependencyFile := filepath.Join(releaseRoot, "src", "example.com", "rebased", "dependency", "dependency.go")
	original := rebasedExportData(test, file, dependencyFile)
	originalDigest, originalPositions := readPositions(test, original, rebasedPath)
	for _, carried := range []string{rebasedDependencyPath + ".Item.Label", rebasedPath + ".Holder.Count"} {
		if !strings.HasPrefix(originalPositions[carried], filepath.Dir(file)) {
			test.Fatalf("want the original export data to place %s under %s, got %q among\n%v", carried,
				filepath.Dir(file), originalPositions[carried], originalPositions)
		}
	}

	marked := renamedOrFatal(test, original, renameToGoRootMarker(releaseRoot))
	markedDigest, markedPositions := readPositions(test, marked, rebasedPath)
	if markedDigest != originalDigest {
		test.Fatalf("want the marked export data to read as the same API")
	}
	for name, position := range markedPositions {
		if !strings.HasPrefix(position, goRootMarker+"/src/example.com/rebased/") {
			test.Fatalf("want every position of the marked export data under %s, got %s at %s", goRootMarker, name,
				position)
		}
	}

	back := renamedOrFatal(test, marked, renameFromGoRootMarker(releaseRoot))
	backDigest, backPositions := readPositions(test, back, rebasedPath)
	if backDigest != originalDigest || !reflect.DeepEqual(backPositions, originalPositions) {
		test.Fatalf("want the export data renamed back to read as the original\nback     %v\noriginal %v",
			backPositions, originalPositions)
	}

	local := renamedOrFatal(test, marked, renameFromGoRootMarker(localRoot))
	localDigest, localPositions := readPositions(test, local, rebasedPath)
	want := map[string]string{}
	for name, position := range originalPositions {
		want[name] = filepath.Join(localRoot, strings.TrimPrefix(position, releaseRoot))
	}
	if localDigest != originalDigest || !reflect.DeepEqual(localPositions, want) {
		test.Fatalf("want every position under %s at its line and column\ngot  %v\nwant %v", localRoot,
			localPositions, want)
	}
	if _, carried := localPositions[rebasedDependencyPath+".Item.Labelled"]; !carried {
		test.Fatalf("want the dependency's carried method among the positions, got %v", localPositions)
	}
}

// A release refuses export data that names a file outside the Go installation, as the dependency's is here; a run
// refuses export data that names a file without the marker; and bytes that do not read as export data are refused
// rather than renamed.
func TestRenamingExportDataRefusesAFileItCannotName(test *testing.T) {
	test.Parallel()

	root := filepath.Join(test.TempDir(), "go")
	file := filepath.Join(root, "src", "example.com", "rebased", "rebased.go")
	outside := filepath.Join(test.TempDir(), "elsewhere", "dependency.go")
	escaping := rebasedExportData(test, file, outside)
	if _, renameError := renameExportDataFiles(escaping, rebasedPath, renameToGoRootMarker(root)); renameError == nil ||
		!strings.Contains(renameError.Error(), outside) {
		test.Fatalf("want the dependency's file outside %s refused, got %v", root, renameError)
	}

	unmarked := rebasedExportData(test, file, filepath.Join(root, "src", "dependency.go"))
	_, unmarkedError := renameExportDataFiles(unmarked, rebasedPath, renameFromGoRootMarker(root))
	if unmarkedError == nil {
		test.Fatalf("want export data that names no file by %s refused", goRootMarker)
	}

	for _, corrupt := range [][]byte{nil, []byte("i not export data"), unmarked[:len(unmarked)/2]} {
		_, corruptError := renameExportDataFiles(corrupt, rebasedPath, renameToGoRootMarker(root))
		if corruptError == nil {
			test.Fatalf("want %d corrupt bytes refused", len(corrupt))
		}
	}
}

// Each rename takes exactly the files it is for: the release's, a file below the root and no other, the run's, a
// marked path below the marker and no other.
func TestTheGoRootRenamesTakeOnlyTheirOwnFiles(test *testing.T) {
	test.Parallel()

	root := filepath.Join(test.TempDir(), "go")
	toMarker, fromMarker := renameToGoRootMarker(root), renameFromGoRootMarker(root)
	marked, markError := toMarker(filepath.Join(root, "src", "net", "cgo_unix.go"))
	if markError != nil || marked != goRootMarker+"/src/net/cgo_unix.go" {
		test.Fatalf("want %s/src/net/cgo_unix.go, got %q and %v", goRootMarker, marked, markError)
	}
	resolved, resolveError := fromMarker(marked)
	if resolveError != nil || resolved != filepath.Join(root, "src", "net", "cgo_unix.go") {
		test.Fatalf("want the marked file back under %s, got %q and %v", root, resolved, resolveError)
	}
	for _, refused := range []string{root, root + "2/src/net/net.go", filepath.Join(root, "..", "net.go"),
		"src/net/net.go", goRootMarker + "/src/net/net.go"} {
		if renamed, renameError := toMarker(refused); renameError == nil {
			test.Fatalf("want %q refused by the release's rename, got %q", refused, renamed)
		}
	}
	for _, refused := range []string{filepath.Join(root, "src", "net", "net.go"), goRootMarker,
		goRootMarker + "/", goRootMarker + "/.", goRootMarker + "/../net.go", goRootMarker + "2/src/net/net.go"} {
		if renamed, renameError := fromMarker(refused); renameError == nil {
			test.Fatalf("want %q refused by the run's rename, got %q", refused, renamed)
		}
	}
}

// Withheld's files are renamed to the marker and back, each error's file and each declaration's, the line and column
// kept, and a file outside the root, in an error or a declaration, is refused.
func TestWithheldFilesRenameToTheMarkerAndBack(test *testing.T) {
	test.Parallel()

	releaseRoot := filepath.Join(test.TempDir(), "release", "go")
	localRoot := filepath.Join(test.TempDir(), "local", "go")
	in := func(root string) load.PackageWithheld {
		return load.PackageWithheld{
			Errors: []load.WithheldError{{File: filepath.Join(root, "src", "net", "cgo_unix.go"), Line: 12, Column: 3,
				Message: "invalid operation"}},
			Declarations: []string{filepath.Join(root, "src", "net", "cgo_unix.go") + ":40:1",
				filepath.Join(root, "src", "os", "user", "cgo_lookup_unix.go") + ":7"},
		}
	}
	marked, markError := renameWithheldFiles(in(releaseRoot), renameToGoRootMarker(releaseRoot))
	want := load.PackageWithheld{
		Errors: []load.WithheldError{{File: goRootMarker + "/src/net/cgo_unix.go", Line: 12, Column: 3,
			Message: "invalid operation"}},
		Declarations: []string{goRootMarker + "/src/net/cgo_unix.go:40:1",
			goRootMarker + "/src/os/user/cgo_lookup_unix.go:7"},
	}
	if markError != nil || !reflect.DeepEqual(marked, want) {
		test.Fatalf("want\n%+v\ngot\n%+v and %v", want, marked, markError)
	}
	local, localError := renameWithheldFiles(marked, renameFromGoRootMarker(localRoot))
	if localError != nil || !reflect.DeepEqual(local, in(localRoot)) {
		test.Fatalf("want\n%+v\ngot\n%+v and %v", in(localRoot), local, localError)
	}

	outside := filepath.Join(test.TempDir(), "elsewhere.go")
	for _, escaping := range []load.PackageWithheld{
		{Errors: []load.WithheldError{{File: outside, Line: 1, Column: 1}}},
		{Declarations: []string{outside + ":3:1"}},
	} {
		if _, renameError := renameWithheldFiles(escaping, renameToGoRootMarker(releaseRoot)); renameError == nil {
			test.Fatalf("want %+v refused outside %s", escaping, releaseRoot)
		}
	}
}

// A position's file is what precedes its line and column, which are read from the end, so a Windows file's own colon
// stays in the file, and a position with only a line, or none, keeps what it has.
func TestSplitPositionTakesTheLineAndColumnFromTheEnd(test *testing.T) {
	test.Parallel()

	for position, want := range map[string][2]string{
		`C:\Go\src\net\net.go:12:3`: {`C:\Go\src\net\net.go`, ":12:3"},
		"/go/src/net/net.go:12":     {"/go/src/net/net.go", ":12"},
		"/go/src/net/net.go":        {"/go/src/net/net.go", ""},
		"/go/src/a:b/net.go:4:5":    {"/go/src/a:b/net.go", ":4:5"},
	} {
		file, lineAndColumn := splitPosition(position)
		if file != want[0] || lineAndColumn != want[1] {
			test.Fatalf("splitting %q: want %q and %q, got %q and %q", position, want[0], want[1], file, lineAndColumn)
		}
	}
}
