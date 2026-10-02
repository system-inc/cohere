package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/release/packaging"
)

// The defect these tests exist for: the global cohere was built from the shared working tree, so a
// rule one member had not committed was live in every project's gate. Each test builds a miniature of
// the real layout (a superproject pinning a compiler submodule, carrying a patch for it, holding a
// rule) and asserts something about which tree the binary came from, measured by running it.

func TestAnUncommittedEditDoesNotReachTheBuiltBinaryAndCommittingItDoes(t *testing.T) {
	fixture := newCommittedFixture(t, applyingPatch)

	first, firstCommit, built, err := ResolveCommitted(fixture.paths, "./command/cohere")
	if err != nil {
		t.Fatalf("building from the first commit: %v", err)
	}
	if !built {
		t.Fatal("an empty cache reported no build, so the binary came from somewhere other than this resolve")
	}
	if firstCommit != fixture.head(t) {
		t.Fatalf("built from %s, HEAD is %s", firstCommit, fixture.head(t))
	}
	if output := run(t, first); output != "rule=one compiler=patched\n" {
		t.Fatalf("the first build printed %q; the patch or the rule did not land", output)
	}

	// Two uncommitted edits, one to a rule and one inside the compiler submodule's checkout. Neither
	// may reach the binary, and the second is the subtle one: the pin is read from the commit, not
	// from what the submodule has checked out.
	writeFile(t, filepath.Join(fixture.superproject, "rules", "rules.go"), "package rules\n\nconst Value = \"uncommitted\"\n")
	writeFile(t, filepath.Join(fixture.superproject, "TypeScript", "tsc", "checker", "checker.go"),
		"package checker\n\nfunc Value() string { return \"uncommitted\" }\n")

	again, _, built, err := ResolveCommitted(fixture.paths, "./command/cohere")
	if err != nil {
		t.Fatalf("resolving with uncommitted edits on disk: %v", err)
	}
	if built || again != first {
		t.Fatalf("uncommitted edits changed the binary (built=%v, %s then %s), which is the defect", built, first, again)
	}
	if output := run(t, again); output != "rule=one compiler=patched\n" {
		t.Fatalf("with uncommitted edits on disk the binary printed %q", output)
	}

	// Committing a rule edit is what moves the binary. The check above passed on a cache hit, which
	// says nothing about what a fresh build reads, so this build runs with uncommitted edits on disk
	// to both the rule and the compiler, each different from what is committed. It has to print the
	// committed rule and the patched compiler.
	writeFile(t, filepath.Join(fixture.superproject, "rules", "rules.go"), "package rules\n\nconst Value = \"two\"\n")
	fixture.commit(t, "rules/rules.go")
	writeFile(t, filepath.Join(fixture.superproject, "rules", "rules.go"), "package rules\n\nconst Value = \"uncommitted\"\n")

	second, secondCommit, built, err := ResolveCommitted(fixture.paths, "./command/cohere")
	if err != nil {
		t.Fatalf("building from the second commit: %v", err)
	}
	if !built || second == first {
		t.Fatalf("a new commit did not produce a new binary (built=%v, %s)", built, second)
	}
	if output := run(t, second); output != "rule=two compiler=patched\n" {
		t.Fatalf("after committing, the binary printed %q", output)
	}

	// The binary names the commit that reproduces it.
	version := run(t, second, "--version")
	if !strings.Contains(version, "commit:         "+shortCommit(secondCommit)) {
		t.Fatalf("--version does not name commit %s:\n%s", shortCommit(secondCommit), version)
	}

	// Nothing is left behind but the binaries and the shared compiler.
	if entries, _ := os.ReadDir(fixture.paths.SnapshotDirectory()); len(entries) != 0 {
		t.Fatalf("%d snapshot(s) left in %s after building", len(entries), fixture.paths.SnapshotDirectory())
	}
	if entries, _ := os.ReadDir(fixture.paths.CompilerDirectory()); len(entries) != 1 {
		t.Fatalf("two commits pinning one compiler with one patch set left %d compiler extractions, expected 1", len(entries))
	}
}

