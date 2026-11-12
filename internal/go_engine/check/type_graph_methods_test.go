package check

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/gcexportdata"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// methodsModule is the module of the unexported-method fixtures, whose packages are directories under it.
const methodsModule = "example.com/methods"

// methodsFiles are the fixtures' packages, each a directory of files, some of one target each.
//
//   - file is os.File's shape: a File whose unexported method differs by target, and no interface with an unexported
//     method, so no importer can tell the targets apart through it.
//   - embedded declares Thing, whose unexported measure differs by target, and Measurer, an interface with measure, so
//     whether a type embedding Thing satisfies it differs by target.
//   - wrapper declares Wrapper, a struct embedding embedded.Thing, which carries Thing's measure into Wrapper's method
//     set, while wrapper itself declares no interface.
var methodsFiles = map[string]string{
	"file/file.go": "package file\n\ntype File struct{ name string }\n\n" +
		"func (file *File) Name() string { return file.name }\n",
	"file/file_darwin.go": "package file\n\nfunc (file *File) readdir() []string { return nil }\n",
	"file/file_linux.go":  "package file\n\nfunc (file *File) readdir(count int) []string { return nil }\n",
	"embedded/embedded.go": "package embedded\n\ntype Thing struct{}\n\n" +
		"type Measurer interface{ measure() int }\n",
	"embedded/embedded_darwin.go": "package embedded\n\nfunc (Thing) measure() int { return 0 }\n",
	"embedded/embedded_linux.go":  "package embedded\n\nfunc (Thing) measure() int64 { return 0 }\n",
	"wrapper/wrapper.go": "package wrapper\n\nimport \"example.com/methods/embedded\"\n\n" +
		"type Wrapper struct{ embedded.Thing }\n",
}

// targetPackage is one package of a fixture checked per target: the directory of its files, and its path.
type targetPackage struct {
	directory string
	path      string
}

// targetFixture is a module whose packages, some of their files of one target each, are checked per target: its
// files by path, and its packages, each after what it imports.
type targetFixture struct {
	files    map[string]string
	packages []targetPackage
}

// methodsFixture is the unexported-method fixtures' module.
var methodsFixture = targetFixture{files: methodsFiles, packages: []targetPackage{
	{"file", methodsModule + "/file"},
	{"embedded", methodsModule + "/embedded"},
	{"wrapper", methodsModule + "/wrapper"},
}}

