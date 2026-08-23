package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/verify/internal/prettier"
)

// The defect these guard is the one the ruling singled out: a release that embeds a stale Prettier
// bundle formats the whole tree slightly wrong, forever, and every diff after the first is noise.
// A missing bundle is easy to notice and a stale one is not, so absence, emptiness, and staleness
// are each checked and each tested separately. A guard that only covered absence would look like it
// worked while missing the case that actually happens.

func TestFormatterSourceRefusesAForkThatIsNotThere(t *testing.T) {
	t.Setenv(FormatterForkPathVariable, filepath.Join(t.TempDir(), "no-fork-here"))

	source, err := ResolveFormatterSource()
	if err == nil {
		t.Fatalf("resolved %s with no fork present", source.Directory)
	}
	if !strings.Contains(err.Error(), FormatterForkPathVariable) {
		t.Errorf("the failure does not say how to point at a fork: %v", err)
	}
}

func TestFormatterSourceRefusesAForkThatWasNeverBuilt(t *testing.T) {
	// The common first-run case: the fork is cloned, nothing has been built. It must not resolve to
	// an empty bundle set that embeds a formatter doing nothing.
	fork := newFork(t)
	t.Setenv(FormatterForkPathVariable, fork)

	if source, err := ResolveFormatterSource(); err == nil {
		t.Fatalf("resolved %s from an unbuilt fork", source.Directory)
	}
}

