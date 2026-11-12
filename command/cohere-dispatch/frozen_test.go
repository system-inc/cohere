//go:build !windows

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
	"github.com/system-inc/cohere/internal/streams"
)

// stagedHash names the binary writeFrozenModule stages, so the announcement can be read for it.
const stagedHash = "0123456789abcdef"

// stagedBinaryName is the staged binary's file name, as the launcher names a build from that hash.
var stagedBinaryName = fmt.Sprintf("cohere-%s-%s-%s", runtime.GOOS, runtime.GOARCH, stagedHash)

// writeFrozenModule writes a cohere module whose binary cache holds one binary, a script that writes a line to
// stdout and then one to stderr, and returns the module's root. A dispatcher run there with --frozen announces
// that script and runs it.
func writeFrozenModule(test *testing.T) string {
	root := test.TempDir()
	module := []byte("module github.com/system-inc/cohere\n")
	if writeError := os.WriteFile(filepath.Join(root, "go.mod"), module, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	binaries := filepath.Join(root, ".cache", "cohere", "bin")
	if createDirectoryError := os.MkdirAll(binaries, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	stub := "#!/bin/sh\necho 'the stub engine on stdout'\necho 'the stub engine on stderr' >&2\n"
	if writeError := executable_testing.WriteFile(
		filepath.Join(binaries, stagedBinaryName),
		[]byte(stub),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	return root
}

// The --frozen announcement is written to a buffer, and the binary it announces writes to the terminal itself,
// so it reaches the caller only if the dispatcher delivers it before handing the terminal over: before the
// exec when it waits, which drops the buffer whole, and before the child starts when it returns early, or it
// lands after everything the engine said. Both modes are run with stdout and stderr on one pipe, so the order
// the caller reads is the order it was written in. COHERE_BINARY is emptied, since a binary named that way is
// run with nothing buffered ahead of it, and so tests neither flush.
func TestTheFrozenAnnouncementArrivesWholeBeforeTheEngineSpeaks(test *testing.T) {
	test.Parallel()
	dispatcher := buildBinary(test, "cohere-dispatch", ".")
	expected := regexp.MustCompile(
		`^cohere: --frozen, so the rules on disk were never checked against this binary\.\n` +
			`  running: ` + regexp.QuoteMeta(stagedBinaryName) + `\n` +
			`  hash:    ` + stagedHash + `\n` +
			`  built:   \d{4}-\d\d-\d\d \d\d:\d\d:\d\d\n` +
			`the stub engine on stdout\n` +
			`the stub engine on stderr\n$`)
	for _, mode := range []struct {
		name string
		wait string
	}{{"waiting, through exec", "1"}, {"returning early, through a child", ""}} {
		test.Run(mode.name, func(test *testing.T) {
			test.Parallel()
			command := exec.Command(dispatcher, "--frozen")
			command.Dir = writeFrozenModule(test)
			command.Env = append(os.Environ(), "COHERE_BINARY=", "CI=", "COHERE_WAIT="+mode.wait)
			var output bytes.Buffer
			command.Stdout, command.Stderr = &output, &output
			if runError := command.Run(); runError != nil {
				test.Fatalf("the frozen run failed: %v\n%s", runError, output.String())
			}
			if !expected.MatchString(output.String()) {
				test.Fatalf("the announcement did not arrive whole and first, "+
					"so a frozen run can read as an ordinary one:\n%s", output.String())
			}
		})
	}
}

// panicChildVariable tells a test binary run by TestAPanicDeliversWhatWasWrittenBeforeIt to be the dispatcher
// that panics.
const panicChildVariable = "COHERE_DISPATCH_PANIC_CHILD"

// panicMarker is what the panicking dispatcher writes to stderr first.
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

// A panic in main still delivers what was written to stderr before it. The test binary runs itself as the
// dispatcher, with --frozen and waiting, so the announcement and a line before it sit in stderr's buffer when
// the exec's Finish flushes stdout first, and stdout is a stream that panics there. Nothing else would flush
// stderr after that, so the lines arrive only through the show finishedExitCode defers. The child is a process
// of its own running this test alone, so the globals it sets are shared with no other test, and the test runs in
// parallel.
func TestAPanicDeliversWhatWasWrittenBeforeIt(test *testing.T) {
	test.Parallel()
	if os.Getenv(panicChildVariable) == "1" {
		fmt.Fprintln(streams.Errors, panicMarker)
		streams.Output = bufio.NewWriter(&panicOnce{})
		fmt.Fprintln(streams.Output, "held in the buffer until the exec")
		os.Args = []string{"cohere-dispatch", "--frozen"}
		main()
		return
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	command := exec.Command(executable, "-test.run=^TestAPanicDeliversWhatWasWrittenBeforeIt$")
	command.Dir = writeFrozenModule(test)
	command.Env = append(os.Environ(), panicChildVariable+"=1", "COHERE_BINARY=", "CI=", "COHERE_WAIT=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if runError := command.Run(); runError == nil {
		test.Fatalf("the dispatcher that panics exited cleanly:\n%s", stderr.String())
	}
	text := stderr.String()
	panicAt := strings.Index(text, "panic: the output stream broke")
	if panicAt < 0 {
		test.Fatalf("the dispatcher did not panic where the test made it:\n%s", text)
	}
	before := text[:panicAt]
	if !strings.Contains(before, panicMarker) || !strings.Contains(before, "  hash:    "+stagedHash+"\n") {
		test.Fatalf("what was written to stderr before the panic did not arrive ahead of its report:\n%s", text)
	}
}
