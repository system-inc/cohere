package go_listing_test

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/go_listing"
)

// listFixture is the real `go list -e -deps -test -json ./...` of a fresh fixture, run through Run,
// with the fixture's root.
func listFixture(test *testing.T) ([]go_listing.Package, string) {
	test.Helper()
	root := writeFixture(test)
	arguments, argumentsError := go_listing.Arguments(go_listing.Request{
		Errors:       true,
		Dependencies: true,
		Tests:        true,
		Patterns:     []string{"./..."},
	})
	if argumentsError != nil {
		test.Fatal(argumentsError)
	}
	packages, runError := go_listing.Run(goCommand(test, root, arguments))
	if runError != nil {
		test.Fatal(runError)
	}
	return packages, root
}

// byImportPath is the listed package of importPath, failing the test when the listing holds none.
func byImportPath(test *testing.T, packages []go_listing.Package, importPath string) go_listing.Package {
	test.Helper()
	for _, listed := range packages {
		if listed.ImportPath == importPath {
			return listed
		}
	}
	test.Fatalf("the listing holds no %s", importPath)
	return go_listing.Package{}
}

// moduleImportPaths are the import paths of packages that aren't standard, sorted.
func moduleImportPaths(packages []go_listing.Package) []string {
	var paths []string
	for _, listed := range packages {
		if !listed.Standard {
			paths = append(paths, listed.ImportPath)
		}
	}
	sort.Strings(paths)
	return paths
}

// Run of the real go list names exactly the fixture's packages, their variants and test mains, and
// every standard package they import.
func TestRunListsExactlyTheFixturesPackages(test *testing.T) {
	test.Parallel()
	packages, _ := listFixture(test)
	if got, want := moduleImportPaths(packages), []string{
		"example.com/fixture",
		"example.com/fixture [example.com/fixture.test]",
		"example.com/fixture.test",
		"example.com/fixture/broken",
		"example.com/fixture/cg",
		"example.com/fixture/dep",
		"example.com/fixture/dep [example.com/fixture/dep.test]",
		"example.com/fixture/dep.test",
		"example.com/fixture/user",
		"example.com/fixture_test [example.com/fixture.test]",
	}; !slices.Equal(got, want) {
		test.Errorf("the fixture's packages are\n%q\nwant\n%q", got, want)
	}
	for _, standard := range []string{"embed", "testing", "runtime/cgo", "unsafe"} {
		if !byImportPath(test, packages, standard).Standard {
			test.Errorf("%s isn't listed as standard", standard)
		}
	}
	// -deps lists every dependency before what imports it.
	seen := map[string]bool{}
	for _, listed := range packages {
		for _, dependency := range listed.Deps {
			if !seen[dependency] {
				test.Errorf("%s is listed before its dependency %s", listed.ImportPath, dependency)
			}
		}
		seen[listed.ImportPath] = true
	}
}

