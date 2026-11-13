package load

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/project"
)

// listedModuleFiles are a module with what go list's answer turns on: a test-only import of the standard library, an
// external test, a command, an internal package, a module replaced into a directory of the tree and one it requires,
// marked `// indirect` though the module imports it, embedded files by name, by directory and with `all:`, files the
// build constraints leave out, a directory every file of which they leave out, one of tests alone, a cgo package and a
// standard one with cgo files of its own, and the directories go's walk never enters.
var listedModuleFiles = map[string]string{
	"go.mod": "module example.com/listed\n\ngo 1.27\n\nrequire (\n\texample.com/dependency v1.0.0 // indirect\n" +
		"\texample.com/transitive v1.0.0 // indirect\n)\n\nreplace (\n" +
		"\texample.com/dependency => ./dependency_module\n\texample.com/transitive => ./transitive_module\n)\n",
	"listed.go": "package listed\n\nimport (\n\t_ \"embed\"\n\t\"net/http\"\n\n" +
		"\t\"example.com/dependency\"\n\t\"example.com/listed/internal/helper\"\n)\n\n" +
		"//go:embed data.txt static all:hidden\nvar files string\n\n" +
		"// Serve is a handler.\nfunc Serve() http.Handler { _ = dependency.D(); _ = helper.H(); return nil }\n",
	"listed_test.go": "package listed\n\nimport (\n\t\"testing\"\n\t\"testing/fstest\"\n)\n\n" +
		"func TestServe(test *testing.T) { _ = fstest.MapFS{} }\n",
	"external_test.go": "package listed_test\n\nimport (\n\t\"testing\"\n\t\"testing/iotest\"\n\n" +
		"\t\"example.com/listed\"\n)\n\nfunc TestExternal(test *testing.T) { _ = iotest.ErrTimeout; _ = listed.Serve }\n",
	"only_windows.go":    "package listed\n",
	"ignored.go":         "//go:build ignore\n\npackage listed\n",
	"data.txt":           "data\n",
	"static/a.txt":       "a\n",
	"static/.hidden.txt": "hidden\n",
	"static/_under.txt":  "under\n",
	"hidden/.dot.txt":    "dot\n",
	"hidden/plain.txt":   "plain\n",

	"internal/helper/helper.go": "package helper\n\n// H is one.\nfunc H() int { return 1 }\n",
	"command/tool/main.go": "package main\n\nimport (\n\t\"os/user\"\n\n\t\"example.com/dependency\"\n)\n\n" +
		"func main() { _, _ = user.Current(); _ = dependency.D() }\n",
	"testsonly/only_test.go":        "package testsonly\n\nimport \"testing\"\n\nfunc TestOnly(test *testing.T) {}\n",
	"allignored/ignored.go":         "//go:build ignore\n\npackage allignored\n",
	"cgo/cgo.go":                    "package cgo\n\n// int one(void) { return 1; }\nimport \"C\"\n\n// One is one.\nfunc One() int { return int(C.one()) }\n",
	"testdata/skipped.go":           "package skipped\n",
	"_skipped/skipped.go":           "package skipped\n",
	".skipped/skipped.go":           "package skipped\n",
	"documents/readme.md":           "# Not a package\n",
	"scripts/vendor/tool.js":        "// A vendor directory with no Go in it.\n",
	"scripts/vendor/inner/inner.go": "package inner\n",
	"dependency_module/go.mod":      "module example.com/dependency\n\ngo 1.27\n\nrequire example.com/transitive v1.0.0\n",
	"dependency_module/dependency.go": "package dependency\n\nimport \"example.com/transitive\"\n\n" +
		"// D is one.\nfunc D() int { return transitive.T() }\n",
	"transitive_module/go.mod":        "module example.com/transitive\n\ngo 1.27\n",
	"transitive_module/transitive.go": "package transitive\n\n// T is one.\nfunc T() int { return 1 }\n",
}

