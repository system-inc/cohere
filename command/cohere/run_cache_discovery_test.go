package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A file created after discovery lists the tree, and before the run cache starts its clock, is never replayed
// over. The build's tsconfig enumeration reads discovery's listings (#bjv0tg4), so a run like that checks a tree
// without the file. If its record watched the directory from after the file appeared, the directory's time
// would match on the next run and the verdict would replay, clean, over a file with a type error in it. The
// record's clock starts before discovery reads anything, so that run is never recorded. Found in format's
// review of the listed format walk (#g3046x5).
func TestAFileCreatedAfterDiscoveryIsNeverReplayedOver(t *testing.T) {
	t.Parallel()
	fixture := newRunCacheFixture(t, buildCohere(t))
	fixture.establishHit()

	late := filepath.Join(fixture.root, "source", "late.ts")
	command := exec.Command(fixture.binary, verboseArguments([]string{"--no-format"})...)
	command.Dir = fixture.root
	command.Env = append([]string{"HOME=" + fixture.home, "COHERE_TEST_CREATE_AFTER_DISCOVERY=" + late}, withoutHome(os.Environ())...)
	if output, err := command.CombinedOutput(); err != nil && !strings.Contains(string(output), "late.ts") {
		// A run that saw the file fails on it, which is fine; one that failed for any other reason is not.
		if _, isExit := err.(*exec.ExitError); !isExit {
			t.Fatalf("the run that created the file failed: %v\n%s", err, output)
		}
	}
	if _, err := os.Stat(late); err != nil {
		t.Fatalf("the instrument created no file, so nothing below is about it: %v", err)
	}

	output, _ := fixture.run(true)
	if isRunCacheReplay(output) {
		t.Fatalf("the run after a file appeared beside discovery's listing replayed a verdict that never saw it:\n%s", output)
	}
	if !strings.Contains(output, "late.ts") {
		t.Errorf("the file with a type error in it was not reported:\n%s", output)
	}
}

// withoutHome is the environment without its HOME, which a fixture sets for itself.
func withoutHome(environment []string) []string {
	kept := make([]string, 0, len(environment))
	for _, variable := range environment {
		if !strings.HasPrefix(variable, "HOME=") {
			kept = append(kept, variable)
		}
	}
	return kept
}
