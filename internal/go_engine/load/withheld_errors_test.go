package load

import (
	"fmt"
	"go/ast"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
)

// wrapperFiles are a cgo package that wraps its C names in `_C_` functions, as the standard library's net and
// os/user do: the cgo file defines the wrappers, and the plain file calls one and converts what it returns, which,
// with package "C" faked, is of an invalid type, so the type checker reports the conversion in the plain file.
func wrapperFiles(packageName string) map[string]string {
	return map[string]string{
		packageName + "_cgo.go": "package " + packageName + "\n\n// struct thing { unsigned int count; };\n" +
			"import \"C\"\n\ntype _C_struct_thing = C.struct_thing\n\ntype _C_uint = C.uint\n\n" +
			"func _C_count(thing *_C_struct_thing) _C_uint { return thing.count }\n",
		packageName + ".go": "package " + packageName + "\n\n// Count is how many things there are.\n" +
			"func Count() uint64 {\n\tvar thing _C_struct_thing\n\treturn uint64(_C_count(&thing))\n}\n",
	}
}

// otherFiles are a second package of the dependency with a cgo file, whose plain file reaches nothing of it: one
// function, and a type error of its own; and a third, cgofree, with no cgo file, whose function's type error leaves
// an expression without a type. It is a package of its own because the type checker stays silent on an
// invalid type once a package has reported any error, which would hide the dependency's `_C_` call.
var otherFiles = map[string]string{
	"dependency/other/other_cgo.go": "package other\n\n// static int one(void) { return 1; }\nimport \"C\"\n\n" +
		"func _C_one() int { return int(C.one()) }\n",
	"dependency/other/other.go": "package other\n\n// Plain is one.\nfunc Plain() int { return 1 }\n\n" +
		"// Broken is of the wrong type.\nvar Broken int = \"text\"\n",
	"dependency/cgofree/cgofree.go": "package cgofree\n\n// Broken adds one to what is not declared.\n" +
		"func Broken() int { return missing + 1 }\n",
}

// cgoFixtures are the cgo loads, written and loaded once per test binary. project is a module whose own package
// own wraps C names, and whose package user imports dependency, a module beside it, outside the workspace, that
// wraps them the same way, and its package other. A project with cgo of its own is loaded through go/packages, so
// project is loaded from source by the tests alone, to judge that load on the project's own cgo package too. linux
// is a package importing net and os/user, loaded for linux with cgo on, as the probe that settled the boundary
// loaded it.
var cgoFixtures struct {
	once    sync.Once
	root    string
	project *Loaded
	linux   *Loaded
	failure error
}