// listedWorkspaceFiles are a go.work of three modules, one nested in another and one whose path is below another's,
// so a module pattern reaches into a module of its own and the walk of `./...` stops at each.
var listedWorkspaceFiles = map[string]string{
	"go.work": "go 1.27\n\nuse (\n\t.\n\t./shim\n\t./shim/nested\n)\n",
	"go.mod":  "module example.com/root\n\ngo 1.27\n",
	"root.go": "package root\n\nimport (\n\t\"example.com/shim\"\n\t\"example.com/shim/nested\"\n)\n\n" +
		"// R is two.\nfunc R() int { return shim.S() + nested.N() }\n",
	"shim/go.mod":           "module example.com/shim\n\ngo 1.27\n",
	"shim/shim.go":          "package shim\n\nimport \"example.com/shim/inner\"\n\n// S is one.\nfunc S() int { return inner.I() }\n",
	"shim/inner/inner.go":   "package inner\n\n// I is one.\nfunc I() int { return 1 }\n",
	"shim/nested/go.mod":    "module example.com/shim/nested\n\ngo 1.27\n",
	"shim/nested/nested.go": "package nested\n\n// N is one.\nfunc N() int { return 1 }\n",
}

// The in-process lister's answer is go list's, package for package and field for field, in go list's order, on a
// module under each cgo setting and on a workspace, so the load cannot tell which of the two listed.
func TestInProcessListingIsGoListsAnswer(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name        string
		files       map[string]string
		environment []string
		patterns    []string

		// testOnly is a package only the roots' tests import, which go list must have listed for the second
		// listing to be compared at all.
		testOnly string
	}{
		{"ModuleWithCgo", listedModuleFiles, []string{"GOWORK=off", "CGO_ENABLED=1"}, []string{"./..."},
			"testing/fstest"},
		{"ModuleWithoutCgo", listedModuleFiles, []string{"GOWORK=off", "CGO_ENABLED=0"}, []string{"./..."},
			"testing/fstest"},
		{"Workspace", listedWorkspaceFiles, nil, []string{"./...", "example.com/shim/...",
			"example.com/shim/nested/..."}, ""},
		// One directory each, as the editor's save lists its file's package: the module's own, with its tests, one
		// below it, and in a workspace one inside a module nested in another.
		{"ModuleDirectory", listedModuleFiles, []string{"GOWORK=off", "CGO_ENABLED=0"}, []string{"."},
			"testing/fstest"},
		{"ModuleSubdirectory", listedModuleFiles, []string{"GOWORK=off", "CGO_ENABLED=0"},
			[]string{"./internal/helper"}, ""},
		{"WorkspaceDirectory", listedWorkspaceFiles, nil, []string{"./shim/inner", "./shim/nested"}, ""},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			directory := test.TempDir()
			writeListingFiles(test, directory, each.files)
			goList := requireGoListsAnswer(test, directory, each.environment, each.patterns)
			if each.testOnly != "" && !goList.testOnly[each.testOnly] {
				test.Errorf(
					"go list did not list %s for the tests alone: %v",
					each.testOnly,
					sortedKeys(goList.testOnly),
				)
			}
		})
	}
}

// The same holds on cohere's own repository, listed as the engine lists it: the workspace's patterns and environment
// from project.ReadWorkspace, every package of cohere, its dependencies and the standard library.
func TestInProcessListingIsGoListsAnswerOnCohere(test *testing.T) {
	test.Parallel()
	root, absoluteError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	workspace, workspaceError := project.ReadWorkspace(root)
	if workspaceError != nil {
		test.Fatal(workspaceError)
	}
	requireGoListsAnswer(test, workspace.Root, workspace.Environment(), workspace.Patterns())
}

