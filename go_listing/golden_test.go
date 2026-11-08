package go_listing_test

import (
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/system-inc/cohere/go_listing"
)

// testdata/fixture.json is go1.27.0's `go list -e -test -json ./... unsafe` of fixtureModule on
// darwin/arm64, its bytes as go printed them but for three absolute prefixes respelled so the golden
// names no machine: the fixture's root as /fixture, `GOROOT` as /goroot and `GOCACHE` as /gocache. Without
// -deps it holds the standard library's unsafe alone.
func goldenPackages(test *testing.T) []go_listing.Package {
	test.Helper()
	output, readError := os.ReadFile("testdata/fixture.json")
	if readError != nil {
		test.Fatal(readError)
	}
	packages, decodeError := go_listing.Decode(output)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	return packages
}

// Decode reads every object of the golden, in go's order, each field as go printed it.
func TestDecodeReadsTheGoldenAsGoPrintedIt(test *testing.T) {
	test.Parallel()
	packages := goldenPackages(test)
	var importPaths []string
	for _, listed := range packages {
		importPaths = append(importPaths, listed.ImportPath)
	}
	if want := []string{
		"example.com/fixture",
		"example.com/fixture/broken",
		"example.com/fixture/cg",
		"example.com/fixture/dep",
		"example.com/fixture/user",
		"unsafe",
		"example.com/fixture.test",
		"example.com/fixture [example.com/fixture.test]",
		"example.com/fixture_test [example.com/fixture.test]",
		"example.com/fixture/dep.test",
		"example.com/fixture/dep [example.com/fixture/dep.test]",
	}; !slices.Equal(importPaths, want) {
		test.Errorf("the golden's packages are\n%q\nwant\n%q", importPaths, want)
	}
	module := &go_listing.Module{
		Path:      "example.com/fixture",
		Main:      true,
		Directory: "/fixture",
		GoMod:     "/fixture/go.mod",
		GoVersion: "1.21",
	}
	brokenError := &go_listing.PackageError{
		Position:    "broken/broken.go:3:8",
		Message:     "string literal not terminated",
		ImportStack: []string{"example.com/fixture/broken"},
	}
	for _, want := range []go_listing.Package{
		{
			ImportPath:     "example.com/fixture/broken",
			Name:           "broken",
			Directory:      "/fixture/broken",
			Module:         module,
			Incomplete:     true,
			GoFiles:        []string{"broken.go"},
			InvalidGoFiles: []string{"broken.go"},
			Error:          brokenError,
		},
		{
			ImportPath: "example.com/fixture/user",
			Name:       "user",
			Directory:  "/fixture/user",
			Module:     module,
			Incomplete: true,
			GoFiles:    []string{"user.go"},
			Imports:    []string{"example.com/fixture/broken"},
			Deps:       []string{"example.com/fixture/broken"},
			DepsErrors: []*go_listing.PackageError{brokenError},
		},
		{
			ImportPath: "unsafe",
			Name:       "unsafe",
			Directory:  "/goroot/src/unsafe",
			Standard:   true,
			GoFiles:    []string{"unsafe.go"},
		},
	} {
		if got := byImportPath(test, packages, want.ImportPath); !reflect.DeepEqual(got, want) {
			test.Errorf("%s decodes as\n%+v\nwant\n%+v", want.ImportPath, got, want)
		}
	}
	cg := byImportPath(test, packages, "example.com/fixture/cg")
	if !slices.Equal(cg.CgoCFlags, []string{"-I/fixture/cg/../include", "-I", "/fixture/cg/include2"}) ||
		!slices.Equal(cg.Imports, []string{"C"}) ||
		!slices.Contains(cg.Deps, "runtime/cgo") {
		test.Errorf("cg decodes as %+v", cg)
	}
	external := byImportPath(test, packages, "example.com/fixture_test [example.com/fixture.test]")
	if external.ForTest != "example.com/fixture" ||
		!reflect.DeepEqual(
			external.ImportMap,
			map[string]string{"example.com/fixture": "example.com/fixture [example.com/fixture.test]"},
		) {
		test.Errorf("the external test decodes as %+v", external)
	}
}

// SourceFiles, CgoIncludeDirectories, Reach and ReachError answer from the golden as from a live run.
func TestTheGoldensFilesAndReach(test *testing.T) {
	test.Parallel()
	packages := goldenPackages(test)
	for _, each := range []struct {
		importPath string
		lists      []go_listing.FileList
		want       []string
	}{
		{"example.com/fixture", go_listing.BuildFileLists(), []string{"lib.go", "message.txt"}},
		{
			"example.com/fixture",
			[]go_listing.FileList{
				go_listing.IgnoredGoFiles,
				go_listing.IgnoredOtherFiles,
				go_listing.TestGoFiles,
				go_listing.XTestGoFiles,
				go_listing.XTestEmbedFiles,
			},
			[]string{"never_test.go", "other.go", "asm_plan9.s", "lib_test.go", "x_test.go", "expected.txt"},
		},
		{"example.com/fixture/cg", go_listing.BuildFileLists(), []string{"cg.go", "cg.c", "cg.h"}},
		{
			"example.com/fixture.test",
			go_listing.BuildFileLists(),
			[]string{"/gocache/a8/a8a16cca7f4d86baa576e1f061fb000c0cc758eed07969399f8e7d18508d6010-d"},
		},
		{"unsafe", go_listing.BuildFileLists(), []string{"unsafe.go"}},
	} {
		if got := byImportPath(test, packages, each.importPath).SourceFiles(each.lists); !slices.Equal(got, each.want) {
			test.Errorf("%s's %q are %q, want %q", each.importPath, each.lists, got, each.want)
		}
	}
	testMain := byImportPath(test, packages, "example.com/fixture.test")
	if path := testMain.Path(testMain.GoFiles[0]); path != testMain.GoFiles[0] {
		test.Errorf("the test main's absolute file is placed at %s", path)
	}
	if path := byImportPath(test, packages, "unsafe").Path("unsafe.go"); path != "/goroot/src/unsafe/unsafe.go" {
		test.Errorf("unsafe.go is placed at %s", path)
	}
	if got, want := byImportPath(test, packages, "example.com/fixture/cg").CgoIncludeDirectories(), []string{
		"/fixture/include",
		"/fixture/cg/include2",
	}; !slices.Equal(got, want) {
		test.Errorf("cg's include directories are %q, want %q", got, want)
	}
	// Without -deps the listing holds only what the patterns matched, so the reach is those of root's
	// dependencies the listing holds: here unsafe, which cg imports.
	var reach []string
	for _, listed := range go_listing.Reach(packages, "example.com/fixture") {
		reach = append(reach, listed.ImportPath)
	}
	if want := []string{
		"example.com/fixture",
		"example.com/fixture/cg",
		"example.com/fixture/dep",
		"unsafe",
		"example.com/fixture.test",
		"example.com/fixture [example.com/fixture.test]",
		"example.com/fixture_test [example.com/fixture.test]",
	}; !slices.Equal(reach, want) {
		test.Errorf("the root's reach is\n%q\nwant\n%q", reach, want)
	}
	if reachError := go_listing.ReachError(go_listing.Reach(packages, "example.com/fixture/user")); reachError == nil {
		test.Error("user's reach holds broken's error, and ReachError names none")
	}
}