// cgoEnvironment is what the cgo fixtures load under: linux/amd64 with cgo on, which go list reads without a C
// compiler, so the fixtures load the same on every host.
var cgoEnvironment = []string{"GOWORK=off", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1"}

// loadedCgoFixtures writes and loads the cgo fixtures on first use and returns the root they were written under.
func loadedCgoFixtures(test *testing.T) string {
	test.Helper()

	cgoFixtures.once.Do(func() {
		cgoFixtures.root, cgoFixtures.failure = os.MkdirTemp("", "cohere-go-engine-load-cgo-")
		if cgoFixtures.failure != nil {
			return
		}
		files := map[string]string{
			"project/go.mod": "module example.com/project\n\ngo 1.21\n\nrequire example.com/dependency v0.0.0\n\n" +
				"replace example.com/dependency => ../dependency\n",
			"project/user/user.go": "package user\n\nimport (\n\t\"example.com/dependency\"\n" +
				"\t\"example.com/dependency/cgofree\"\n\t\"example.com/dependency/other\"\n)\n\n" +
				"// Total is the dependency's count.\n" +
				"func Total() uint64 { return dependency.Count() + uint64(other.Plain()+cgofree.Broken()) }\n",
			"dependency/go.mod": "module example.com/dependency\n\ngo 1.21\n",
			"linux/go.mod":      "module example.com/linux\n\ngo 1.21\n",
			"linux/linux.go":    "package linux\n\nimport (\n\t_ \"net\"\n\t_ \"os/user\"\n)\n",
		}
		for name, contents := range wrapperFiles("own") {
			files["project/own/"+name] = contents
		}
		for name, contents := range wrapperFiles("dependency") {
			files["dependency/"+name] = contents
		}
		maps.Copy(files, otherFiles)
		for name, contents := range files {
			path := filepath.Join(cgoFixtures.root, filepath.FromSlash(name))
			if cgoFixtures.failure = os.MkdirAll(filepath.Dir(path), 0o755); cgoFixtures.failure != nil {
				return
			}
			if cgoFixtures.failure = os.WriteFile(path, []byte(contents), 0o644); cgoFixtures.failure != nil {
				return
			}
		}
		cgoFixtures.project, cgoFixtures.failure = loadWorkspace(filepath.Join(cgoFixtures.root, "project"),
			[]string{"./..."}, cgoEnvironment, nil, false)
		if cgoFixtures.failure != nil {
			return
		}
		cgoFixtures.linux, cgoFixtures.failure = Packages(filepath.Join(cgoFixtures.root, "linux"),
			[]string{"./..."}, cgoEnvironment, nil)
	})
	if cgoFixtures.failure != nil {
		test.Fatalf("loading the cgo fixtures: %v", cgoFixtures.failure)
	}
	return cgoFixtures.root
}

// A dependency's `_C_` call, whose type the faked package "C" leaves invalid, is withheld: no diagnostic, and the
// file, its package and the one error counted.
func TestADependencysCgoTypeErrorIsWithheldAndCounted(test *testing.T) {
	test.Parallel()

	root := loadedCgoFixtures(test)
	dependencyFile := filepath.Join(root, "dependency", "dependency.go")
	want := []Withheld{{Package: "example.com/dependency", File: dependencyFile, Errors: 1}}
	if !slices.Equal(cgoFixtures.project.WithheldInDependencyCgo, want) {
		test.Fatalf("want withheld %+v, got %+v", want, cgoFixtures.project.WithheldInDependencyCgo)
	}
	for _, diagnostic := range cgoFixtures.project.Diagnostics {
		if diagnostic.File == dependencyFile {
			test.Errorf("the withheld error is a diagnostic too: %+v", diagnostic)
		}
	}
}

// A type error in a dependency with cgo files whose expression reaches nothing of them is the dependency's own,
// so it stays a diagnostic, reported where it is, and nothing of it is withheld.
func TestADependencysTypeErrorThatReachesNoCgoIsADiagnostic(test *testing.T) {
	test.Parallel()

	root := loadedCgoFixtures(test)
	plainFile := filepath.Join(root, "dependency", "other", "other.go")
	var found []Diagnostic
	for _, diagnostic := range cgoFixtures.project.Diagnostics {
		if diagnostic.File == plainFile {
			found = append(found, diagnostic)
		}
	}
	if len(found) != 1 || found[0].Kind != KindType || found[0].Line != 7 {
		test.Fatalf("want one type diagnostic at %s:7, got %+v (all: %+v)", plainFile, found,
			cgoFixtures.project.Diagnostics)
	}
	for _, withheld := range cgoFixtures.project.WithheldInDependencyCgo {
		if withheld.File == plainFile {
			test.Errorf("an error that reaches no cgo file was withheld: %+v", withheld)
		}
	}
}

// A dependency with no cgo file has no package "C" faked, so a type error in it is its own, whatever expression it
// leaves without a type: a diagnostic, never withheld.
func TestACgoFreeDependencysTypeErrorIsADiagnostic(test *testing.T) {
	test.Parallel()

	root := loadedCgoFixtures(test)
	cgoFreeFile := filepath.Join(root, "dependency", "cgofree", "cgofree.go")
	var found []Diagnostic
	for _, diagnostic := range cgoFixtures.project.Diagnostics {
		if diagnostic.File == cgoFreeFile {
			found = append(found, diagnostic)
		}
	}
	if len(found) != 1 || found[0].Kind != KindType || found[0].Line != 4 {
		test.Fatalf("want one type diagnostic at %s:4, got %+v (all: %+v)", cgoFreeFile, found,
			cgoFixtures.project.Diagnostics)
	}
	for _, withheld := range cgoFixtures.project.WithheldInDependencyCgo {
		if withheld.File == cgoFreeFile {
			test.Errorf("a cgo-free dependency's error was withheld: %+v", withheld)
		}
	}
}

// The same call in the project's own cgo package, were it loaded from source, is the project's to fix, so it is a
// type diagnostic, reported where it is, and nothing of it is withheld.
func TestTheSameCgoTypeErrorInTheProjectIsADiagnostic(test *testing.T) {
	test.Parallel()

	root := loadedCgoFixtures(test)
	ownFile := filepath.Join(root, "project", "own", "own.go")
	var found []Diagnostic
	for _, diagnostic := range cgoFixtures.project.Diagnostics {
		if diagnostic.File == ownFile {
			found = append(found, diagnostic)
		}
	}
	if len(found) != 1 || found[0].Kind != KindType || found[0].Line != 6 ||
		!strings.Contains(found[0].Message, "invalid type") {
		test.Fatalf("want one type diagnostic at %s:6 about the invalid type, got %+v (all: %+v)", ownFile, found,
			cgoFixtures.project.Diagnostics)
	}
	for _, withheld := range cgoFixtures.project.WithheldInDependencyCgo {
		if strings.HasPrefix(withheld.File, filepath.Join(root, "project")+string(filepath.Separator)) {
			test.Errorf("a project file's error was withheld: %+v", withheld)
		}
	}
}

// The probe that settled the boundary: net and os/user for linux with cgo on wrap their C names, and exactly the
// one error each has, in net's cgo_unix.go and os/user's cgo_lookup_unix.go, is withheld, and nothing is reported.
func TestNetAndOsUserForLinuxWithholdExactlyTheirWrapperCallers(test *testing.T) {
	test.Parallel()

	loadedCgoFixtures(test)
	command := exec.Command("go", "env", "GOROOT")
	command.Env = append(os.Environ(), cgoEnvironment...)
	goRoot, goEnvironmentError := command.Output()
	if goEnvironmentError != nil {
		test.Fatalf("reading GOROOT with go env: %v", goEnvironmentError)
	}
	source := filepath.Join(strings.TrimSpace(string(goRoot)), "src")
	want := []Withheld{
		{Package: "net", File: filepath.Join(source, "net", "cgo_unix.go"), Errors: 1},
		{Package: "os/user", File: filepath.Join(source, "os", "user", "cgo_lookup_unix.go"), Errors: 1},
	}
	if !slices.Equal(cgoFixtures.linux.WithheldInDependencyCgo, want) {
		test.Fatalf("want withheld\n%+v\ngot\n%+v", want, cgoFixtures.linux.WithheldInDependencyCgo)
	}
	if len(cgoFixtures.linux.Diagnostics) > 0 {
		test.Fatalf("want no diagnostics, got %+v", cgoFixtures.linux.Diagnostics)
	}
	if cgoFixtures.linux.CgoTranslated {
		test.Error("a project that only reaches net went through go/packages; only its own cgo sends it there")
	}
	if cgoFixtures.linux.WithheldDependencyCgoDeclarations == 0 {
		test.Error("no declaration of net or os/user was withheld, though their wrappers call C")
	}
}

// functionsOf is each function declared in the package path the analyzed packages reach, by name, from the
// syntax the rules are given.
func functionsOf(loaded *Loaded, path string) map[string]*ast.FuncDecl {
	functions := map[string]*ast.FuncDecl{}
	packages.Visit(loaded.Packages, nil, func(visited *packages.Package) {
		if visited.PkgPath != path {
			return
		}
		for _, file := range visited.Syntax {
			for _, declaration := range file.Decls {
				if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
					functions[function.Name.Name] = function
				}
			}
		}
	})
	return functions
}

