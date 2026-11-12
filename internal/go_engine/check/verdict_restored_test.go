package check

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/gcexportdata"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// The restored fixture's paths: hidden declares an unexported type with a Close method, and an unexported close, that
// nothing exported reaches, so its export data leaves the type out, and its user asks which type names of its closure
// have a Close method.
const (
	restoredHiddenPath = "example.com/restored/hidden"
	restoredUserPath   = "example.com/restored/user"
)

// restoredSources are hidden and its user, by path.
var restoredSources = map[string]string{
	restoredHiddenPath: "package hidden\n\ntype closer struct{}\n\nfunc (closer) Close() error { return nil }\n\n" +
		"func (closer) close() {}\n\nfunc Count() int { return 1 }\n",
	restoredUserPath: "package user\n\nimport \"example.com/restored/hidden\"\n\n" +
		"type Mine struct{}\n\nfunc (Mine) Close() error { return nil }\n\nfunc (Mine) close() {}\n\n" +
		"func Use() int { return hidden.Count() }\n",
}

// restoredWorld is the fixture on the host, hidden and its user checked from source, the user's type reads recorded
// and a Method lookup of Close added, and hidden as a build restores it from the host, read from the export data the
// host's check of it writes.
type restoredWorld struct {
	restoring    Restoring
	exportHidden *types.Package
}

// checkedRestoredWorld checks the fixture on the host and reads hidden back from its export data.
func checkedRestoredWorld(test *testing.T) restoredWorld {
	test.Helper()

	fileSet := token.NewFileSet()
	parse := func(path string) *ast.File {
		parsed, parseError := parser.ParseFile(fileSet, path+".go", restoredSources[path], parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		return parsed
	}
	hostHidden, hiddenError := (&types.Config{}).Check(restoredHiddenPath, fileSet,
		[]*ast.File{parse(restoredHiddenPath)}, nil)
	if hiddenError != nil {
		test.Fatal(hiddenError)
	}
	info := &types.Info{
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if path == restoredHiddenPath {
			return hostHidden, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	hostUser, userError := configuration.Check(restoredUserPath, fileSet, []*ast.File{parse(restoredUserPath)}, info)
	if userError != nil {
		test.Fatal(userError)
	}
	var exportData bytes.Buffer
	if writeError := gcexportdata.Write(&exportData, fileSet, hostHidden); writeError != nil {
		test.Fatal(writeError)
	}
	exportHidden, readError := gcexportdata.Read(&exportData, token.NewFileSet(), map[string]*types.Package{},
		restoredHiddenPath)
	if readError != nil {
		test.Fatal(readError)
	}
	if exportHidden.Scope().Lookup("closer") != nil {
		test.Fatal("hidden's export data holds closer, which nothing exported reaches, so the fixture tells nothing")
	}
	reads := &build_reads.Reads{}
	// Without the syntax every read is judged by its closure: the lookup is what this fixture is about.
	reads.RecordTypes(hostUser, info, nil)
	reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Close",
		Subject: methodOf(hostUser, "Mine", "Close")})
	return restoredWorld{
		restoring:    Restoring{Path: restoredUserPath, Imports: []string{restoredHiddenPath}, Reads: reads},
		exportHidden: exportHidden,
	}
}

// A method lookup holds in a build that restored a package of the closure from the host, though the export data its
// types were read from lacks an unexported type with the method: the restored package gives the host's own type names,
// since checked from source it declares what the host's does. The same package checked from source in the build, as
// its export data stands for here, is scanned there, and the type it lacks fails the lookup.
func TestAMethodLookupAsksAPackageRestoredFromTheHostOfTheHost(test *testing.T) {
	test.Parallel()

	world := checkedRestoredWorld(test)
	closure := map[string]*types.Package{restoredHiddenPath: world.exportHidden}
	for _, step := range []struct {
		name     string
		restored map[*types.Package]bool
		want     string
	}{
		{"restored from the host", map[*types.Package]bool{world.exportHidden: true}, ""},
		{"checked from source", nil,
			"L lookup Method Close: 1 type names have the method on the host, 0 in the build"},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			holds, why := TypesHold(world.restoring, closure, step.restored, NewReadsBuild(), NewReadsBuild())
			if holds != (step.want == "") || why != step.want {
				test.Errorf("want %t, %q, got %t, %q", step.want == "", step.want, holds, why)
			}
		})
	}
}

// Many verdicts, each with many Interface and Method lookups, over packages that share their imports, walk each
// root's closure once per build and index each package's methods once per build, however many lookups ask: the
// method index is built once, never per lookup or per verdict.
func TestTheMethodIndexIsBuiltOncePerBuild(test *testing.T) {
	test.Parallel()

	world := checkedRestoredWorld(test)
	host, build := NewReadsBuild(), NewReadsBuild()
	counted := func(counts map[*types.Package]int) func(*types.Package) {
		return func(counted *types.Package) { counts[counted]++ }
	}
	hostWalked, buildWalked := map[*types.Package]int{}, map[*types.Package]int{}
	hostIndexed, buildIndexed := map[*types.Package]int{}, map[*types.Package]int{}
	host.walking, build.walking = counted(hostWalked), counted(buildWalked)
	host.indexing, build.indexing = counted(hostIndexed), counted(buildIndexed)
	subject := world.restoring.Reads.Lookups()[0].Subject
	closure := map[string]*types.Package{restoredHiddenPath: world.exportHidden}
	for range 3 {
		reads := &build_reads.Reads{}
		for _, lookup := range world.restoring.Reads.Lookups() {
			reads.AddLookup(lookup)
		}
		for _, name := range []string{"Close", "Count", "Missing", "Close"} {
			reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryInterface, Name: name, Subject: subject})
		}
		restoring := Restoring{Path: restoredUserPath, Imports: []string{restoredHiddenPath}, Reads: reads}
		reads.RecordTypes(subject.Pkg(), &types.Info{}, nil)
		if holds, why := TypesHold(restoring, closure, map[*types.Package]bool{world.exportHidden: true}, host,
			build); !holds {
			test.Fatalf("the lookups of one source do not hold: %s", why)
		}
	}
	for side, counts := range map[string]map[*types.Package]int{
		"host's walks": hostWalked, "build's walks": buildWalked, "host's index": hostIndexed,
		"build's index": buildIndexed,
	} {
		if len(counts) == 0 {
			test.Errorf("the %s counted no package", side)
		}
		for counted, times := range counts {
			if times != 1 {
				test.Errorf("the %s counted %s %d times, want once", side, counted.Path(), times)
			}
		}
	}
}
