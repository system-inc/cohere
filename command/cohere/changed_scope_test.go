package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git in a directory and skips the test when git itself is unavailable, the same
// convention the older scope fixtures use.
func gitIn(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("git is unavailable or refused (%v): %s", err, output)
	}
}

func makeFixtureRepository(t *testing.T, directory string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, directory, "init", "--quiet")
	gitIn(t, directory, "config", "user.email", "fixture@example.com")
	gitIn(t, directory, "config", "user.name", "fixture")
	gitIn(t, directory, "config", "protocol.file.allow", "always")
}

// repositoryWithSubmodule builds a parent holding Root.ts and a submodule at `library` holding
// Inner.ts, both committed and clean.
func repositoryWithSubmodule(t *testing.T) string {
	t.Helper()
	inner := filepath.Join(t.TempDir(), "inner")
	makeFixtureRepository(t, inner)
	writeTree(t, inner, map[string]string{"Inner.ts": "export const inner = 1;\n"})
	gitIn(t, inner, "add", "Inner.ts")
	gitIn(t, inner, "commit", "--quiet", "-m", "inner")

	root := t.TempDir()
	makeFixtureRepository(t, root)
	writeTree(t, root, map[string]string{"Root.ts": "export const root = 1;\n"})
	gitIn(t, root, "add", "Root.ts")
	gitIn(t, root, "commit", "--quiet", "-m", "root")
	gitIn(t, root, "-c", "protocol.file.allow=always", "submodule", "--quiet", "add", inner, "library")
	gitIn(t, root, "commit", "--quiet", "-m", "add the submodule")
	return root
}

// TestAChangedScopeBelowTheRepositoryTopResolvesItsOwnPaths holds a project that is not the top of
// its repository.
//
// Git's porcelain paths are relative to the top level whatever directory git ran in, and they were
// joined onto the project directory, so `sub/A.ts` became `<root>/sub/sub/A.ts` and matched nothing.
// That was latent while cohere had to run from the root; once the root is found by walking up and an
// empty change set is green, a monorepo subproject with real edits would report nothing changed.
//
// The sibling edit is the other half: `-- .` keeps the answer to the directory asked about, so a
// change elsewhere in the repository is not this project's change.
func TestAChangedScopeBelowTheRepositoryTopResolvesItsOwnPaths(t *testing.T) {
	repository := t.TempDir()
	makeFixtureRepository(t, repository)
	writeTree(t, repository, map[string]string{
		"project/A.ts": "export const a = 1;\n",
		"sibling/B.ts": "export const b = 1;\n",
		"project/C.ts": "export const c = 1;\n",
	})
	gitIn(t, repository, "add", ".")
	gitIn(t, repository, "commit", "--quiet", "-m", "baseline")

	project := filepath.Join(repository, "project")
	clean, err := changedFilesScope(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(clean.FileNames) != 0 {
		t.Fatalf("a clean subproject reported changes: %v", clean.FileNames)
	}

	writeTree(t, repository, map[string]string{
		"project/A.ts": "export const a = 2;\n",
		"sibling/B.ts": "export const b = 2;\n",
	})

	scope, err := changedFilesScope(project)
	if err != nil {
		t.Fatal(err)
	}
	if !scope.includes(filepath.Join(project, "A.ts")) {
		t.Errorf("an edit inside the subproject did not resolve to its real path: %v", scope.FileNames)
	}
	if len(scope.FileNames) != 1 {
		t.Errorf("expected exactly the one edit inside the subproject, got %v", scope.FileNames)
	}
}

// TestAMovedSubmodulePointerIsAChange holds the edits a submodule's own status cannot see.
//
// Committing inside a submodule moves its HEAD along with the edit, so the submodule reports itself
// clean and the parent reports only the gitlink, which is dropped. Before this the edited file left
// the scope entirely, and with an empty change set now meaning green, `--changed` would have passed
// over it as "nothing changed".
func TestAMovedSubmodulePointerIsAChange(t *testing.T) {
	root := repositoryWithSubmodule(t)
	library := filepath.Join(root, "library")

	writeTree(t, library, map[string]string{"Inner.ts": "export const inner = 2;\n", "Added.ts": "export {};\n"})
	gitIn(t, library, "add", ".")
	gitIn(t, library, "commit", "--quiet", "-m", "moved inside the submodule")

	scope, err := changedScopeForCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Inner.ts", "Added.ts"} {
		if !scope.includes(filepath.Join(library, want)) {
			t.Errorf("%s changed against the commit the parent records and is not in scope: %v", want, scope.FileNames)
		}
	}
	if scope.includes(library) {
		t.Errorf("the submodule pointer itself entered the scope: %v", scope.FileNames)
	}

	// The control: once the parent records the new commit, nothing differs from HEAD.
	gitIn(t, root, "add", "library")
	gitIn(t, root, "commit", "--quiet", "-m", "record the new pointer")
	recorded, err := changedScopeForCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.FileNames) != 0 {
		t.Errorf("a recorded pointer still reported changes: %v", recorded.FileNames)
	}
}

// TestAnUnreadableSubmoduleIsNamedAndFailsTheCheck holds both callers' answers to a submodule git
// cannot open.
//
// The format phase gets the parent's answer plus the name of what it could not read. `--changed` gets
// an error, because its scope is its verdict and a hole in it reads exactly like a submodule with
// nothing changed. The earlier code asked the parent about submodules too, which failed the whole
// call for one broken directory, and skipped an unreadable one in silence when the parent did not.
func TestAnUnreadableSubmoduleIsNamedAndFailsTheCheck(t *testing.T) {
	root := repositoryWithSubmodule(t)

	// A gitdir pointing nowhere is the shape a moved or deleted .git/modules entry leaves behind.
	if err := os.WriteFile(filepath.Join(root, "library", ".git"), []byte("gitdir: /nonexistent/cohere-fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"Root.ts": "export const root = 2;\n"})

	scope, err := changedFilesScope(root)
	if err != nil {
		t.Fatalf("one unreadable submodule failed the parent's whole answer: %v", err)
	}
	if !scope.includes(filepath.Join(root, "Root.ts")) {
		t.Errorf("the parent's own change was lost: %v", scope.FileNames)
	}
	if len(scope.UnreadableSubmodules) != 1 || !strings.HasPrefix(scope.UnreadableSubmodules[0], "library") {
		t.Errorf("the unreadable submodule was not named: %v", scope.UnreadableSubmodules)
	}

	_, err = changedScopeForCheck(root)
	if err == nil {
		t.Fatal("--changed accepted a change set with an unreadable submodule in it")
	}
	if !strings.Contains(err.Error(), "library") {
		t.Errorf("the error does not name the submodule it could not read: %v", err)
	}
}
