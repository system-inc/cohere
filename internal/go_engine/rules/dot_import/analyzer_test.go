package dot_import

import (
	"go/build"
	"go/version"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/stylecheck/st1001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// versionFolders is staticcheck's own fixtures for `ST1001`, copied unchanged from stylecheck/st1001/testdata, one
// folder for each Go version staticcheck's testutil loads them under. Each holds the go.mod testutil lays over its
// folder, `module example.com` at the folder's version, written to disk here since the engine's load reads go.mod from
// disk.
func versionFolders(test *testing.T) []string {
	test.Helper()
	folders, globError := filepath.Glob(filepath.Join(analysistest.TestData(), "go1*"))
	if globError != nil {
		test.Fatal(globError)
	}
	if len(folders) == 0 {
		test.Fatal("testdata holds no version folder of staticcheck's fixtures, so the comparison would prove nothing")
	}
	return folders
}

// loadableVersion is the Go version folder loads under, after it is held to what staticcheck's testutil asks of one:
// a valid Go version, laid out with testutil's go.mod. A folder newer than the toolchain is skipped, as testutil skips
// it.
func loadableVersion(test *testing.T, folder string) string {
	test.Helper()
	folderVersion := filepath.Base(folder)
	if !version.IsValid(folderVersion) {
		test.Fatalf("%s is not a valid Go version", folder)
	}
	releaseTags := build.Default.ReleaseTags
	if newest := releaseTags[len(releaseTags)-1]; version.Compare(folderVersion, newest) == 1 {
		test.Skipf("%s is newer than the toolchain's Go version (%s), as staticcheck's testutil skips it",
			folderVersion, newest)
	}
	moduleFile, readError := os.ReadFile(filepath.Join(folder, "go.mod"))
	if readError != nil {
		test.Fatalf("reading the go.mod testutil lays over %s: %v", folder, readError)
	}
	// testutil's own overlay, `module example.com\ngo 1.0`, with the newline gofmt and editors leave at the end.
	if wanted := "module example.com\ngo " + folderVersion[len("go"):] + "\n"; string(moduleFile) != wanted {
		test.Fatalf("%s/go.mod reads %q, where staticcheck's testutil lays %q over the folder", folder, moduleFile,
			wanted)
	}
	return folderVersion
}

// moduleEnvironment is the environment a version folder loads under, a module with nothing to fetch, as parity loads a
// fixture that holds a go.mod.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// Every `//@ diag(...)` note staticcheck's fixtures hold, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects, in each version folder loaded as staticcheck's testutil
// loads it. testutil checks each package it loads on its own, the package and its test variant each, against the notes
// of that package's files, so a note in a file both hold is met in each. The fixture's test file is in the package
// itself, so the package's file is in both and its one note is met twice, once in each, as here.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, folder := range versionFolders(test) {
		test.Run(filepath.Base(folder), func(test *testing.T) {
			test.Parallel()
			loadableVersion(test, folder)
			wants, readError := staticcheck_wants.Read(folder)
			if readError != nil {
				test.Fatal(readError)
			}
			if len(wants) == 0 {
				test.Fatal("the fixture holds no notes, so the comparison proves nothing")
			}
			loaded, loadError := load.Packages(folder, []string{"./..."}, moduleEnvironment, nil)
			if loadError != nil {
				test.Fatal(loadError)
			}
			if len(loaded.Roots) == 0 {
				test.Fatal("loading the fixture found no package, so the comparison would prove nothing")
			}
			for _, failure := range parity.LoadFailures(loaded.Roots) {
				test.Fatalf("fixture package %s does not load clean: %s", failure.Package, failure.Error)
			}
			// heldWants counts the wants some package's files hold, so a note in a file no package loads is a
			// failure rather than a want nobody asked about.
			heldWants := map[int]bool{}
			for _, root := range loaded.Roots {
				files := map[string]bool{}
				for _, file := range root.CompiledGoFiles {
					files[file] = true
				}
				packageWants := []staticcheck_wants.Want{}
				for index, want := range wants {
					if files[want.File] {
						packageWants = append(packageWants, want)
						heldWants[index] = true
					}
				}
				outcomes, analyzeError := parity.Analyze(
					[]parity.Pair{{Name: Name, Original: st1001.Analyzer, Port: Analyzer}},
					[]*packages.Package{root},
				)
				if analyzeError != nil {
					test.Fatal(analyzeError)
				}
				for _, failure := range outcomes[0].Port.Failures {
					test.Fatalf("the port did not run on %s: %s", failure.Package, failure.Error)
				}
				found := []staticcheck_wants.Finding{}
				for _, finding := range outcomes[0].Port.Findings {
					found = append(found, staticcheck_wants.Finding{
						File: finding.File, Line: finding.Line, Message: finding.Message,
					})
				}
				test.Logf("in %s, the fixture expects %d, the port found %d", root.ID, len(packageWants), len(found))
				unexpected, untaken := staticcheck_wants.Compare(packageWants, found)
				for _, extra := range unexpected {
					test.Errorf("%s: %s:%d: the port reported %q, which no note on the line expects", root.ID,
						extra.File, extra.Line, extra.Message)
				}
				for _, missing := range untaken {
					test.Errorf("%s: the port reported nothing matching %s", root.ID, missing)
				}
			}
			for index, want := range wants {
				if !heldWants[index] {
					test.Errorf("%s is in a file no package of the fixture holds, so nothing could meet it", want)
				}
			}
		})
	}
}

// Over staticcheck's fixtures, each version folder loaded as its testutil loads it, the original and the port find
// the same findings, each the same in every field.
func TestPortMatchesOriginalOverUpstreamFixtures(test *testing.T) {
	test.Parallel()
	for _, folder := range versionFolders(test) {
		test.Run(filepath.Base(folder), func(test *testing.T) {
			test.Parallel()
			loadableVersion(test, folder)
			parity.RequireIdentical(test, st1001.Analyzer, Analyzer, folder, "./...")
		})
	}
}

// Over parity's fixtures, every import shape the check reads and every near miss, in a package under no
// staticcheck.conf and in one under a staticcheck.conf allowing some paths, the two find the same findings. The port
// differs from the original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1001.Analyzer, Analyzer, analysistest.TestData(), "parity/...")
}
