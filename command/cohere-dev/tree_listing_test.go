//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// A change on main to a package land links, with command/cohere-dev left as it was, hands a landing from a branch
// that lacks it to main's land, and names the package; a branch that holds it runs its own (#ehjasc6). Before,
// only command/cohere-dev was compared, so after #j3bfnyt changed internal/go_engine/project every older branch
// ran its own land and gated main's tree with code that refused it.
//
// The fixture is a module whose command/cohere-dev imports internal/gate, which imports internal/deeper, and
// internal/unlinked, which nothing land links imports. A change on main to deeper hands the landing on; one to
// unlinked does not. The stand-in for main's land logs that it ran and fails, so a test sees which land ran
// without one landing.
func TestALandingHandsOnWhenAPackageLandLinksDiffersFromMains(test *testing.T) {
	test.Parallel()
	for _, change := range []struct {
		name, file  string
		handsOn     bool
		branchHasIt bool
		wantDiffers string
	}{
		{name: "a package land links, two imports down", file: "internal/deeper/deeper.go", handsOn: true,
			wantDiffers: "internal/deeper"},
		{name: "the same change, which the branch already holds", file: "internal/deeper/deeper.go",
			branchHasIt: true},
		{name: "a package land never links", file: "internal/unlinked/unlinked.go"},
		{name: "go.sum, which pins what land links from outside", file: "go.sum", handsOn: true,
			wantDiffers: "go.sum"},
	} {
		test.Run(change.name, func(test *testing.T) {
			test.Parallel()
			fixture := newLandFixture(test)
			files := map[string]string{
				"go.mod": "module example.com/fixture\n\ngo 1.27\n",
				"go.sum": "",
				"command/cohere-dev/main.go": "package main\n\nimport _ \"example.com/fixture/internal/gate\"\n\n" +
					"func main() {}\n",
				// Its presence on main says main's land takes a landing handed to it.
				"command/cohere-dev/" + landMainFile: "package main\n",
				"internal/gate/gate.go":              "package gate\n\nimport _ \"example.com/fixture/internal/deeper\"\n",
				"internal/deeper/deeper.go":          "package deeper\n",
				"internal/unlinked/unlinked.go":      "package unlinked\n",
			}
			for name, contents := range files {
				writeFixtureFile(test, filepath.Join(fixture.repository, name), contents)
			}
			fixture.git(fixture.repository, "add", ".")
			fixture.git(fixture.repository, "commit", "-q", "-m", fixtureSubject("a module land links"))
			worktreeA := fixture.worktree("a")
			fixture.git(worktreeA, "merge", "-q", "--no-edit", "-m", "Merge main", "main")
			// Main moves on, in a file land links or one it doesn't.
			writeFixtureFile(test, filepath.Join(fixture.repository, change.file), files[change.file]+"// changed\n")
			fixture.git(fixture.repository, "commit", "-q", "-am", fixtureSubject("change "+change.file))
			if change.branchHasIt {
				fixture.git(worktreeA, "merge", "-q", "--no-edit", "-m", "Merge main", "main")
			}

			isMains, differs := runsMainsLand(worktreeA)
			if isMains == change.handsOn || differs != change.wantDiffers {
				test.Fatalf("runsMainsLand is %t naming %q, want %t naming %q", isMains, differs, !change.handsOn,
					change.wantDiffers)
			}

			log := filepath.Join(test.TempDir(), "main-land")
			standIn := filepath.Join(test.TempDir(), "main-land")
			script := "#!/bin/sh\necho ran >> '" + log + "'\nexit 3\n"
			if writeError := executable_testing.WriteFile(standIn, []byte(script), 0o755); writeError != nil {
				test.Fatal(writeError)
			}
			landing := exec.Command(fixture.wrapper, "land", "--message", "a landing")
			landing.Dir, landing.Env = worktreeA, append(fixture.environment, mainLandVariable+"="+standIn)
			// The stand-in fails on purpose and a fixture's own land may too: which land ran is what is read.
			output, landError := landing.CombinedOutput()
			if landError != nil {
				test.Logf("the landing ended: %v", landError)
			}
			ran := len(readMaybeMissing(test, log)) > 0
			if ran != change.handsOn {
				test.Errorf("main's land ran: %t, want %t:\n%s", ran, change.handsOn, output)
			}
			if change.handsOn && !strings.Contains(string(output), change.wantDiffers+" differs from main's") {
				test.Errorf("the hand-off does not name %s:\n%s", change.wantDiffers, output)
			}
		})
	}
}

// writeFixtureFile writes a file, making its directory.
func writeFixtureFile(test *testing.T, path string, contents string) {
	test.Helper()
	if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}
