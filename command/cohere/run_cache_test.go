package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every line written through a taggingWriter starts with the tag, however the writes are split.
//
// The lines it carries are the ones a replay must not print, so a line that escaped untagged would be
// replayed as current: "graph built in 436ms" on a run that built nothing. Partial writes are the case
// worth testing, since Fprintf can hand over a line in pieces.
func TestTaggingWriterTagsEveryLine(t *testing.T) {
	var out bytes.Buffer
	writer := &taggingWriter{out: &out, atLineStart: true}
	fmt.Fprint(writer, "graph built in ")
	fmt.Fprint(writer, "436ms\ntypes ran")
	fmt.Fprint(writer, " in 409ms\n")

	want := string(invocationTag) + "graph built in 436ms\n" + string(invocationTag) + "types ran in 409ms\n"
	if out.String() != want {
		t.Fatalf("tagged output:\n  got  %q\n  want %q", out.String(), want)
	}
}

// Without a recording, invocationOutput is the writer itself, so a run that is not recorded prints
// exactly what it printed before the run cache existed.
func TestInvocationOutputIsUntouchedWithoutARecording(t *testing.T) {
	previous := activeRunCache
	activeRunCache = nil
	defer func() { activeRunCache = previous }()

	var out bytes.Buffer
	if invocationOutput(&out) != &out {
		t.Fatal("with no recording in progress, invocation lines were still wrapped")
	}
}

// The tee strips the tag on the way to the terminal and keeps tagged lines out of the replay.
//
// Both halves matter and fail differently. A tag reaching the terminal prints a control byte at the
// front of a line; a tagged line reaching the buffer is replayed as a fact about this run. This drives
// the real tee through a real pipe, since the pipe is the reason the tag travels in the stream.
func TestTeeStripsTagsFromTheTerminalAndTheReplay(t *testing.T) {
	terminal, err := os.Create(filepath.Join(t.TempDir(), "terminal"))
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()

	target := terminal
	var tee teeStream
	if err := tee.start(&target); err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(target, "fix: 0 of 5 files rewritten\n")
	fmt.Fprint(&taggingWriter{out: target, atLineStart: true}, "graph built in 436ms\n")
	fmt.Fprint(target, "coverage: 460 rules\n")
	fmt.Fprint(&taggingWriter{out: target, atLineStart: true}, "phases: fix ran in 2.9s\n  total 4.0s\n")
	fmt.Fprint(target, "  this binary was built from a modified tree\n")
	tee.stop(&target)

	if target != terminal {
		t.Fatal("stop did not restore the real stream")
	}
	shown, err := os.ReadFile(terminal.Name())
	if err != nil {
		t.Fatal(err)
	}

	wantShown := "fix: 0 of 5 files rewritten\ngraph built in 436ms\ncoverage: 460 rules\nphases: fix ran in 2.9s\n  total 4.0s\n  this binary was built from a modified tree\n"
	if string(shown) != wantShown {
		t.Errorf("the terminal did not get every line untagged:\n  got  %q\n  want %q", shown, wantShown)
	}
	wantReplayed := "fix: 0 of 5 files rewritten\ncoverage: 460 rules\n  this binary was built from a modified tree\n"
	if tee.buffer.String() != wantReplayed {
		t.Errorf("the replay holds the wrong lines:\n  got  %q\n  want %q", tee.buffer.String(), wantReplayed)
	}
	if strings.Contains(tee.buffer.String(), string(invocationTag)) || bytes.Contains(shown, invocationTag) {
		t.Error("the tag itself escaped into the terminal or the replay")
	}
}
