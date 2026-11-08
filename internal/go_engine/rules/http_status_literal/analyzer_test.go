package http_status_literal

import (
	"go/build"
	"go/version"
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/stylecheck/st1013"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// versionFolders is staticcheck's own fixtures for `ST1013`, copied unchanged from stylecheck/st1013/testdata, one
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

// Every `//@ diag(...)` note staticcheck's fixtures hold, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects, in each version folder loaded as staticcheck's testutil
// loads it.
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
			// The findings name a file by its path below the folder, so each want is named the same way.
			for index := range wants {
				below, relativeError := filepath.Rel(folder, wants[index].File)
				if relativeError != nil {
					test.Fatal(relativeError)
				}
				wants[index].File = filepath.ToSlash(below)
			}
			_, portFindings := parity.Findings(test, st1013.Analyzer, Analyzer, folder, "./...")
			found := []staticcheck_wants.Finding{}
			for _, finding := range portFindings {
				found = append(found, staticcheck_wants.Finding{
					File: finding.File, Line: finding.Line, Message: finding.Message,
				})
			}
			test.Logf("the fixture expects %d, the port found %d", len(wants), len(found))
			unexpected, untaken := staticcheck_wants.Compare(wants, found)
			for _, extra := range unexpected {
				test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
					extra.Message)
			}
			for _, missing := range untaken {
				test.Errorf("the port reported nothing matching %s", missing)
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
			parity.RequireIdentical(test, st1013.Analyzer, Analyzer, folder, "./...")
		})
	}
}

// Over parity's fixture, every call the check reads, every argument written as a literal it reads and every near
// miss, under staticcheck's default list of codes left alone and, in parity/configured, under a staticcheck.conf of
// its own, the two find the same findings, each with the same fix. The port differs from the original nowhere, so
// there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1013.Analyzer, Analyzer, analysistest.TestData(), "parity/...")
}