func TestAPatchThatDoesNotApplyBuildsNothing(t *testing.T) {
	fixture := newCommittedFixture(t, conflictingPatch)

	_, _, _, err := ResolveCommitted(fixture.paths, "./command/cohere")
	if err == nil {
		t.Fatal("a patch that cannot apply produced a binary, which would be the stock compiler under a binary that lists the patch")
	}
	if !strings.Contains(err.Error(), "0001-fix.patch") {
		t.Fatalf("the refusal does not name the patch: %v", err)
	}
	if entries, _ := os.ReadDir(fixture.paths.BinaryDirectory()); len(entries) != 0 {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("a refused build left %v in the cache", names)
	}
}

func TestCheckCommittedVersionReadsWhatARealBinaryPrints(t *testing.T) {
	// The text comes from packaging itself rather than being typed here, so a change to how the
	// commit line is printed breaks this test instead of every committed build.
	commit := "508863131ed19b76776da9cd08862e4ef39d00b0"
	provenance := release.Provenance{Version: "dev", SelfCommit: commit, CompilerCommit: "1f70213d4922", GoToolchain: "go1.27.0", Platform: "darwin/arm64"}
	patchLine := "    0001-fix.patch  " + PatchPresentMarker
	clean := provenance.String() + "\n  compiler patches: 1\n" + patchLine + "\n"

	if err := checkCommittedVersion(clean, commit, 1); err != nil {
		t.Fatalf("a correct version was refused: %v", err)
	}

	cases := map[string]string{
		"another commit": strings.Replace(clean, shortCommit(commit), "000000000000", 1),
		"a dirty tree": func() string {
			dirty := provenance
			dirty.SourceTreeModified = true
			return dirty.String() + "\n" + patchLine + "\n"
		}(),
		"a missing patch":  provenance.String() + "\n    0001-fix.patch  MISSING: probe reported 1 diagnostics, expected 0\n",
		"no commit at all": release.Provenance{Version: "dev"}.String() + "\n" + patchLine + "\n",
	}
	for name, version := range cases {
		if err := checkCommittedVersion(version, commit, 1); err == nil {
			t.Errorf("%s was accepted:\n%s", name, version)
		}
	}
}

func TestFrozenRunsOnlyCohereBinaries(t *testing.T) {
	// Measured on 2026-10-02: the newest file in the real cache was `cohere-swift-current`, and the
	// launcher itself sits there as `cohere-dispatch`. Both matched the old `cohere-` prefix.
	cohere := platformBinaryPrefix() + "aaaaaaaaaaaaaaaa"
	// Each of these is stamped newer than the real binary, so it would win if it were a candidate.
	impostors := []string{
		"cohere-swift-current",
		"cohere-swift-darwin-arm64-bbbbbbbbbbbbbbbb",
		"cohere-dispatch",
		platformBinaryPrefix() + "cccccccccccccccc.partial-123",
	}
	binaries := map[string]string{cohere: "binary"}
	for _, name := range impostors {
		binaries[name] = "not cohere"
	}
	paths := newCacheWithBinaries(t, binaries)
	setModificationTimes(t, paths, append([]string{cohere}, impostors...))

	frozen, err := ResolveFrozen(paths)
	if err != nil {
		t.Fatalf("resolving frozen: %v", err)
	}
	if filepath.Base(frozen.Path) != cohere {
		t.Fatalf("--frozen chose %s, which is not a cohere binary", filepath.Base(frozen.Path))
	}
}

// committedFixture is a superproject pinning a compiler submodule, laid out as the real one is.
type committedFixture struct {
	superproject string
	paths        Paths
}

// The two patches the fixtures commit: one that applies to the stock compiler and one that cannot.
const (
	applyingPatch = `diff --git a/tsc/checker/checker.go b/tsc/checker/checker.go
--- a/tsc/checker/checker.go
+++ b/tsc/checker/checker.go
@@ -1,3 +1,3 @@
 package checker

-func Value() string { return "stock" }
+func Value() string { return "patched" }
`
	conflictingPatch = `diff --git a/tsc/checker/checker.go b/tsc/checker/checker.go
--- a/tsc/checker/checker.go
+++ b/tsc/checker/checker.go
@@ -1,3 +1,3 @@
 package checker

-func Value() string { return "something the pinned compiler never contained" }
+func Value() string { return "patched" }
`
)