// A dependency's function whose body reaches its cgo files, a `_C_` wrapper and its caller, comes to the rules
// without its body, counted, and every other function keeps it: the dependency's plain one, the cgo-free
// dependency's one though its body holds an expression without a type, and every function of the project's own cgo
// package, which is the project's to check.
func TestADependencysFunctionsThatReachCgoComeWithoutTheirBodies(test *testing.T) {
	test.Parallel()

	loadedCgoFixtures(test)
	if cgoFixtures.project.WithheldDependencyCgoDeclarations != 3 {
		test.Errorf("want 3 declarations withheld, got %d", cgoFixtures.project.WithheldDependencyCgoDeclarations)
	}
	dependency := functionsOf(cgoFixtures.project, "example.com/dependency")
	other := functionsOf(cgoFixtures.project, "example.com/dependency/other")
	cgoFree := functionsOf(cgoFixtures.project, "example.com/dependency/cgofree")
	own := functionsOf(cgoFixtures.project, "example.com/project/own")
	for _, expected := range []struct {
		function *ast.FuncDecl
		name     string
		bodied   bool
	}{
		{dependency["Count"], "the dependency's Count", false},
		{dependency["_C_count"], "the dependency's _C_count", false},
		{other["_C_one"], "the dependency's _C_one", false},
		{other["Plain"], "the dependency's Plain", true},
		{cgoFree["Broken"], "the cgo-free dependency's Broken", true},
		{own["Count"], "the project's Count", true},
		{own["_C_count"], "the project's _C_count", true},
	} {
		if expected.function == nil {
			test.Errorf("%s is not in the load", expected.name)
		} else if bodied := expected.function.Body != nil; bodied != expected.bodied {
			test.Errorf("%s has a body %t, want %t", expected.name, bodied, expected.bodied)
		}
	}
}

