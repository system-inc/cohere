package main

import (
	"bufio"
	"strings"
	"testing"
)

// flushStream writes what a stream holds into the buffer beneath it, as streams.Finish delivers a run's output,
// so a test reads everything the code under test printed. A flush that fails fails the test.
func flushStream(test testing.TB, stream *bufio.Writer) {
	test.Helper()
	if flushError := stream.Flush(); flushError != nil {
		test.Fatal(flushError)
	}
}

// TestALineNoOneReadsIsDiscarded: outside --verbose, accountOutput hands back a stream that keeps nothing, and
// under it the stream it was given.
// Not parallel: it sets the package-level activeOutput, which the printers' parallel tests read
func TestALineNoOneReadsIsDiscarded(test *testing.T) {
	saved := activeOutput
	defer func() { activeOutput = saved }()

	var out strings.Builder
	stream := bufio.NewWriter(&out)
	activeOutput = outputSettings{Mode: outputHuman}
	if account := accountOutput(stream); account == stream {
		test.Fatal("the human view's account is the stream itself, so its lines would print")
	}
	activeOutput = outputSettings{Mode: outputVerbose}
	if account := accountOutput(stream); account != stream {
		test.Fatal("--verbose's account is not the stream it was given")
	}
}