// requireGoListsAnswer fails unless the in-process lister answers in directory and its answer is go list's, and
// returns go list's.
func requireGoListsAnswer(test *testing.T, directory string, extra []string, patterns []string) *listing {
	test.Helper()
	goEnvironment := goEnvironmentOf(test, directory, extra)
	inProcess, doubt := listInProcess(goEnvironment, patterns, nil, nil, nil, nil)
	if doubt != "" {
		test.Fatalf("the in-process lister did not answer: %s", doubt)
	}
	goList, listError := listWithGoList(goEnvironment.guard, directory, goEnvironment.fullEnvironment, patterns,
		nil)
	if listError != nil {
		test.Fatal(listError)
	}
	if len(inProcess.packages) != len(goList.packages) {
		test.Errorf("listed %d packages, go list %d", len(inProcess.packages), len(goList.packages))
	}
	for index := range min(len(inProcess.packages), len(goList.packages)) {
		listed, wanted := inProcess.packages[index], goList.packages[index]
		if listed.ImportPath != wanted.ImportPath {
			test.Fatalf("package %d is %s, go list's is %s", index, listed.ImportPath, wanted.ImportPath)
		}
		if !reflect.DeepEqual(listed, wanted) {
			test.Errorf("%s differs from go list's: %s", listed.ImportPath, fieldDifferences(listed, wanted))
		}
	}
	if !reflect.DeepEqual(inProcess.roots, goList.roots) || !reflect.DeepEqual(inProcess.testOnly, goList.testOnly) {
		test.Errorf("roots %v and test-only %v, go list's %v and %v", sortedKeys(inProcess.roots),
			sortedKeys(inProcess.testOnly), sortedKeys(goList.roots), sortedKeys(goList.testOnly))
	}
	if len(goList.roots) == 0 {
		test.Error("go list listed no roots, so the comparison proves nothing")
	}
	return goList
}

// fieldDifferences names each field of two listed packages that differs, with both values.
func fieldDifferences(listed *listedPackage, wanted *listedPackage) string {
	var differences []string
	listedValue, wantedValue := reflect.ValueOf(listed).Elem(), reflect.ValueOf(wanted).Elem()
	for index := range listedValue.NumField() {
		left, right := listedValue.Field(index), wantedValue.Field(index)
		if reflect.DeepEqual(left.Interface(), right.Interface()) {
			continue
		}
		differences = append(differences, listedValue.Type().Field(index).Name+": "+asJson(left)+", go list's "+
			asJson(right))
	}
	return strings.Join(differences, "; ")
}

// asJson is value as JSON, or as Go prints it when it has no JSON form, so a difference is always shown.
func asJson(value reflect.Value) string {
	encoded, encodeError := json.Marshal(value.Interface())
	if encodeError != nil {
		return fmt.Sprintf("%+v", value.Interface())
	}
	return string(encoded)
}

// What go list would report as an error, and what the lister does not carry, is a doubt, never a listing of its
// own: each module here is one such thing, and the lister answers none of them, so go list does.
func TestInProcessListingDoubtsWhatItDoesNotCarry(test *testing.T) {
	test.Parallel()
	module := "module example.com/doubted\n\ngo 1.27\n"
	cases := []struct {
		name        string
		files       map[string]string
		environment []string

		// reason is a piece of the doubt, so each module is proven doubted for its own reason and not another's.
		reason string
	}{
		{
			"MissingImport",
			map[string]string{"go.mod": module, "a.go": "package a\n\nimport _ \"example.com/missing\"\n"},
			nil,
			"example.com/missing is in 0 modules",
		},
		{"Cycle", map[string]string{
			"go.mod": module,
			"a/a.go": "package a\n\nimport _ \"example.com/doubted/b\"\n",
			"b/b.go": "package b\n\nimport _ \"example.com/doubted/a\"\n",
		}, nil, "in an import cycle"},
		{"InternalFromOutside", map[string]string{
			"go.mod":            module,
			"a/internal/x/x.go": "package x\n",
			"b/b.go":            "package b\n\nimport _ \"example.com/doubted/a/internal/x\"\n",
		}, nil, "an internal package of another tree"},
		{
			"Vendor",
			map[string]string{"go.mod": module, "a.go": "package a\n", "vendor/modules.txt": ""},
			nil,
			"vendor directory",
		},
		{
			"IgnoreDirective",
			map[string]string{"go.mod": module + "\nignore ./unused\n", "a.go": "package a\n"},
			nil,
			"an ignore or a godebug directive",
		},
		{"EmbedMatchesNothing", map[string]string{
			"go.mod": module,
			"a.go":   "package a\n\nimport _ \"embed\"\n\n//go:embed missing.txt\nvar missing string\n",
		}, nil, "matches no file"},
		{
			"FlagBeyondTheLister",
			map[string]string{"go.mod": module, "a.go": "package a\n"},
			[]string{"GOFLAGS=-race"},
			"GOFLAGS holds -race",
		},
		{"NotParsing", map[string]string{"go.mod": module, "a.go": "packge a\n"}, nil, "expected 'package'"},
		{
			"MultiplePackages",
			map[string]string{"go.mod": module, "a.go": "package a\n", "b.go": "package b\n"},
			nil,
			"found packages a (a.go) and b (b.go)",
		},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			directory := test.TempDir()
			writeListingFiles(test, directory, each.files)
			goEnvironment := goEnvironmentOf(test, directory, append([]string{"GOWORK=off"},
				each.environment...))
			listed, doubt := listInProcess(goEnvironment, []string{"./..."}, nil, nil, nil, nil)
			if doubt == "" {
				test.Fatalf("the lister answered with %d packages", len(listed.packages))
			}
			if !strings.Contains(doubt, each.reason) {
				test.Errorf("the lister doubted %q, not for %q", doubt, each.reason)
			}
		})
	}
}

