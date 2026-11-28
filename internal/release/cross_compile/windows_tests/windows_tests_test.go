package windows_tests_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/release/cross_compile"
	release "github.com/system-inc/cohere/internal/release/packaging"
)

// TestEveryPackagesTestsTypeCheckForWindows type-checks every package of the module for Windows, its tests
// included, so a test that cannot build there fails here and not on a Windows machine someone finally runs
// it on.
//
// Release targets prove only what ships: command/cohere's tests used a Unix-only stand-in binary for weeks
// without anything noticing, since only three named Windows tests run in release.yml (#tejf9bc). Windows
// alone, because it is the one shipped platform that is not Unix: a file a Unix build constraint keeps
// out of linux builds would also be kept out of darwin's, which every test run here already compiles.
func TestEveryPackagesTestsTypeCheckForWindows(test *testing.T) {
	test.Parallel()

	if testing.Short() {
		test.Skip(
			"NOT MEASURED: -short skips type-checking the module's tests for Windows, so a test that no longer builds there would pass here",
		)
	}
	var windows release.Target
	for _, target := range release.Targets {
		if target.GoOperatingSystem == "windows" {
			windows = target
			break
		}
	}
	if windows.GoOperatingSystem == "" {
		test.Fatal("no release target is Windows, so there is nothing for this test to check against")
	}
	// The module root, four directories up from internal/release/cross_compile/windows_tests.
	if problems := cross_compile.TypeCheck(
		filepath.Join("..", "..", "..", ".."),
		windows,
		true,
		"./...",
	); len(problems) > 0 {
		test.Fatalf("the module's tests do not type-check for %s:\n%s", windows, strings.Join(problems, "\n"))
	}
}
