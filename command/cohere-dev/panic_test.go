package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/streams"
)

// panicChildVariable tells a test binary run by TestAPanicDeliversWhatWasWrittenBeforeIt to be the cohere-dev
// that panics.
const panicChildVariable = "COHERE_DEV_PANIC_CHILD"

// panicMarker is what the panicking cohere-dev writes to stderr first.
const panicMarker = "written to stderr before the panic"

// panicOnce is a stream that panics on its first write and takes every later one, so a deferred flush of the
// stream the panic came from does not panic a second time over the first.
type panicOnce struct {
	panicked bool
}

func (stream *panicOnce) Write(data []byte) (int, error) {
	if !stream.panicked {
		stream.panicked = true
		panic("the output stream broke")
	}
	return len(data), nil
}

// A panic in main still delivers what was written to stderr before it. The test binary runs itself as a
// cohere-dev with no verb, whose usage lands in stderr's buffer behind a line written first, and whose
// Finish flushes stdout first, into a stream that panics. Nothing else would flush stderr after that, so the
// lines arrive only through main's deferred show.
func TestAPanicDeliversWhatWasWrittenBeforeIt(test *testing.T) {
	if os.Getenv(panicChildVariable) == "1" {
		fmt.Fprintln(streams.Errors, panicMarker)
		streams.Output = bufio.NewWriter(&panicOnce{})
		fmt.Fprintln(streams.Output, "held in the buffer until the finish")
		os.Args = []string{"cohere-dev"}
		main()
		return
	}
	test.Parallel()
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	command := exec.Command(executable, "-test.run=^TestAPanicDeliversWhatWasWrittenBeforeIt$")
	command.Env = append(os.Environ(), panicChildVariable+"=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if runError := command.Run(); runError == nil {
		test.Fatalf("the cohere-dev that panics exited cleanly:\n%s", stderr.String())
	}
	text := stderr.String()
	panicAt := strings.Index(text, "panic: the output stream broke")
	if panicAt < 0 {
		test.Fatalf("cohere-dev did not panic where the test made it:\n%s", text)
	}
	before := text[:panicAt]
	if !strings.Contains(before, panicMarker+"\n") || !strings.Contains(before, "usage: cohere-dev test") {
		test.Fatalf("what was written to stderr before the panic did not arrive ahead of its report:\n%s", text)
	}
}
