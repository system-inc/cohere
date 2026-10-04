package dispatch

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// To a pipe or a file, a step and a note print nothing; a failure always prints (#ytqqv8v).
func TestProgressOffATerminalSaysOnlyFailures(t *testing.T) {
	var out bytes.Buffer
	progress := NewProgress(&out, false, false)
	progress.Step("cohere: building from commit %s", "abc")
	progress.Note("cohere: pruned %d", 3)
	progress.Clear()
	if out.Len() != 0 {
		t.Fatalf("a step and a note printed off a terminal: %q", out.String())
	}
	progress.Fail("cohere: the cache was not pruned: %s", "denied")
	if out.String() != "cohere: the cache was not pruned: denied\n" {
		t.Fatalf("a failure printed %q", out.String())
	}
}

// On a terminal a step is one dim line that the next step rewrites and Clear removes, so the report
// that follows starts on a clean row; a failure clears it first; a long step is kept to one row.
func TestProgressOnATerminalIsOneLineThatClears(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	var out bytes.Buffer
	progress := NewProgress(&out, false, true)
	progress.Step("cohere: building from commit %s", "abc")
	progress.Step("cohere: extracting the compiler")
	progress.Note("cohere: pruned 3")
	progress.Clear()
	want := "\r\x1b[2K\x1b[2mcohere: building from commit abc\x1b[22m" +
		"\r\x1b[2K\x1b[2mcohere: extracting the compiler\x1b[22m" +
		"\r\x1b[2K"
	if out.String() != want {
		t.Fatalf("on a terminal:\n got %q\nwant %q", out.String(), want)
	}

	out.Reset()
	progress.Step("%s", strings.Repeat("x", 200))
	progress.Fail("cohere: failed")
	got := out.String()
	if !strings.Contains(got, strings.Repeat("x", stepWidth-1)+"…") || strings.Contains(got, strings.Repeat("x", stepWidth)) {
		t.Errorf("a long step was not kept to one row: %q", got)
	}
	if !strings.HasSuffix(got, "\r\x1b[2Kcohere: failed\n") {
		t.Errorf("a failure did not clear the step first: %q", got)
	}

	out.Reset()
	t.Setenv("NO_COLOR", "1")
	NewProgress(&out, false, true).Step("cohere: building")
	if strings.Contains(out.String(), "\x1b[2m") {
		t.Errorf("NO_COLOR still dimmed the step: %q", out.String())
	}
}

// With --verbose every step and note prints in full, a line each, as it always did.
func TestProgressVerboseSaysEverything(t *testing.T) {
	var out bytes.Buffer
	progress := NewProgress(&out, true, true)
	progress.Step("cohere: building from commit %s", "abc")
	progress.Note("cohere: pruned %d", 3)
	progress.Clear()
	if out.String() != "cohere: building from commit abc\ncohere: pruned 3\n" {
		t.Fatalf("verbose printed %q", out.String())
	}
}

// The golden case, through the real launcher on a real checkout: a run that rebuilds, and a run after it
// that does not, each print exactly what cohere prints and nothing of the launcher's, when stderr is not
// a terminal. --verbose still names the build. The fixture's cohere prints one line, standing in for the
// footer.
func TestALaunchedRunPrintsOnlyCoheresOwnOutputOffATerminal(t *testing.T) {
	fixture := newCommittedFixture(t)
	launcher := filepath.Join(t.TempDir(), "cohere-dispatch")
	if output, err := exec.Command("go", "build", "-o", launcher, "../../../command/cohere-dispatch").CombinedOutput(); err != nil {
		t.Fatalf("building the launcher: %v\n%s", err, output)
	}
	// The launcher keeps its cache in the checkout. Its Go cache is the toolchain's own, so the fixture
	// does not compile the standard library cold, as newCommittedFixture does for its own paths.
	cache := filepath.Join(fixture.superproject, ".cache", "cohere")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(strings.TrimSpace(goEnvironmentValue(t, "GOCACHE")), filepath.Join(cache, "gocache")); err != nil {
		t.Fatal(err)
	}

	launch := func(arguments ...string) string {
		t.Helper()
		command := exec.Command(launcher, arguments...)
		command.Dir = fixture.superproject
		// The default cache the launcher bounds in the background is one of the test's own.
		command.Env = append(os.Environ(), "COHERE_WAIT=1", "GOCACHE="+t.TempDir())
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("the launcher failed: %v\n%s", err, output)
		}
		return string(output)
	}

	for _, run := range []string{"a run that rebuilds", "a run that does not"} {
		if output := launch(); output != "rule=one compiler=pinned\n" {
			t.Fatalf("%s printed more than cohere's own output:\n%q", run, output)
		}
	}

	writeFile(t, filepath.Join(fixture.superproject, "rules", "rules.go"), "package rules\n\nconst Value = \"two\"\n")
	fixture.commit(t, "rules/rules.go")
	output := launch("--verbose")
	if !strings.Contains(output, "cohere: building from commit") || !strings.HasSuffix(output, "rule=two compiler=pinned\n") {
		t.Fatalf("a --verbose rebuild does not name the build, or lost cohere's output:\n%s", output)
	}
}

// What a prune removed is in the prune log and, under --verbose, on screen; a default run says nothing of
// it. The cache here has binaries the prune removes, so the silence is not a prune that found nothing.
func TestAPruneIsSilentUnlessVerbose(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		paths, _ := seedPruneCache(t)
		writeFile(t, filepath.Join(paths.GoCacheDirectory(), "README"),
			"This directory holds cached build artifacts from the Go build system.\n")
		var out bytes.Buffer
		previous := Report
		Report = NewProgress(&out, verbose, false)
		pruneAfterBuild(paths, filepath.Join(paths.BinaryDirectory(), "cohere-"+platformTag()+"-keepme0000000000"), "")
		Report = previous

		log, err := os.ReadFile(paths.PruneLogPath())
		if err != nil || !strings.Contains(string(log), "removed") {
			t.Fatalf("the prune removed nothing, so its silence proves nothing: %q, %v", log, err)
		}
		switch {
		case !verbose && out.Len() != 0:
			t.Errorf("a default run printed the prune: %q", out.String())
		case verbose && !strings.Contains(out.String(), "cohere: pruned "):
			t.Errorf("a --verbose run did not print the prune: %q", out.String())
		}
	}
}