// newCommittedFixture builds the superproject and its compiler and commits both.
//
// It needs git and a Go toolchain, and it compiles a real binary, so it is skipped under -short.
func newCommittedFixture(t *testing.T, patch string) committedFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a binary from a git snapshot")
	}

	root := t.TempDir()
	compilerSource := filepath.Join(root, "compiler")
	superproject := filepath.Join(root, "cohere")

	writeFile(t, filepath.Join(compilerSource, "tsc", "go.mod"), "module example.com/tsc\n\ngo 1.27\n")
	writeFile(t, filepath.Join(compilerSource, "tsc", "checker", "checker.go"), "package checker\n\nfunc Value() string { return \"stock\" }\n")
	gitCommand(t, compilerSource, "init", "-q")
	gitCommand(t, compilerSource, "add", "tsc")
	gitCommand(t, compilerSource, "commit", "-q", "-m", "stock compiler")

	writeFile(t, filepath.Join(superproject, "go.mod"), "module github.com/system-inc/cohere\n\ngo 1.27\n")
	writeFile(t, filepath.Join(superproject, "go.work"), "go 1.27\n\nuse (\n\t.\n\t./TypeScript/tsc\n)\n")
	writeFile(t, filepath.Join(superproject, "rules", "rules.go"), "package rules\n\nconst Value = \"one\"\n")
	writeFile(t, filepath.Join(superproject, "internal", "release", "packaging", "stamp.go"),
		"package packaging\n\nvar (\n\tselfCommit     string\n\tcompilerCommit string\n)\n\nfunc SelfCommit() string { return selfCommit }\n")
	// --version measures the patch the way the real probe does: by asking the compiler this binary
	// links, so a build that skipped the patch reports it missing rather than reciting it.
	writeFile(t, filepath.Join(superproject, "command", "cohere", "main.go"), fmt.Sprintf(`package main

import (
	"fmt"
	"os"

	"example.com/tsc/checker"
	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/rules"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("cohere dev")
		if commit := packaging.SelfCommit(); len(commit) >= 12 {
			fmt.Println("  commit:         " + commit[:12])
		}
		state := "MISSING"
		if checker.Value() == "patched" {
			state = %q
		}
		fmt.Println("    0001-fix.patch  " + state)
		return
	}
	fmt.Printf("rule=%%s compiler=%%s\n", rules.Value, checker.Value())
}
`, PatchPresentMarker))
	writeFile(t, filepath.Join(superproject, "patches", "0001-fix.patch"), patch)

	gitCommand(t, superproject, "init", "-q")
	gitCommand(t, root, "clone", "-q", compilerSource, filepath.Join(superproject, "TypeScript"))
	pin := strings.TrimSpace(gitCommand(t, compilerSource, "rev-parse", "HEAD"))
	gitCommand(t, superproject, "update-index", "--add", "--cacheinfo", "160000,"+pin+",TypeScript")
	gitCommand(t, superproject, "add", "go.mod", "go.work", "rules", "internal", "command", "patches")
	gitCommand(t, superproject, "commit", "-q", "-m", "first")

	cache := filepath.Join(root, "cache")
	// The Go build cache is shared with the toolchain's own, so the fixture does not compile the
	// standard library cold for every test. It is content-addressed, so sharing it changes nothing.
	goCache := strings.TrimSpace(goEnvironmentValue(t, "GOCACHE"))
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(goCache, filepath.Join(cache, "gocache")); err != nil {
		t.Fatal(err)
	}

	return committedFixture{
		superproject: superproject,
		paths:        Paths{ModuleDirectory: superproject, CacheDirectory: cache},
	}
}

func (fixture committedFixture) head(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(gitCommand(t, fixture.superproject, "rev-parse", "HEAD"))
}

func (fixture committedFixture) commit(t *testing.T, paths ...string) {
	t.Helper()
	gitCommand(t, fixture.superproject, append([]string{"add"}, paths...)...)
	gitCommand(t, fixture.superproject, "commit", "-q", "-m", "next")
}

// gitCommand runs git isolated from the machine's configuration, so a signing key or a hook in
// someone's global config cannot change what the fixture commits.
func gitCommand(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.com",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, arguments...)...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(arguments, " "), directory, err, output)
	}
	return string(output)
}

func goEnvironmentValue(t *testing.T, name string) string {
	t.Helper()
	output, err := exec.Command("go", "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}
	return string(output)
}

func run(t *testing.T, binary string, arguments ...string) string {
	t.Helper()
	output, err := exec.Command(binary, arguments...).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s: %v\n%s", binary, err, output)
	}
	return string(output)
}
