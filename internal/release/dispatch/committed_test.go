package dispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/release/packaging"
)

// The defect these tests exist for: the global cohere was built from the shared working tree, so a
// rule one member had not committed was live in every project's gate. Each test builds a miniature of
// the real layout (a superproject pinning a compiler submodule and holding a rule) and asserts
// something about which tree the binary came from, measured by running it.

func TestAnUncommittedEditDoesNotReachTheBuiltBinaryAndCommittingItDoes(test *testing.T) {
	test.Parallel()

	fixture := newCommittedFixture(test)

	first, firstCommit, built, resolveCommittedError := ResolveCommitted(fixture.paths, "./command/cohere")
	if resolveCommittedError != nil {
		test.Fatalf("building from the first commit: %v", resolveCommittedError)
	}
	if !built {
		test.Fatal("an empty cache reported no build, so the binary came from somewhere other than this resolve")
	}
	if firstCommit != fixture.head(test) {
		test.Fatalf("built from %s, HEAD is %s", firstCommit, fixture.head(test))
	}
	if output := run(test, first); output != "rule=one compiler=pinned\n" {
		test.Fatalf("the first build printed %q; the rule or the pinned compiler did not land", output)
	}

	// Two uncommitted edits, one to a rule and one inside the compiler submodule's checkout. Neither
	// may reach the binary, and the second is the subtle one: the pin is read from the commit, not
	// from what the submodule has checked out.
	writeFile(
		test,
		filepath.Join(fixture.superproject, "rules", "rules.go"),
		"package rules\n\nconst Value = \"uncommitted\"\n",
	)
	writeFile(test, filepath.Join(fixture.superproject, "TypeScript", "tsc", "checker", "checker.go"),
		"package checker\n\nfunc Value() string { return \"uncommitted\" }\n")

	again, _, built, resolveCommittedError := ResolveCommitted(fixture.paths, "./command/cohere")
	if resolveCommittedError != nil {
		test.Fatalf("resolving with uncommitted edits on disk: %v", resolveCommittedError)
	}
	if built || again != first {
		test.Fatalf(
			"uncommitted edits changed the binary (built=%v, %s then %s), which is the defect",
			built,
			first,
			again,
		)
	}
	if output := run(test, again); output != "rule=one compiler=pinned\n" {
		test.Fatalf("with uncommitted edits on disk the binary printed %q", output)
	}

	// Committing a rule edit is what moves the binary. The check above passed on a cache hit, which
	// says nothing about what a fresh build reads, so this build runs with uncommitted edits on disk
	// to both the rule and the compiler, each different from what is committed. It has to print the
	// committed rule and the pinned compiler.
	writeFile(
		test,
		filepath.Join(fixture.superproject, "rules", "rules.go"),
		"package rules\n\nconst Value = \"two\"\n",
	)
	fixture.commit(test, "rules/rules.go")
	writeFile(
		test,
		filepath.Join(fixture.superproject, "rules", "rules.go"),
		"package rules\n\nconst Value = \"uncommitted\"\n",
	)

	second, secondCommit, built, resolveCommittedError := ResolveCommitted(fixture.paths, "./command/cohere")
	if resolveCommittedError != nil {
		test.Fatalf("building from the second commit: %v", resolveCommittedError)
	}
	if !built || second == first {
		test.Fatalf("a new commit did not produce a new binary (built=%v, %s)", built, second)
	}
	if output := run(test, second); output != "rule=two compiler=pinned\n" {
		test.Fatalf("after committing, the binary printed %q", output)
	}

	// The binary names the commit that reproduces it.
	version := run(test, second, "--version")
	if !strings.Contains(version, "commit:         "+release.ShortCommit(secondCommit)) {
		test.Fatalf("--version does not name commit %s:\n%s", release.ShortCommit(secondCommit), version)
	}

	// Nothing is left behind but the binaries and the shared compiler.
	snapshots, readSnapshotsError := os.ReadDir(fixture.paths.SnapshotDirectory())
	if readSnapshotsError != nil {
		test.Fatalf("reading the snapshot directory: %v", readSnapshotsError)
	}
	if len(snapshots) != 0 {
		test.Fatalf("%d snapshot(s) left in %s after building", len(snapshots), fixture.paths.SnapshotDirectory())
	}
	compilers, readCompilersError := os.ReadDir(fixture.paths.CompilerDirectory())
	if readCompilersError != nil {
		test.Fatalf("reading the compiler directory: %v", readCompilersError)
	}
	if len(compilers) != 1 {
		test.Fatalf("two commits pinning one compiler left %d compiler extractions, expected 1", len(compilers))
	}
}