func TestFormatterSourceRefusesAMissingBundle(t *testing.T) {
	// A partial build. Every named bundle is required, because a formatter missing its TypeScript
	// plugin loads fine and then cannot format the language this tool checks.
	fork := newFork(t)
	buildBundles(t, fork, time.Now())
	if err := os.Remove(filepath.Join(fork, "dist", "prettier", "plugins", "typescript.js")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(FormatterForkPathVariable, fork)

	_, err := ResolveFormatterSource()
	if err == nil {
		t.Fatalf("resolved with plugins/typescript.js missing")
	}
	if !strings.Contains(err.Error(), "typescript.js") {
		t.Errorf("the failure does not name the missing bundle: %v", err)
	}
}

func TestFormatterSourceRefusesAnEmptyBundle(t *testing.T) {
	// An interrupted build leaves zero-byte files, and a zero-byte bundle embeds cleanly, ships,
	// and produces a formatter that silently does nothing. That is the exact shape of failure this
	// whole tool exists to make impossible.
	fork := newFork(t)
	buildBundles(t, fork, time.Now())
	emptied := filepath.Join(fork, "dist", "prettier", "standalone.js")
	if err := os.WriteFile(emptied, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(FormatterForkPathVariable, fork)

	_, err := ResolveFormatterSource()
	if err == nil {
		t.Fatalf("resolved with an empty standalone.js")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("the failure does not say the bundle was empty: %v", err)
	}
}

func TestFormatterSourceRefusesBundlesOlderThanTheSource(t *testing.T) {
	// The case a commit-recording check misses entirely: an edited working tree that was never
	// rebuilt. The fork's HEAD has not moved, so a stamp comparison would see agreement, and the
	// bundles are wrong anyway.
	fork := newFork(t)
	buildBundles(t, fork, time.Now().Add(-time.Hour))
	touch(t, filepath.Join(fork, "src", "index.js"), time.Now())
	t.Setenv(FormatterForkPathVariable, fork)

	_, err := ResolveFormatterSource()
	if err == nil {
		t.Fatalf("resolved bundles that predate the source they were built from")
	}
	if !strings.Contains(err.Error(), "older") {
		t.Errorf("the failure does not say the bundles are stale: %v", err)
	}
}

func TestFormatterSourceAcceptsAFreshBuild(t *testing.T) {
	// The check has to pass when nothing is wrong. A guard that refuses everything is as useless as
	// one that refuses nothing, and it is the version that gets disabled rather than fixed.
	fork := newFork(t)
	touch(t, filepath.Join(fork, "src", "index.js"), time.Now().Add(-time.Hour))
	buildBundles(t, fork, time.Now())
	t.Setenv(FormatterForkPathVariable, fork)

	source, err := ResolveFormatterSource()
	if err != nil {
		t.Fatalf("a freshly built fork did not resolve: %v", err)
	}
	if source.Commit == "" {
		t.Errorf("resolved without recording which commit built the bundles")
	}
	if !strings.HasSuffix(source.Directory, filepath.Join("dist", "prettier")) {
		t.Errorf("resolved to %s, which is not the fork's bundle directory", source.Directory)
	}
}

func TestStalenessTestRunsInOneDirectionOnly(t *testing.T) {
	// This asserts a limitation rather than a guarantee, and it is here so the limitation is a
	// measured fact instead of an assumption nobody checked.
	//
	// Source newer than bundles proves staleness. Bundles newer than source proves nothing: any
	// operation that bumps an mtime without rebuilding makes stale bundles pass. The function is
	// named `requireFreshBundles`, which reads as though it decides both directions, and it does
	// not.
	//
	// If someone later closes this — by hashing bundle contents, or by recording what built them —
	// this test fails, and the right response is to delete it and update the comment it guards. A
	// test that pins a weakness must be easy to retire on purpose and impossible to lose by
	// accident.
	fork := newFork(t)
	buildBundles(t, fork, time.Now().Add(-2*time.Hour))
	touch(t, filepath.Join(fork, "src", "index.js"), time.Now().Add(-time.Hour))

	// The bundles are genuinely stale at this point, and the guard says so.
	t.Setenv(FormatterForkPathVariable, fork)
	if _, err := ResolveFormatterSource(); err == nil {
		t.Fatalf("the guard failed to catch bundles older than the source, which is the direction it does decide")
	}

	// Bumping their mtime without rebuilding changes nothing about the bundles and everything about
	// the verdict.
	for _, name := range FormatterBundleNames {
		touch(t, filepath.Join(fork, "dist", "prettier", filepath.FromSlash(name)), time.Now())
	}
	if _, err := ResolveFormatterSource(); err != nil {
		t.Fatalf("touched-but-unrebuilt bundles were refused, so this limitation has been closed — delete this test and update the comment on requireFreshBundles: %v", err)
	}
}

func TestStalenessSeesFilesOutsideTheObviousSourceDirectories(t *testing.T) {
	// The probe this replaces named three paths and saw 677 of the fork's 9,343 tracked files. It
	// could not see `bin/`, `_system/`, `.yarn/`, or any root build configuration, so an edit to
	// the build itself produced different bundles while the check reported fresh.
	//
	// A probe that enumerates what to look at is silent about whatever nobody thought to list, and
	// the silence looks exactly like an answer. So this edits a file in none of the three original
	// directories and asserts the guard notices.
	fork := newFork(t)
	writeFile(t, filepath.Join(fork, "bin", "prettier.cjs"), "#!/usr/bin/env node\n")
	run(t, fork, "git", "add", ".")
	run(t, fork, "git", "commit", "--quiet", "-m", "add a build entry point")

	buildBundles(t, fork, time.Now().Add(-time.Hour))
	touch(t, filepath.Join(fork, "bin", "prettier.cjs"), time.Now())
	t.Setenv(FormatterForkPathVariable, fork)

	_, err := ResolveFormatterSource()
	if err == nil {
		t.Fatalf("an edit to bin/ left the bundles looking fresh, so the probe cannot see the build's own source")
	}
	if !strings.Contains(err.Error(), "bin/prettier.cjs") {
		t.Errorf("the failure does not name the file that changed: %v", err)
	}
}

func TestStalenessIgnoresPathsThatCannotReachTheBundles(t *testing.T) {
	// The other half, and the reason this is an exclusion list rather than "watch everything".
	// `tests` is 8,274 of the fork's 9,343 tracked files; watching it would make an ordinary test
	// edit look like a stale build, and a warning that fires when nothing is wrong is one nobody
	// reads later.
	fork := newFork(t)
	writeFile(t, filepath.Join(fork, "tests", "format.js"), "// a test\n")
	run(t, fork, "git", "add", ".")
	run(t, fork, "git", "commit", "--quiet", "-m", "add a test")

	// Every watched file is aged behind the bundles, so the only thing newer is the test edit. Aging
	// them individually rather than trusting the fixture's creation time: `newFork` writes
	// `scripts/build.js` at the moment it runs, which is newer than any bundle built "an hour ago",
	// and that alone fails this test for a reason unrelated to what it asks.
	aged := time.Now().Add(-time.Hour)
	for _, watched := range []string{"src/index.js", "scripts/build.js", "package.json"} {
		touch(t, filepath.Join(fork, filepath.FromSlash(watched)), aged)
	}
	buildBundles(t, fork, time.Now().Add(-30*time.Minute))
	touch(t, filepath.Join(fork, "tests", "format.js"), time.Now())
	t.Setenv(FormatterForkPathVariable, fork)

	if _, err := ResolveFormatterSource(); err != nil {
		_, path, _ := newestSourceTime(fork)
		t.Fatalf("a test edit was read as a stale build; the guard flagged %s: %v", path, err)
	}
}

func TestGuardChecksEveryBundleTheEngineActuallyLoads(t *testing.T) {
	// The list this guard checks and the list the engine loads must be one list, not two that agree
	// today. They did not agree: this file once named three bundles chosen as "the minimal set that
	// formats TypeScript", while the engine hard-errors on any of its eight being absent, so a
	// release could pass every check and produce a binary that died the first time someone formatted
	// markdown.
	//
	// Asserting identity rather than a count, because a count passes the moment someone adds a
	// fourth name by hand and reintroduces exactly the drift this is here to prevent.
	if &FormatterBundleNames[0] != &prettier.BundleFiles[0] {
		t.Fatalf("the release guard checks its own bundle list rather than the engine's, which will drift")
	}
	if len(FormatterBundleNames) != len(prettier.BundleFiles) {
		t.Fatalf("guard covers %d bundles, engine loads %d", len(FormatterBundleNames), len(prettier.BundleFiles))
	}
}

func TestStalenessCheckRefusesToMeasureNothing(t *testing.T) {
	// The vacuous-probe guard, and the reason it is here: `git ls-files` against a path that does
	// not exist succeeds and returns empty, so a staleness check reading that as "no source
	// changed" would pass for every build while measuring nothing. That exact mistake, on that
	// exact command, reached a ruling earlier tonight.
	//
	// A repository with no tracked source must be an error rather than a silent pass.
	fork := newFork(t)
	removeTrackedSource(t, fork)

	if _, _, err := newestSourceTime(fork); err == nil {
		t.Fatalf("the staleness check accepted a fork with no tracked source, so it measured nothing")
	}
}

// newFork builds a git repository shaped like the Prettier fork: tracked source, one commit.
func newFork(t *testing.T) string {
	t.Helper()
	fork := t.TempDir()

	writeFile(t, filepath.Join(fork, "package.json"), `{"name":"prettier"}`)
	writeFile(t, filepath.Join(fork, "src", "index.js"), "export const format = () => {};\n")
	writeFile(t, filepath.Join(fork, "scripts", "build.js"), "// build\n")

	run(t, fork, "git", "init", "--quiet")
	run(t, fork, "git", "config", "user.email", "test@example.com")
	run(t, fork, "git", "config", "user.name", "test")
	run(t, fork, "git", "add", ".")
	run(t, fork, "git", "commit", "--quiet", "-m", "initial")

	return fork
}

// buildBundles writes every required bundle with the given modification time.
func buildBundles(t *testing.T, fork string, modified time.Time) {
	t.Helper()
	for _, name := range FormatterBundleNames {
		path := filepath.Join(fork, "dist", "prettier", filepath.FromSlash(name))
		writeFile(t, path, "// "+name+"\n")
		touch(t, path, modified)
	}
}

// removeTrackedSource empties the index so `git ls-files` returns nothing for a real repository.
func removeTrackedSource(t *testing.T, fork string) {
	t.Helper()
	run(t, fork, "git", "rm", "--quiet", "-r", "--cached", ".")
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, path string, modified time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, directory string, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(arguments, " "), err, output)
	}
}