// A pattern the lister cannot match by module, a directory below or outside the workspace, or one that matches
// nothing, is doubted, so go list answers it: an empty listing would check nothing and say nothing.
func TestAPatternTheListerCannotMatchIsDoubted(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, map[string]string{
		"go.mod":   "module example.com/patterned\n\ngo 1.27\n",
		"a.go":     "package a\n",
		"sub/b.go": "package b\n",
	})
	goEnvironment := goEnvironmentOf(test, directory, []string{"GOWORK=off"})
	cases := []struct {
		pattern string
		reason  string
	}{
		{"./sub/...", "names a directory"},
		{filepath.Join(directory, "sub") + "/...", "names a directory"},
		{"example.com/patterned/missing/...", "matched no packages"},
	}
	for _, each := range cases {
		listed, doubt := listInProcess(goEnvironment, []string{each.pattern}, nil, nil, nil, nil)
		if doubt == "" {
			test.Errorf("the lister answered %s with %d packages", each.pattern, len(listed.packages))
			continue
		}
		if !strings.Contains(doubt, each.reason) {
			test.Errorf("the lister doubted %s for %q, not for %q", each.pattern, doubt, each.reason)
		}
	}
	// Through list, the directory pattern is go list's to answer, and it names the package below it.
	listed, listError := list(goEnvironment, []string{"./sub/..."}, nil, nil, nil, nil)
	if listError != nil {
		test.Fatal(listError)
	}
	if listed.inProcess || listed.byPath["example.com/patterned/sub"] == nil {
		test.Errorf("list of ./sub/... answered in process %v with %v, want go list's answer naming "+
			"example.com/patterned/sub", listed.inProcess, sortedKeys(listed.roots))
	}
}

// A doubt hands the listing to go list, which says what is wrong as the package's own error, and the listing says why
// go list was asked.
func TestADoubtIsGoListsToAnswer(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, map[string]string{
		"go.mod": "module example.com/doubted\n\ngo 1.27\n",
		"a.go":   "package a\n\nimport _ \"example.com/missing\"\n",
	})
	goEnvironment := goEnvironmentOf(test, directory, []string{"GOWORK=off"})
	listed, listError := list(goEnvironment, []string{"./..."}, nil, nil, nil, nil)
	if listError != nil {
		test.Fatal(listError)
	}
	if listed.inProcess || listed.goListReason == "" {
		test.Fatalf("listed in process %t, with go list's reason %q", listed.inProcess, listed.goListReason)
	}
	if missing := listed.byPath["example.com/missing"]; missing == nil || missing.Error == nil {
		test.Errorf("go list listed the missing import as %+v", missing)
	}
}
