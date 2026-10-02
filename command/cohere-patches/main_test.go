package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/patches"
)

// The release build trusts `-check` to exit nonzero whenever a patch is not applied. These pin that
// against a throwaway repository holding the pinned compiler's own copy of each patched file, so
// they exercise the real patches rather than synthetic ones.

func TestCheckFailsWhenAPatchIsMissing(t *testing.T) {
	root := fixtureRoot(t)
	if code, output := runIn(t, root, "-check"); code == 0 {
		t.Fatalf("-check exited 0 on an unpatched compiler, so a release would ship it stock:\n%s", output)
	}
	if modified(t, root) {
		t.Fatal("-check wrote to the compiler")
	}
}

func TestApplyThenCheckPasses(t *testing.T) {
	root := fixtureRoot(t)
	if code, output := runIn(t, root); code != 0 {
		t.Fatalf("applying exited %d:\n%s", code, output)
	}
	if code, output := runIn(t, root, "-check"); code != 0 {
		t.Fatalf("-check exited %d after applying:\n%s", code, output)
	}
	// Applying twice is a no-op, so a build step can run it unconditionally.
	if code, output := runIn(t, root); code != 0 {
		t.Fatalf("a second apply exited %d:\n%s", code, output)
	}
}

func TestAPatchThatAppliesInNeitherDirectionIsRefusedBothWays(t *testing.T) {
	root := fixtureRoot(t)
	// Change one line of context the patch anchors on, so it cannot apply forward or in reverse.
	target := filepath.Join(root, "TypeScript", firstPatchedFile(t))
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	anchor := "In case of a reverse mapped type with an intersection constraint"
	if !bytes.Contains(original, []byte(anchor)) {
		t.Fatalf("the anchor this test corrupts is not in %s; rewrite the test against the new pin", target)
	}
	corrupted := bytes.Replace(original, []byte(anchor), []byte("In the case of a reverse mapped type"), 1)
	if err := os.WriteFile(target, corrupted, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, arguments := range [][]string{{"-check"}, {}} {
		code, output := runIn(t, root, arguments...)
		if code == 0 {
			t.Fatalf("%v exited 0 on a patch that cannot apply, so the stock compiler would build:\n%s",
				arguments, output)
		}
		if !strings.Contains(output, patches.All[0].File) {
			t.Fatalf("%v refused without naming the patch:\n%s", arguments, output)
		}
	}
	if after, _ := os.ReadFile(target); !bytes.Equal(after, corrupted) {
		t.Fatal("a refused patch still wrote to the compiler")
	}
}

func TestAChangeUpstreamMergedIsReportedForDeletion(t *testing.T) {
	root := fixtureRoot(t)
	if code, output := runIn(t, root); code != 0 {
		t.Fatalf("applying exited %d:\n%s", code, output)
	}
	git(t, filepath.Join(root, "TypeScript"), "commit", "-qam", "upstream merged it")

	code, output := runIn(t, root, "-check")
	if code != 0 {
		t.Fatalf("-check exited %d when the pinned compiler already contains the change:\n%s", code, output)
	}
	if !strings.Contains(output, "UPSTREAM") {
		t.Fatalf("a patch upstream merged was not reported for deletion:\n%s", output)
	}
}

// fixtureRoot makes a cohere-shaped root whose TypeScript repository holds the pinned compiler's
// copy of every file the patches touch, committed, with nothing applied.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	repositoryRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	submodule := filepath.Join(repositoryRoot, "TypeScript")

	root := t.TempDir()
	fixture := filepath.Join(root, "TypeScript")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, fixture, "init", "-q")
	for _, patch := range patches.All {
		contents, _ := patches.Contents(patch.File)
		for _, match := range patchedFile.FindAllSubmatch(contents, -1) {
			name := string(match[1])
			pinned, err := exec.Command("git", "-C", submodule, "show", "HEAD:"+name).Output()
			if err != nil {
				t.Fatalf("reading %s at the pinned commit: %v", name, err)
			}
			destination := filepath.Join(fixture, name)
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, pinned, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	git(t, fixture, "add", "-A")
	git(t, fixture, "commit", "-qm", "pinned")
	return root
}

func firstPatchedFile(t *testing.T) string {
	t.Helper()
	contents, _ := patches.Contents(patches.All[0].File)
	return string(patchedFile.FindSubmatch(contents)[1])
}

func runIn(t *testing.T, root string, arguments ...string) (int, string) {
	t.Helper()
	var output bytes.Buffer
	code := run(append([]string{"-root", root}, arguments...), patches.All, &output, &output)
	return code, output.String()
}

func modified(t *testing.T, root string) bool {
	t.Helper()
	status, err := exec.Command("git", "-C", filepath.Join(root, "TypeScript"), "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	return len(bytes.TrimSpace(status)) > 0
}

// git runs with the user's hooks disabled, because a fixture commit is not a commit anyone reads and
// a global commit-message hook would otherwise decide whether these tests can run.
func git(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory, "-c", "core.hooksPath=/dev/null",
		"-c", "user.name=fixture", "-c", "user.email=fixture@local"}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