// SourceFiles names exactly the files each list holds, in the lists the caller chose, and every file
// any list names is one the fixture holds (or, for the test main, one go wrote into its cache).
func TestSourceFilesNamesExactlyTheFixturesFiles(test *testing.T) {
	test.Parallel()
	packages, root := listFixture(test)
	every := []go_listing.FileList{
		go_listing.GoFiles,
		go_listing.CgoFiles,
		go_listing.CFiles,
		go_listing.CxxFiles,
		go_listing.MFiles,
		go_listing.HFiles,
		go_listing.FFiles,
		go_listing.SFiles,
		go_listing.SwigFiles,
		go_listing.SwigCxxFiles,
		go_listing.SysoFiles,
		go_listing.EmbedFiles,
		go_listing.IgnoredGoFiles,
		go_listing.IgnoredOtherFiles,
		go_listing.InvalidGoFiles,
		go_listing.CompiledGoFiles,
		go_listing.TestGoFiles,
		go_listing.XTestGoFiles,
		go_listing.TestEmbedFiles,
		go_listing.XTestEmbedFiles,
	}
	for _, each := range []struct {
		importPath string
		lists      []go_listing.FileList
		want       []string
	}{
		{"example.com/fixture", go_listing.BuildFileLists(), []string{"lib.go", "message.txt"}},
		{
			"example.com/fixture",
			[]go_listing.FileList{go_listing.IgnoredGoFiles, go_listing.IgnoredOtherFiles},
			[]string{"never_test.go", "other.go", "asm_plan9.s"},
		},
		{
			"example.com/fixture",
			[]go_listing.FileList{go_listing.TestGoFiles, go_listing.XTestGoFiles, go_listing.XTestEmbedFiles},
			[]string{"lib_test.go", "x_test.go", "expected.txt"},
		},
		// A test variant names its test files in the build's lists: they are its build's.
		{
			"example.com/fixture [example.com/fixture.test]",
			go_listing.BuildFileLists(),
			[]string{"lib.go", "lib_test.go", "message.txt"},
		},
		{
			"example.com/fixture_test [example.com/fixture.test]",
			go_listing.BuildFileLists(),
			[]string{"x_test.go", "expected.txt"},
		},
		{"example.com/fixture/cg", go_listing.BuildFileLists(), []string{"cg.go", "cg.c", "cg.h"}},
		{"example.com/fixture/dep", go_listing.BuildFileLists(), []string{"dep.go"}},
		// go list fills a dependency's test files too, which no binary but its own compiles.
		{"example.com/fixture/dep", []go_listing.FileList{go_listing.TestGoFiles}, []string{"dep_test.go"}},
		{"example.com/fixture/broken", every, []string{"broken.go", "broken.go"}},
		{"example.com/fixture/user", every, []string{"user.go"}},
		{"example.com/fixture", nil, nil},
	} {
		listed := byImportPath(test, packages, each.importPath)
		if got := listed.SourceFiles(each.lists); !slices.Equal(got, each.want) {
			test.Errorf("%s's %q are %q, want %q", each.importPath, each.lists, got, each.want)
		}
	}
	named := map[string]bool{}
	for _, listed := range packages {
		if listed.Standard {
			continue
		}
		for _, name := range listed.SourceFiles(every) {
			path := listed.Path(name)
			if listed.IsTestMain() {
				if _, statError := os.Stat(path); statError != nil || strings.HasPrefix(path, root) {
					test.Errorf(
						"%s's test main names %s, which isn't a file of go's cache (%v)",
						listed.ImportPath,
						path,
						statError,
					)
				}
				continue
			}
			relative, relativeError := filepath.Rel(root, path)
			if relativeError != nil {
				test.Fatal(relativeError)
			}
			named[filepath.ToSlash(relative)] = true
		}
	}
	var want []string
	for name := range fixtureModule {
		// go.mod is the module's, and the headers outside cg's directory are named by its flags alone.
		if name != "go.mod" && name != "include/shared.h" && name != "cg/include2/local.h" {
			want = append(want, name)
		}
	}
	sort.Strings(want)
	var got []string
	for name := range named {
		got = append(got, name)
	}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		test.Errorf("the listing names\n%q\nwant\n%q", got, want)
	}
}

