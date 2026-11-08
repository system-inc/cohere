package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/streams"
)

// Every line of a text tagLines tags starts with the tag.
//
// The lines it carries are the ones a replay must not print, so a line that escaped untagged would be
// replayed as current: "graph built in 436ms" on a run that built nothing. A text is tagged whole, before
// any of it is written, so how a stream later splits its writes cannot leave a line untagged.
func TestTagLinesTagsEveryLine(test *testing.T) {
	test.Parallel()
	want := string(invocationTag) + "graph built in 436ms\n" + string(invocationTag) + "types ran in 409ms\n"
	if tagged := tagLines("graph built in 436ms\ntypes ran in 409ms\n", invocationTag); tagged != want {
		test.Fatalf("tagged output:\n  got  %q\n  want %q", tagged, want)
	}
	if tagged := tagLines("", invocationTag); tagged != "" {
		test.Errorf("an empty text came back as %q", tagged)
	}
}

// While a recording is in progress, printInvocation and printProvenance tag every line they print; without
// one, they print the text exactly, so a run that is not recorded prints what it printed before the run cache
// existed.
// Not parallel: it sets the package-level activeRunCache for its length
func TestTheTagPrintersTagOnlyARecordedRun(test *testing.T) {
	previous := activeRunCache
	defer func() { activeRunCache = previous }()
	printed := func() string {
		var out bytes.Buffer
		stream := bufio.NewWriter(&out)
		printInvocation(stream, "graph built in %s\n%s in 409ms\n", "436ms", "types ran")
		printProvenance(stream, "%v\n", "fix: 0 of 5 files rewritten, 5 not formatted")
		flushStream(test, stream)
		return out.String()
	}

	activeRunCache = nil
	want := "graph built in 436ms\ntypes ran in 409ms\nfix: 0 of 5 files rewritten, 5 not formatted\n"
	if got := printed(); got != want {
		test.Errorf("with no recording in progress:\n  got  %q\n  want %q", got, want)
	}
	activeRunCache = &runCacheSession{}
	want = string(invocationTag) + "graph built in 436ms\n" + string(invocationTag) + "types ran in 409ms\n" +
		string(provenanceTag) + "fix: 0 of 5 files rewritten, 5 not formatted\n"
	if got := printed(); got != want {
		test.Errorf("with a recording in progress:\n  got  %q\n  want %q", got, want)
	}
}

// The tee strips the tag on the way to the terminal and keeps tagged lines out of the replay.
//
// Both halves matter and fail differently. A tag reaching the terminal prints a control byte at the
// front of a line; a tagged line reaching the buffer is replayed as a fact about this run. This drives
// the real tee through a real pipe, since the pipe is the reason the tag travels in the stream.
func TestTeeStripsTagsFromTheTerminalAndTheReplay(test *testing.T) {
	test.Parallel()
	terminal, createError := os.Create(filepath.Join(test.TempDir(), "terminal"))
	if createError != nil {
		test.Fatal(createError)
	}
	// Ignored: written unbuffered by the tee and read back by name before this close, so it loses nothing.
	defer terminal.Close()

	target := terminal
	buffered := streams.Over(&target)
	var tee teeStream
	if startError := tee.start(&target, buffered); startError != nil {
		test.Fatal(startError)
	}
	fmt.Fprint(buffered, "fix: 0 of 5 files rewritten\n")
	fmt.Fprint(buffered, tagLines("graph built in 436ms\n", invocationTag))
	fmt.Fprint(buffered, "coverage: 460 rules\n")
	fmt.Fprint(buffered, tagLines("phases: fix ran in 2.9s\n  total 4.0s\n", invocationTag))
	fmt.Fprint(buffered, "  this binary was built from a modified tree\n")
	tee.stop(&target, buffered)

	if target != terminal {
		test.Fatal("stop did not restore the real stream")
	}
	shown, readError := os.ReadFile(terminal.Name())
	if readError != nil {
		test.Fatal(readError)
	}

	wantShown := "fix: 0 of 5 files rewritten\ngraph built in 436ms\ncoverage: 460 rules\nphases: fix ran in 2.9s\n  total 4.0s\n  this binary was built from a modified tree\n"
	if string(shown) != wantShown {
		test.Errorf("the terminal did not get every line untagged:\n  got  %q\n  want %q", shown, wantShown)
	}
	wantReplayed := "fix: 0 of 5 files rewritten\ncoverage: 460 rules\n  this binary was built from a modified tree\n"
	if tee.buffer.String() != wantReplayed {
		test.Errorf("the replay holds the wrong lines:\n  got  %q\n  want %q", tee.buffer.String(), wantReplayed)
	}
	if strings.Contains(tee.buffer.String(), string(invocationTag)) || bytes.Contains(shown, invocationTag) {
		test.Error("the tag itself escaped into the terminal or the replay")
	}
}