func TestCheckCommittedVersionReadsWhatARealBinaryPrints(test *testing.T) {
	test.Parallel()

	// The text comes from packaging itself rather than being typed here, so a change to how the
	// commit line is printed breaks this test instead of every committed build.
	commit := "508863131ed19b76776da9cd08862e4ef39d00b0"
	provenance := release.Provenance{
		Version:        "dev",
		SelfCommit:     commit,
		CompilerCommit: "1f70213d4922",
		GoToolchain:    "go1.27.0",
		Platform:       "darwin/arm64",
	}
	clean := provenance.String() + "\n"

	if checkCommittedVersionError := checkCommittedVersion(clean, commit); checkCommittedVersionError != nil {
		test.Fatalf("a correct version was refused: %v", checkCommittedVersionError)
	}

	cases := map[string]string{
		"another commit": strings.Replace(clean, release.ShortCommit(commit), "000000000000", 1),
		"a dirty tree": func() string {
			dirty := provenance
			dirty.SourceTreeModified = true
			return dirty.String() + "\n"
		}(),
		"no commit at all": release.Provenance{Version: "dev"}.String() + "\n",
	}
	for name, version := range cases {
		if checkCommittedVersionError := checkCommittedVersion(version, commit); checkCommittedVersionError == nil {
			test.Errorf("%s was accepted:\n%s", name, version)
		}
	}
}

func TestFrozenRunsOnlyCohereBinaries(test *testing.T) {
	test.Parallel()

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
	paths := newCacheWithBinaries(test, binaries)
	setModificationTimes(test, paths, append([]string{cohere}, impostors...))

	frozen, resolveFrozenError := ResolveFrozen(paths)
	if resolveFrozenError != nil {
		test.Fatalf("resolving frozen: %v", resolveFrozenError)
	}
	if filepath.Base(frozen.Path) != cohere {
		test.Fatalf("--frozen chose %s, which is not a cohere binary", filepath.Base(frozen.Path))
	}
}

// committedFixture is a superproject pinning a compiler submodule, laid out as the real one is.
type committedFixture struct {
	superproject string
	paths        Paths
}