// Reach is root, its variants and test main and their dependencies, never another root's, and
// ReachError refuses the reach that holds a package's error and no other.
func TestReachIsWhatABuildOfTheRootReads(test *testing.T) {
	test.Parallel()
	packages, root := listFixture(test)
	if got, want := moduleImportPaths(go_listing.Reach(packages, "example.com/fixture")), []string{
		"example.com/fixture",
		"example.com/fixture [example.com/fixture.test]",
		"example.com/fixture.test",
		"example.com/fixture/cg",
		"example.com/fixture/dep",
		"example.com/fixture_test [example.com/fixture.test]",
	}; !slices.Equal(got, want) {
		test.Errorf("the root's reach is\n%q\nwant\n%q", got, want)
	}
	reach := go_listing.Reach(packages, "example.com/fixture")
	for _, standard := range []string{"embed", "testing", "runtime/cgo"} {
		byImportPath(test, reach, standard)
	}
	if reachError := go_listing.ReachError(reach); reachError != nil {
		test.Errorf("the root's reach has no error, and ReachError says %v", reachError)
	}
	if got := moduleImportPaths(go_listing.Reach(packages, "example.com/fixture/user")); !slices.Equal(
		got,
		[]string{"example.com/fixture/broken", "example.com/fixture/user"},
	) {
		test.Errorf("user's reach is %q", got)
	}
	if reach := go_listing.Reach(packages, "example.com/missing"); len(reach) != 0 {
		test.Errorf("a root the listing doesn't hold reaches %d packages", len(reach))
	}

	broken := byImportPath(test, packages, "example.com/fixture/broken")
	wantError := go_listing.PackageError{
		Position:    "broken/broken.go:3:8",
		Message:     "string literal not terminated",
		ImportStack: []string{"example.com/fixture/broken"},
	}
	// The import stack is the path go first loaded broken by: under ./... that is broken itself, matched
	// before user is, so user's dependency error names the same stack.
	sameError := func(got *go_listing.PackageError) bool {
		return got != nil &&
			got.Position == wantError.Position &&
			got.Message == wantError.Message &&
			slices.Equal(got.ImportStack, wantError.ImportStack)
	}
	if !sameError(broken.Error) || !broken.Incomplete {
		test.Errorf("broken's error is %+v, want %+v", broken.Error, wantError)
	}
	user := byImportPath(test, packages, "example.com/fixture/user")
	if user.Error != nil || len(user.DepsErrors) != 1 || !sameError(user.DepsErrors[0]) || !user.Incomplete {
		test.Errorf("user's own error is %+v and its dependencies' %+v", user.Error, user.DepsErrors)
	}
	// broken comes first in the reach, so its own error is the one named.
	reachError := go_listing.ReachError(go_listing.Reach(packages, "example.com/fixture/user"))
	if reachError == nil ||
		reachError.Error() != "example.com/fixture/broken: broken/broken.go:3:8: string literal not terminated" {
		test.Errorf("user's reach error is %v", reachError)
	}
	// Without broken, the error is named through user's DepsErrors.
	reachError = go_listing.ReachError([]go_listing.Package{user})
	if reachError == nil || !strings.HasPrefix(reachError.Error(), "example.com/fixture/user, through a dependency: ") {
		test.Errorf("user alone's reach error is %v", reachError)
	}

	cg := byImportPath(test, packages, "example.com/fixture/cg")
	if got, want := cg.CgoIncludeDirectories(), []string{
		filepath.Join(root, "include"),
		filepath.Join(root, "cg", "include2"),
	}; !slices.Equal(got, want) {
		test.Errorf("cg's include directories are %q, want %q", got, want)
	}
	fixture := byImportPath(test, packages, "example.com/fixture")
	if fixture.Module == nil ||
		fixture.Module.Path != "example.com/fixture" ||
		!fixture.Module.Main ||
		fixture.Module.GoMod != filepath.Join(root, "go.mod") ||
		fixture.Directory != root ||
		fixture.DepOnly {
		test.Errorf("the root's module is %+v in %s", fixture.Module, fixture.Directory)
	}
	if !byImportPath(test, packages, "example.com/fixture.test").IsTestMain() ||
		byImportPath(test, packages, "example.com/fixture [example.com/fixture.test]").IsTestMain() ||
		fixture.IsTestMain() {
		test.Error("IsTestMain is the test main's alone")
	}
	if owner := byImportPath(test, packages, "example.com/fixture_test [example.com/fixture.test]").Owner(); owner !=
		"example.com/fixture_test" {
		test.Errorf("the external test's owner is %q", owner)
	}
}

// Run carries go's standard error when go fails, and refuses a command whose output is already
// someone else's.
func TestRunNamesAFailedGoList(test *testing.T) {
	test.Parallel()
	root := writeFixture(test)
	// Without -e a package's error fails the whole run.
	packages, runError := go_listing.Run(goCommand(test, root, []string{"list", "-deps", "-json", "./user"}))
	if runError == nil || !strings.Contains(runError.Error(), "string literal not terminated") || packages != nil {
		test.Errorf("a failed go list is %d packages and %v", len(packages), runError)
	}
	command := goCommand(test, root, []string{"list", "-json", "./dep"})
	command.Stdout = os.Stdout
	if _, runError := go_listing.Run(command); runError == nil {
		test.Error("a command whose output is set ran")
	}
}