// TestTheTeeRecordsWhatTheBufferedStreamHeld: the stream a person reads is buffered over the file the tee swaps
// (see package streams), so the tee shows it at each swap. A line buffered before the recording starts reaches
// the terminal and stays out of the replay, and a line still buffered when it stops is recorded, not lost and
// not written past the recording to the terminal alone.
func TestTheTeeRecordsWhatTheBufferedStreamHeld(test *testing.T) {
	test.Parallel()
	terminal, createError := os.Create(filepath.Join(test.TempDir(), "terminal"))
	if createError != nil {
		test.Fatal(createError)
	}
	// Ignored: written unbuffered by the tee and read back by name before this close, so it loses nothing.
	defer terminal.Close()

	target := terminal
	buffered := streams.Over(&target)
	fmt.Fprint(buffered, "before the recording\n")
	var tee teeStream
	if startError := tee.start(&target, buffered); startError != nil {
		test.Fatal(startError)
	}
	fmt.Fprint(buffered, "coverage: 460 rules\n")
	tee.stop(&target, buffered)
	fmt.Fprint(buffered, "after the recording\n")
	streams.Show(buffered)

	shown, readError := os.ReadFile(terminal.Name())
	if readError != nil {
		test.Fatal(readError)
	}
	if expected := "before the recording\ncoverage: 460 rules\nafter the recording\n"; string(shown) != expected {
		test.Errorf("the terminal:\n  got      %q\n  expected %q", shown, expected)
	}
	if expected := "coverage: 460 rules\n"; tee.buffer.String() != expected {
		test.Errorf("the recording:\n  got      %q\n  expected %q", tee.buffer.String(), expected)
	}
}

// A provenance-tagged line reaches the terminal untagged and stays tagged in the recording, so a replay
// can say where it came from; and replayLines turns it into "label (from the cached run at T): rest"
// with no tag left anywhere.
func TestProvenanceLinesAreKeptTaggedAndReplayedWithTheirSource(test *testing.T) {
	test.Parallel()
	terminal, createError := os.Create(filepath.Join(test.TempDir(), "terminal"))
	if createError != nil {
		test.Fatal(createError)
	}
	// Ignored: written unbuffered by the tee and read back by name before this close, so it loses nothing.
	defer terminal.Close()

	target := terminal
	buffered := streams.Over(&target)
	var tee teeStream
	if startError := tee.start(&target, buffered); startError != nil {
		test.Fatal(startError)
	}
	fmt.Fprint(buffered, tagLines("fix: 0 of 5 files rewritten, 5 not formatted\n", provenanceTag))
	fmt.Fprint(buffered, "coverage: 460 rules\n")
	tee.stop(&target, buffered)

	shown, readError := os.ReadFile(terminal.Name())
	if readError != nil {
		test.Fatal(readError)
	}
	if string(shown) != "fix: 0 of 5 files rewritten, 5 not formatted\ncoverage: 460 rules\n" {
		test.Errorf("the terminal saw the tag or lost the line: %q", shown)
	}
	if !bytes.HasPrefix(tee.buffer.Bytes(), provenanceTag) {
		test.Fatalf(
			"the recording lost the provenance tag, so a replay would print the line as current: %q",
			tee.buffer.String(),
		)
	}

	replayed := string(replayLines(tee.buffer.Bytes(), "03:41:07"))
	want := "fix (from the cached run at 03:41:07): 0 of 5 files rewritten, 5 not formatted\ncoverage: 460 rules\n"
	if replayed != want {
		test.Errorf("the replay:\n  got  %q\n  want %q", replayed, want)
	}
	if strings.Contains(replayed, string(provenanceTag)) || strings.Contains(replayed, string(invocationTag)) {
		test.Error("a tag survived into the replay")
	}
}

// The run cache serves a bare run and `--no-fix`, with the flags runCacheAdmits names beside them. Any other
// argument widens what the output could depend on, and one admitted by accident would be cached on a key that
// covers it but by a cache never shown to be correct for it.
// Not parallel: it sets os.Args and the package-level cacheOff, which runCacheEligible reads
func TestRunCacheEligibility(test *testing.T) {
	previous := os.Args
	defer func() { os.Args = previous }()

	for _, testCase := range []struct {
		arguments []string
		eligible  bool
	}{
		{nil, true},
		{[]string{"--no-fix"}, true},
		{[]string{"--fix"}, false},
		{[]string{"--lint"}, false},
		{[]string{"--no-fix", "--lint"}, false},
		// The land gate's run, its own record since the key holds every argument (#72n9mq0).
		{[]string{"--no-fix", "--other-builds", "--json"}, true},
		{[]string{"source/a.ts"}, false},
	} {
		os.Args = append([]string{"cohere"}, testCase.arguments...)
		if got := runCacheEligible(); got != testCase.eligible {
			test.Errorf("cohere %v: eligible %v, want %v", testCase.arguments, got, testCase.eligible)
		}
	}

	// `--no-cache` is refused by name, so it holds even for an argument shape that is otherwise admitted.
	os.Args = []string{"cohere"}
	cacheOff = true
	defer func() { cacheOff = false }()
	if runCacheEligible() {
		test.Error("--no-cache left a bare run eligible, so a cold run could read or record the cache")
	}
}
