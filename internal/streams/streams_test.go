package streams

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
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
			exitCode := finish(testCase.exitCode, output, errorsStream, &lastResort, &delivery{})
			if exitCode != testCase.expected {
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

// TestAReportedFailureIsCountedAndSaidOnce: a failure reported from another route turns a clean exit into 1
// though both streams arrived, it is said once, and a second finish returns the same code without saying it
// again, since the run cache finishes before its verdict and the exit finishes once more.
func TestAReportedFailureIsCountedAndSaidOnce(test *testing.T) {
	test.Parallel()
	var arrivedOutput, arrivedErrors, lastResort bytes.Buffer
	output, errorsStream := bufio.NewWriter(&arrivedOutput), bufio.NewWriter(&arrivedErrors)
	state := &delivery{}
	state.report(errors.New("copying to the terminal: the reader is gone"))
	state.report(errors.New("a second failure, which is not the one said"))
	for round := 1; round <= 2; round++ {
		if exitCode := finish(0, output, errorsStream, &lastResort, state); exitCode != 1 {
			test.Fatalf("finish round %d = %d, expected 1", round, exitCode)
		}
	}
	if said := strings.Count(lastResort.String(), "Writing the output failed"); said != 1 {
		test.Errorf("the failure was said %d times, expected once: %q", said, lastResort.String())
	}
	if !strings.Contains(lastResort.String(), "the reader is gone") || strings.Contains(lastResort.String(), "second") {
		test.Errorf("the failure said is not the first reported: %q", lastResort.String())
	}
}

// TestAStreamFollowsItsFileThroughASwap: what a stream writes before a swap of its file, shown before the swap,
// lands in the first file, and what it writes after lands in the second, as the run cache's recording needs.
func TestAStreamFollowsItsFileThroughASwap(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	first, firstError := os.Create(filepath.Join(directory, "first"))
	if firstError != nil {
		test.Fatal(firstError)
	}
	second, secondError := os.Create(filepath.Join(directory, "second"))
	if secondError != nil {
		test.Fatal(secondError)
	}
	target := first
	stream := Over(&target)
	stream.WriteString("before\n")
	Show(stream)
	target = second
	stream.WriteString("after\n")
	Show(stream)
	for _, file := range []*os.File{first, second} {
		if closeError := file.Close(); closeError != nil {
			test.Fatal(closeError)
		}
	}
	for name, expected := range map[string]string{"first": "before\n", "second": "after\n"} {
		content, readError := os.ReadFile(filepath.Join(directory, name))
		if readError != nil {
			test.Fatal(readError)
		}
		if string(content) != expected {
			test.Errorf("%s holds %q, expected %q", name, content, expected)
		}
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
