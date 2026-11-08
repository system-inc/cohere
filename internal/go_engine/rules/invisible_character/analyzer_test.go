package invisible_character

import (
	"go/build"
	"go/version"
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/stylecheck/st1018"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreamTestdata is staticcheck's own fixtures for `ST1018`, where the vendored copy keeps them, from this package's
// directory. They are read from there, never copied into this package's testdata: the fixture is not gofmt-clean, one
// comment is a space out of gofmt's alignment, and every Go file the module owns outside third_party must be, so a
// byte-for-byte copy can live only in the test's own temporary directory.
var upstreamTestdata = filepath.Join("..", "..", "..", "..", "third_party", "honnef.co", "go", "tools", "stylecheck",
	"st1018", "testdata")

// versionFolders is staticcheck's version folders for `ST1018`, one for each Go version its testutil loads them
// under.
func versionFolders(test *testing.T) []string {
	test.Helper()
	folders, globError := filepath.Glob(filepath.Join(upstreamTestdata, "go1*"))
	if globError != nil {
		test.Fatal(globError)
	}
	if len(folders) == 0 {
		test.Fatal("staticcheck's testdata holds no version folder, so the comparison would prove nothing")
	}
	return folders
}

// loadableCopy is upstreamFolder copied byte for byte into a temporary directory of the test's, with the go.mod
// staticcheck's testutil lays over the folder, `module example.com` at the folder's version, written to disk beside it
// since the engine's load reads go.mod from disk. The folder must be a valid Go version, and one newer than the
// toolchain is skipped, as testutil skips it.
func loadableCopy(test *testing.T, upstreamFolder string) string {
	test.Helper()
	folderVersion := filepath.Base(upstreamFolder)
	if !version.IsValid(folderVersion) {
		test.Fatalf("%s is not a valid Go version", upstreamFolder)
	}
	releaseTags := build.Default.ReleaseTags
	if newest := releaseTags[len(releaseTags)-1]; version.Compare(folderVersion, newest) == 1 {
		test.Skipf("%s is newer than the toolchain's Go version (%s), as staticcheck's testutil skips it",
			folderVersion, newest)
	}
	folder := filepath.Join(test.TempDir(), folderVersion)
	if copyError := os.CopyFS(folder, os.DirFS(upstreamFolder)); copyError != nil {
		test.Fatalf("copying %s: %v", upstreamFolder, copyError)
	}
	// testutil's own overlay, `module example.com\ngo 1.0`, with the newline gofmt and editors leave at the end.
	moduleFile := "module example.com\ngo " + folderVersion[len("go"):] + "\n"
	if writeError := os.WriteFile(filepath.Join(folder, "go.mod"), []byte(moduleFile), 0o600); writeError != nil {
		test.Fatalf("writing the go.mod testutil lays over %s: %v", folder, writeError)
	}
	return folder
}

// Every `//@ diag(...)` note staticcheck's fixtures hold, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects, in each version folder loaded as staticcheck's testutil
// loads it.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, upstreamFolder := range versionFolders(test) {
		test.Run(filepath.Base(upstreamFolder), func(test *testing.T) {
			test.Parallel()
			folder := loadableCopy(test, upstreamFolder)
			wants, readError := staticcheck_wants.Read(folder)
			if readError != nil {
				test.Fatal(readError)
			}
			if len(wants) == 0 {
				test.Fatal("the fixture holds no notes, so the comparison proves nothing")
			}
			// The findings name a file by its base name, so each want is matched by its file's base name, which is
			// one file's alone while no two files of the folder share one.
			wantedFiles := map[string]string{}
			for index := range wants {
				base := filepath.Base(wants[index].File)
				if other, seen := wantedFiles[base]; seen && other != wants[index].File {
					test.Fatalf("%s and %s share a base name, so a finding in one could take a want of the other",
						other, wants[index].File)
				}
				wantedFiles[base] = wants[index].File
				wants[index].File = base
			}
			_, portFindings := parity.Findings(test, st1018.Analyzer, Analyzer, folder, "./...")
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
// the same findings, each the same in every field, their fixes among them.
func TestPortMatchesOriginalOverUpstreamFixtures(test *testing.T) {
	test.Parallel()
	for _, upstreamFolder := range versionFolders(test) {
		test.Run(filepath.Base(upstreamFolder), func(test *testing.T) {
			test.Parallel()
			parity.RequireIdentical(test, st1018.Analyzer, Analyzer, loadableCopy(test, upstreamFolder), "./...")
		})
	}
}

// Over parity's fixture, every character the check reads and every near miss, in every kind of literal it judges and
// the kind it leaves, the two find the same findings, fixes and all. The port differs from the original nowhere, so
// there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1018.Analyzer, Analyzer, analysistest.TestData(), "parity")
}