// Every declaration of net and os/user that reaches C comes without it, functions without their bodies and
// package-level variables without their initial values, so an analyzer that walks every expression of the
// dependencies for its facts, as printf does, finishes on each, where the faked package "C" would stop it.
func TestPrintfFinishesOverNetAndOsUserForLinux(test *testing.T) {
	test.Parallel()

	loadedCgoFixtures(test)
	// A copy whose panic is its error, so a body the load left in fails this test and not the whole binary.
	recovering := *printf.Analyzer
	recovering.Run = func(pass *analysis.Pass) (result any, runError error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				runError = fmt.Errorf("it panicked with %v", recovered)
			}
		}()
		return printf.Analyzer.Run(pass)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{&recovering}, cgoFixtures.linux.Packages, nil)
	if analyzeError != nil {
		test.Fatal(analyzeError)
	}
	ran := 0
	for action := range graph.All() {
		ran++
		if action.Err != nil {
			test.Errorf("%s did not finish on %s: %v", action.Analyzer.Name, action.Package.ID, action.Err)
		}
	}
	if ran < 2 {
		test.Fatalf("printf ran on %d packages, want it on the dependencies too", ran)
	}
}

// A load that withholds nothing says so with an empty list, never nil, which would read as not counted.
func TestALoadWithNothingWithheldSaysSo(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	if withheld := fixtures.clean.WithheldInDependencyCgo; withheld == nil || len(withheld) > 0 {
		test.Fatalf("want an empty list, got %#v", withheld)
	}
}

// os/user's userBuffer and groupBuffer are initialized from cgo constants, so they reach the analyzers without
// their values, in the syntax and in TypesInfo.InitOrder both, since buildssa and buildir build the package's
// initialization from InitOrder; os/user's other variables keep theirs.
func TestOsUsersCgoInitializersLeaveTheSyntaxAndTheInitOrder(test *testing.T) {
	test.Parallel()

	loadedCgoFixtures(test)
	var osUser *packages.Package
	packages.Visit(cgoFixtures.linux.Packages, nil, func(visited *packages.Package) {
		if visited.PkgPath == "os/user" {
			osUser = visited
		}
	})
	if osUser == nil {
		test.Fatal("os/user is not in the linux load")
	}
	initialized := map[string]bool{}
	for _, initializer := range osUser.TypesInfo.InitOrder {
		for _, variable := range initializer.Lhs {
			initialized[variable.Name()] = true
		}
	}
	valued := map[string]bool{}
	for _, file := range osUser.Syntax {
		for _, declaration := range file.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral {
				continue
			}
			for _, specification := range general.Specs {
				if value, isValue := specification.(*ast.ValueSpec); isValue && len(value.Values) > 0 {
					for _, name := range value.Names {
						valued[name.Name] = true
					}
				}
			}
		}
	}
	for _, name := range []string{"userBuffer", "groupBuffer"} {
		if initialized[name] || valued[name] {
			test.Errorf("%s keeps its initial value: in InitOrder %t, in the syntax %t", name, initialized[name],
				valued[name])
		}
	}
	if len(initialized) == 0 {
		test.Error("os/user's InitOrder lost every initializer, those that reach no cgo too")
	}
}
