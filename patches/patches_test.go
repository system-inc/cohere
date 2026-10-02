package patches

import (
	"context"
	"io/fs"
	"testing"
)

// The guard that makes a build which skipped the patches unable to pass. Each probe reports a
// different count on the stock checker, so this fails on an unpatched submodule and passes on a
// patched one; both were observed before this was trusted.
func TestEveryPatchIsPresentInTheLinkedChecker(t *testing.T) {
	for _, result := range Verify(context.Background(), t.TempDir()) {
		if result.Err != nil {
			t.Fatalf("%s: the probe could not run: %v", result.Patch.File, result.Err)
		}
		if !result.Present() {
			t.Fatalf("%s is not in the compiler this test linked: its probe reported %d diagnostics, "+
				"expected %d. Run `go run ./command/cohere-patches` and test again.",
				result.Patch.File, result.Diagnostics, result.Patch.Expected)
		}
	}
}

// A patch file nobody listed would be applied and never verified, which is the silent kind of
// present this package exists to rule out. A listed patch whose file is gone would be verified
// against nothing.
func TestEveryPatchFileIsListedAndEveryListedPatchHasAFile(t *testing.T) {
	embedded, err := fs.Glob(files, "*.patch")
	if err != nil {
		t.Fatal(err)
	}
	listed := make(map[string]bool, len(All))
	for _, patch := range All {
		listed[patch.File] = true
		if len(patch.Probe) == 0 {
			t.Errorf("%s has no probe, so nothing proves it is present", patch.File)
		}
	}
	for _, name := range embedded {
		if !listed[name] {
			t.Errorf("%s is in patches/ but not in All, so it would be applied and never verified", name)
		}
		delete(listed, name)
	}
	for name := range listed {
		t.Errorf("%s is listed in All but no such file is embedded", name)
	}
}
