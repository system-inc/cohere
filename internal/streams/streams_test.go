package streams

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// failingWriter refuses every write, as a pipe whose reader has gone does.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("the reader is gone") }

// TestAFailedWriteIsANonzeroExit: output that never reached the reader turns a clean exit into 1, keeps a
// failing exit as it was, and is said on the last-resort stream, while output that arrived changes nothing.
func TestAFailedWriteIsANonzeroExit(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name         string
		outputFails  bool
		errorsFails  bool
		exitCode     int
		expected     int
		expectReport bool
	}{
		{"both arrive, a clean exit stays clean", false, false, 0, 0, false},
		{"both arrive, a failing exit stays as it was", false, false, 3, 3, false},
		{"stdout fails, a clean exit becomes 1 and says so", true, false, 0, 1, true},
		{"stdout fails, a failing exit keeps its code", true, false, 2, 2, true},
		{"stderr fails, a clean exit becomes 1 with nowhere to say it", false, true, 0, 1, false},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			var arrivedOutput, arrivedErrors, lastResort bytes.Buffer
			output, errorsStream := bufio.NewWriter(&arrivedOutput), bufio.NewWriter(&arrivedErrors)
			if testCase.outputFails {
				output = bufio.NewWriter(failingWriter{})
			}
			if testCase.errorsFails {
				errorsStream = bufio.NewWriter(failingWriter{})
			}
			// Written and never flushed by the test, so only finish's Flush can deliver it or find it failed.
			output.WriteString("a finding\n")
			errorsStream.WriteString("a note\n")
			if exitCode := finish(testCase.exitCode, output, errorsStream, &lastResort); exitCode != testCase.expected {
				test.Fatalf("finish(%d) = %d, expected %d", testCase.exitCode, exitCode, testCase.expected)
			}
			reported := strings.Contains(lastResort.String(), "Writing the output failed")
			if reported != testCase.expectReport {
				test.Errorf("the failure was reported %t, expected %t: %q",
					reported, testCase.expectReport, lastResort.String())
			}
			if !testCase.outputFails && arrivedOutput.String() != "a finding\n" {
				test.Errorf("stdout received %q, expected the finding", arrivedOutput.String())
			}
		})
	}
}

// TestShowDeliversWhatWasWritten: a stream shown is delivered at once, before anything finishes it.
func TestShowDeliversWhatWasWritten(test *testing.T) {
	test.Parallel()
	var arrived bytes.Buffer
	stream := bufio.NewWriter(&arrived)
	stream.WriteString("waiting for a token\n")
	Show(stream)
	if arrived.String() != "waiting for a token\n" {
		test.Fatalf("Show delivered %q", arrived.String())
	}
}
