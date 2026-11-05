// Package streams holds the two streams a person reads from cohere's commands, buffered so a write that fails
// is never lost.
//
// A write to stdout or stderr can fail: the reader closed the pipe, the disk under a redirect filled. A command
// that drops that error exits as though its output arrived, which is a green nobody saw. Checking the error at
// every print would be several hundred checks saying the same thing, so every print goes through a
// bufio.Writer instead, whose error is sticky by its documented contract: "If an error occurs writing to a
// Writer, no more data will be accepted and all subsequent writes, and Flush, will return the error." Nothing
// is lost by not checking each write, as long as the Flush where the exit code is decided is checked, and
// Finish is that Flush (ruling 5 on #pp04nv8, its mechanism as ruled on #ec8mvbg).
package streams

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/system-inc/cohere/internal/sentence"
)

// Output is stdout, and Errors is stderr, each as a person reads it.
var Output, Errors = bufio.NewWriter(os.Stdout), bufio.NewWriter(os.Stderr)

// Show flushes a stream at a point where a person must see what was written so far: a line of progress before
// a wait, a finding as it streams. Its error is not checked here, because a bufio.Writer keeps it, and Finish
// returns it where the exit code is decided.
func Show(stream *bufio.Writer) {
	// Ignored: a bufio.Writer's error is sticky, and Finish's Flush returns it where the exit code is decided.
	_ = stream.Flush()
}

// Finish flushes both streams and returns the exit code the command should end with: the one it was going to
// end with, or 1 in place of a 0 when either stream failed, since output that never reached the reader is a
// run that did not finish. A stdout failure is said on stderr when stderr still works.
func Finish(exitCode int) int {
	return finish(exitCode, Output, Errors, os.Stderr)
}

// finish is Finish with its streams handed in, so a test can make one fail. lastResort is where a failure of
// output is reported, the unbuffered stderr, written once and then nothing more, under the running program's
// name, since cohere, cohere-dev and cohere-dispatch all finish here.
func finish(exitCode int, output *bufio.Writer, errorStream *bufio.Writer, lastResort io.Writer) int {
	outputError, errorsError := output.Flush(), errorStream.Flush()
	if outputError == nil && errorsError == nil {
		return exitCode
	}
	if outputError != nil && errorsError == nil {
		// Ignored: this is the report of a failed write; if stderr fails too there is nowhere left to say it,
		// and the exit code below already says the run did not finish.
		_, _ = fmt.Fprintf(lastResort, "%s: %s\n", filepath.Base(os.Args[0]),
			sentence.FromError(fmt.Errorf("writing the output failed, so it may not have reached you: %w", outputError)))
	}
	return max(exitCode, 1)
}