// checkedTargets type-checks fixture's packages for goos, from the files its build context matches, by path.
func checkedTargets(test *testing.T, goos string, fixture targetFixture) map[string]*types.Package {
	test.Helper()

	directory := test.TempDir()
	writeFiles(test, directory, fixture.files)
	context := build.Default
	context.GOOS, context.GOARCH, context.CgoEnabled = goos, "amd64", false
	fileSet := token.NewFileSet()
	checked := map[string]*types.Package{}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if imported, found := checked[path]; found {
			return imported, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	for _, checking := range fixture.packages {
		packageDirectory := filepath.Join(directory, checking.directory)
		entries, readError := os.ReadDir(packageDirectory)
		if readError != nil {
			test.Fatal(readError)
		}
		var files []*ast.File
		for _, entry := range entries {
			matched, matchError := context.MatchFile(packageDirectory, entry.Name())
			if matchError != nil {
				test.Fatal(matchError)
			}
			if !matched {
				continue
			}
			parsed, parseError := parser.ParseFile(fileSet, filepath.Join(packageDirectory, entry.Name()), nil,
				parser.SkipObjectResolution)
			if parseError != nil {
				test.Fatal(parseError)
			}
			files = append(files, parsed)
		}
		typed, checkError := configuration.Check(checking.path, fileSet, files, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s for %s: %v", checking.path, goos, checkError)
		}
		checked[checking.path] = typed
	}
	return checked
}

// readsHoldAcross is the load's verdict, in fixture's linux build, on a user package checked on its darwin host from
// source, which imports the package at importedPath and reads what source reads of it, each build's memo told sizes,
// nil for none, and the user judged as a package with assembly files when assembly is set. The reads are recorded
// without the user's syntax, so every read is judged by its object's closure, the rendering these tests are about.
func readsHoldAcross(
	test *testing.T,
	fixture targetFixture,
	importedPath string,
	source string,
	sizes types.Sizes,
	assembly bool,
) (bool, string) {
	test.Helper()

	darwin := checkedTargets(test, "darwin", fixture)
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "user.go", "package user\n\nimport \""+importedPath+"\"\n\n"+
		source, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	info := &types.Info{
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if found, isFound := darwin[path]; isFound {
			return found, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	user, checkError := configuration.Check("example.com/user", fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatal(checkError)
	}
	reads := &build_reads.Reads{}
	reads.RecordTypes(user, info, nil)
	restoring := Restoring{Path: user.Path(), Imports: []string{importedPath}, Reads: reads, Assembly: assembly}
	host, build := NewReadsBuild(), NewReadsBuild()
	host.measureBy(sizes)
	build.measureBy(sizes)
	return TypesHold(restoring, checkedTargets(test, "linux", fixture), nil, host, build)
}

// methodsReadsHold is the load's verdict, in the linux build, on a user package checked on the darwin host from
// source, which imports the methods fixture's package imported and reads what source reads of it.
func methodsReadsHold(test *testing.T, imported string, source string) (bool, string) {
	test.Helper()

	return readsHoldAcross(test, methodsFixture, methodsModule+"/"+imported, source, nil, false)
}

// An importer reading a type whose unexported method differs by target, os.File's shape, reads the same in either
// build, since it can never name the method and its package declares no interface that holds it: it is restored.
func TestAnUnexportedMethodNoInterfaceHoldsLeavesTheRendering(test *testing.T) {
	test.Parallel()

	holds, why := methodsReadsHold(test, "file", "func Use(opened *file.File) string { return opened.Name() }\n")
	if !holds {
		test.Errorf("want the reads of File to hold, its readdir unseen by any importer, got %q", why)
	}
}

// A struct of one package embedding another's type carries that type's unexported method, whose own package declares
// an interface with it, so whether the struct satisfies the interface differs by target: an importer reading the
// struct is checked again. The exception is the method's package's, embedded's, never the struct's, wrapper's, which
// declares no interface.
func TestAnUnexportedMethodItsPackagesInterfaceHoldsStaysInTheRendering(test *testing.T) {
	test.Parallel()

	holds, why := methodsReadsHold(test, "wrapper",
		"func Use(wrapped wrapper.Wrapper) wrapper.Wrapper { return wrapped }\n")
	const want = "T object " + methodsModule + "/wrapper.Wrapper renders differently"
	if holds || why != want {
		test.Errorf("want %q, the embedded measure differing by target, got %t, %q", want, holds, why)
	}
}

// interfaceSources are packages each declaring an interface with an unexported method, by whether an importer can
// reach it: through an exported function's parameter, through an unexported type an exported type's method names, or
// not at all, declared unexported and named by nothing exported.
var interfaceSources = map[string]struct {
	source    string
	reachable bool
}{
	"parameter": {"type measurer interface{ measure() int }\n\nfunc Measure(value measurer) int { return 0 }\n", true},
	"method": {"type Box struct{}\n\nfunc (Box) Hold() holder { return holder{} }\n\n" +
		"type holder struct{ inner measurer }\n\ntype measurer interface{ measure() int }\n", true},
	"unreached": {"type Thing struct{}\n\nfunc (Thing) measure() int { return 0 }\n\n" +
		"type measurer interface{ measure() int }\n\nfunc use(value measurer) int { return value.measure() }\n", false},
}

// Whether a package declares an interface with an unexported method an importer can reach is answered alike from its
// source and from its export data, which holds only what its exported objects reach, so a package a build restored
// from the host keeps the same unexported methods as the host's check of it: an interface nothing exported reaches is
// no importer's, and is left out of both.
func TestAnUnexportedInterfaceIsJudgedAlikeFromSourceAndExportData(test *testing.T) {
	test.Parallel()

	for name, declared := range interfaceSources {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			source := checkedInMemory(test, methodsModule+"/"+name, "package "+name+"\n\n"+declared.source, nil)
			var exportData bytes.Buffer
			if writeError := gcexportdata.Write(&exportData, nil, source); writeError != nil {
				test.Fatal(writeError)
			}
			exported, readError := gcexportdata.Read(&exportData, token.NewFileSet(), map[string]*types.Package{},
				source.Path())
			if readError != nil {
				test.Fatal(readError)
			}
			fromSource, fromExportData := declaresUnexportedInterface(source), declaresUnexportedInterface(exported)
			if fromSource != declared.reachable || fromExportData != declared.reachable {
				test.Errorf("want %t from source and from export data, got %t and %t", declared.reachable,
					fromSource, fromExportData)
			}
		})
	}
}