// newCommittedFixture builds the superproject and its compiler and commits both.
//
// It needs git and a Go toolchain, and it compiles a real binary, so it is skipped under -short.
func newCommittedFixture(test *testing.T) committedFixture {
	test.Helper()
	if testing.Short() {
		test.Skip("builds a binary from a git snapshot")
	}

	root := test.TempDir()
	compilerSource := filepath.Join(root, "compiler")
	superproject := filepath.Join(root, "cohere")

	writeFile(test, filepath.Join(compilerSource, "tsc", "go.mod"), "module example.com/tsc\n\ngo 1.27\n")
	writeFile(
		test,
		filepath.Join(compilerSource, "tsc", "checker", "checker.go"),
		"package checker\n\nfunc Value() string { return \"pinned\" }\n",
	)
	gitCommand(test, compilerSource, "init", "-q")
	gitCommand(test, compilerSource, "add", "tsc")
	gitCommand(test, compilerSource, "commit", "-q", "-m", "pinned compiler")

	writeFile(test, filepath.Join(superproject, "go.mod"), "module github.com/system-inc/cohere\n\ngo 1.27\n")
	writeFile(test, filepath.Join(superproject, "go.work"), "go 1.27\n\nuse (\n\t.\n\t./TypeScript/tsc\n)\n")
	writeFile(test, filepath.Join(superproject, "rules", "rules.go"), "package rules\n\nconst Value = \"one\"\n")
	writeFile(test, filepath.Join(superproject, "internal", "release", "packaging", "stamp.go"),
		"package packaging\n\nvar (\n\tselfCommit         string\n\tcompilerCommit     string\n"+
			"\tsourceTreeModified string\n)\n\n"+
			"func SelfCommit() string { return selfCommit }\n\nfunc SourceTreeModified() bool { return sourceTreeModified == \"true\" }\n")
	writeFile(test, filepath.Join(superproject, "command", "cohere", "main.go"), `package main

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
		if packaging.SourceTreeModified() {
			fmt.Println("  source:         built from a tree with uncommitted changes")
		}
		return
	}
	fmt.Printf("rule=%s compiler=%s\n", rules.Value, checker.Value())
}
`)

	gitCommand(test, superproject, "init", "-q")
	gitCommand(test, root, "clone", "-q", compilerSource, filepath.Join(superproject, "TypeScript"))
	pin := strings.TrimSpace(gitCommand(test, compilerSource, "rev-parse", "HEAD"))
	gitCommand(test, superproject, "update-index", "--add", "--cacheinfo", "160000,"+pin+",TypeScript")
	gitCommand(test, superproject, "add", "go.mod", "go.work", "rules", "internal", "command")
	gitCommand(test, superproject, "commit", "-q", "-m", "first")

	// The builds use the toolchain's own Go cache, as the launcher's do, so the fixture does not compile
	// the standard library cold for every test.
	cache := filepath.Join(root, "cache")
	if createDirectoryError := os.MkdirAll(cache, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}

	return committedFixture{
		superproject: superproject,
		paths:        Paths{ModuleDirectory: superproject, CacheDirectory: cache},
	}
}

func (fixture committedFixture) head(test *testing.T) string {
	test.Helper()
	return strings.TrimSpace(gitCommand(test, fixture.superproject, "rev-parse", "HEAD"))
}

func (fixture committedFixture) commit(test *testing.T, paths ...string) {
	test.Helper()
	gitCommand(test, fixture.superproject, append([]string{"add"}, paths...)...)
	gitCommand(test, fixture.superproject, "commit", "-q", "-m", "next")
}

// gitCommand runs git isolated from the machine's configuration, so a signing key or a hook in
// someone's global config cannot change what the fixture commits.
func gitCommand(test *testing.T, directory string, arguments ...string) string {
	test.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.com",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, arguments...)...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	output, outputError := command.CombinedOutput()
	if outputError != nil {
		test.Fatalf("git %s in %s: %v\n%s", strings.Join(arguments, " "), directory, outputError, output)
	}
	return string(output)
}

func run(test *testing.T, binary string, arguments ...string) string {
	test.Helper()
	output, outputError := exec.Command(binary, arguments...).CombinedOutput()
	if outputError != nil {
		test.Fatalf("running %s: %v\n%s", binary, outputError, output)
	}
	return string(output)
}

// A launcher build compiles through the Go cache the caller's own `go` commands use, the one cache on the
// machine, and keeps none of its own (#3kr3x59). The caller's cache here is the test's, empty before the
// build, so its filling is the build's.
//
// Not parallel: it points `GOCACHE` at its own directory with test.Setenv, which a parallel test may not call.
func TestALaunchedBuildUsesTheCallersGoCache(test *testing.T) {
	fixture := newCommittedFixture(test)
	shared := test.TempDir()
	test.Setenv("GOCACHE", shared)
	if _, _, built, resolveCommittedError := ResolveCommitted(
		fixture.paths,
		"./command/cohere",
	); resolveCommittedError != nil || !built {
		test.Fatalf("building: built=%v, %v", built, resolveCommittedError)
	}
	entries, readDirectoryError := os.ReadDir(shared)
	if readDirectoryError != nil || len(entries) < 2 {
		test.Fatalf(
			"the caller's Go cache holds %d entries after a build, so the build compiled somewhere else: %v",
			len(entries),
			readDirectoryError,
		)
	}
	if _, statError := os.Stat(filepath.Join(fixture.paths.CacheDirectory, "gocache")); !os.IsNotExist(statError) {
		test.Fatalf("the launcher made a Go cache of its own: %v", statError)
	}
}
