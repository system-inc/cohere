package release

import (
	"path/filepath"
	"strings"
	"testing"
)

// moduleWithPatchTool writes a throwaway module whose `command/cohere-patches` prints a line and
// exits with the given code, so the release gate can be driven through both outcomes.
//
// It stands in for the real tool deliberately. The real tool's four states are pinned by its own
// tests, each confirmed by mutation; what these tests own is the other half, that the release turns
// a nonzero exit into a refusal and a zero into a pass.
func moduleWithPatchTool(t *testing.T, line string, exitCode int) string {
	t.Helper()

	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "go.mod"), "module patchgate\n\ngo 1.21\n")
	writeFile(t, filepath.Join(directory, "command", "cohere-patches", "main.go"), `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(`+"`"+line+"`"+`)
	os.Exit(`+string(rune('0'+exitCode))+`)
}
`)
	return directory
}

// TestReleaseRefusesAnUnpatchedCompiler is the known-dirty control: a patch tool reporting a patch
// not applied stops the release, and the refusal carries the tool's own words so the person
// releasing sees which patch rather than a bare exit status.
func TestReleaseRefusesAnUnpatchedCompiler(t *testing.T) {
	t.Parallel()

	directory := moduleWithPatchTool(t, "missing    0001-example.patch", 1)

	err := requireCompilerPatches(directory)
	if err == nil {
		t.Fatal("a patch tool exiting 1 did not stop the release, so a stock compiler would ship")
	}
	if !strings.Contains(err.Error(), "0001-example.patch") {
		t.Fatalf("the refusal does not name the patch the tool reported: %v", err)
	}
}

// TestReleaseProceedsWithAPatchedCompiler is the positive half. Without it, a gate that refused
// every release would pass the control above.
func TestReleaseProceedsWithAPatchedCompiler(t *testing.T) {
	t.Parallel()

	directory := moduleWithPatchTool(t, "applied    0001-example.patch", 0)

	if err := requireCompilerPatches(directory); err != nil {
		t.Fatalf("a patch tool exiting 0 stopped the release: %v", err)
	}
}

// TestReleaseRefusesWithoutAPatchTool holds the case where the tool is absent entirely, which is
// what a module from before the patches existed looks like. A gate that treated "could not run the
// check" as "nothing to check" would pass that release with whatever compiler happened to be there.
func TestReleaseRefusesWithoutAPatchTool(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "go.mod"), "module patchgate\n\ngo 1.21\n")

	if err := requireCompilerPatches(directory); err == nil {
		t.Fatal("a module with no patch tool passed the release gate")
	}
}
