package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
