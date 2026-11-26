package oracle_testing

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

func writeGolden(test *testing.T, contents record) string {
	test.Helper()
	path := filepath.Join(test.TempDir(), "golden.json.gz")
	file, createError := os.Create(path)
	if createError != nil {
		test.Fatal(createError)
	}
	zipped := gzip.NewWriter(file)
	if encodeError := json.NewEncoder(zipped).Encode(contents); encodeError != nil {
		test.Fatal(encodeError)
	}
	if closeError := zipped.Close(); closeError != nil {
		test.Fatal(closeError)
	}
	if closeError := file.Close(); closeError != nil {
		test.Fatal(closeError)
	}
	return path
}

// TestAGoldenFromOtherBundlesIsRefused is the property the golden exists to keep: answers recorded from
// bundles this build does not embed are never compared against.
func TestAGoldenFromOtherBundlesIsRefused(test *testing.T) {
	test.Parallel()
	path := writeGolden(test, record{Digest: "recorded", Answers: map[string]Answer{"k": {Output: "x"}}})
	if _, readError := read(
		path,
		"embedded",
	); readError == nil || !strings.Contains(readError.Error(), "recorded from Prettier bundles recorded") {
		test.Fatalf("a golden from other bundles was read: %v", readError)
	}
	// The control: the same file, with the digest it was recorded under, reads.
	answers, readError := read(path, "recorded")
	if readError != nil || answers["k"].Output != "x" {
		test.Fatalf("a golden from these bundles was refused: %v, %v", answers, readError)
	}
}

// TestAMissingGoldenIsRefused: no golden is a failure that says how to record one, never an empty pass.
func TestAMissingGoldenIsRefused(test *testing.T) {
	test.Parallel()
	if _, readError := read(filepath.Join(test.TempDir(), "absent.json.gz"), "embedded"); readError == nil {
		test.Fatal("a missing golden read as empty")
	}
}

// TestAGoldenNamedInAnotherCaseIsRefused: a golden whose stored name differs from its test's only in case
// fails on every disk, not only on a case-sensitive one where the open itself would miss it.
func TestAGoldenNamedInAnotherCaseIsRefused(test *testing.T) {
	test.Parallel()
	path := writeGolden(test, record{Digest: "embedded", Answers: map[string]Answer{"k": {Output: "x"}}})
	if _, readError := read(
		filepath.Join(filepath.Dir(path), "Golden.json.gz"),
		"embedded",
	); readError == nil || !strings.Contains(readError.Error(), "rename it to Golden.json.gz") {
		test.Fatalf("a golden stored as golden.json.gz was read as Golden.json.gz: %v", readError)
	}
	// The control: the name as stored reads.
	if answers, readError := read(path, "embedded"); readError != nil || answers["k"].Output != "x" {
		test.Fatalf("a golden read by its stored name was refused: %v, %v", answers, readError)
	}
}

// TestKeysDoNotRunTogether: inputs that concatenate to the same text are different keys.
func TestKeysDoNotRunTogether(test *testing.T) {
	test.Parallel()
	if Key("ab", "c") == Key("a", "bc") {
		test.Fatal("two different inputs share a key")
	}
	// Two calls, so the comparison is of two keys computed apart rather than one expression with itself.
	first := Key("a", "b")
	second := Key("a", "b")
	if first != second {
		test.Fatal("one input has two keys")
	}
}

// TestTheBundlesHaveADigest: the digest every golden is stamped with is readable from this build.
func TestTheBundlesHaveADigest(test *testing.T) {
	test.Parallel()
	digest, bundlesDigestError := BundlesDigest()
	if bundlesDigestError != nil || len(digest) != 64 {
		test.Fatalf("no digest for the embedded bundles: %q, %v", digest, bundlesDigestError)
	}
}

// recordingTest is a testing.TB whose errors are kept rather than failing the test that holds it, so a
// recording's own refusal can be asserted. Failed reads false until one arrives, as a real test's does while
// a panic unwinds toward its Cleanup.
type recordingTest struct {
	testing.TB
	errors []string
}

func (recording *recordingTest) Errorf(format string, arguments ...any) {
	recording.errors = append(recording.errors, fmt.Sprintf(format, arguments...))
}

func (recording *recordingTest) Failed() bool {
	return len(recording.errors) > 0
}

// TestARecordingThatPanicsWritesNoGolden: a recording whose engine panicked mid-run leaves the golden it
// would have replaced untouched. The panic reaches Cleanup before the test is marked failed, so a write there
// saved a golden holding only the answers given before it, which the next plain run read as the fork's word.
func TestARecordingThatPanicsWritesNoGolden(test *testing.T) {
	test.Parallel()
	path := writeGolden(test, record{Digest: "embedded", Answers: map[string]Answer{"k": {Output: "x"}}})
	before, readFileError := os.ReadFile(path)
	if readFileError != nil {
		test.Fatal(readFileError)
	}
	recording := &recordingTest{TB: test}
	golden := &Golden{test: recording, path: path, digest: "embedded", update: true, answers: map[string]Answer{}}
	given := func() (string, error) { return "given", nil }
	if _, answerError := golden.Answer(Key("given"), given); answerError != nil {
		test.Fatal(answerError)
	}
	func() {
		defer func() {
			if recover() == nil {
				test.Fatal("the compute's panic never reached the caller")
			}
		}()
		// Ignored: the call never returns, since its compute panics.
		_, _ = golden.Answer(Key("panicked"), func() (string, error) { panic("the engine fell over") })
	}()
	// What Cleanup runs when the test ends.
	golden.write()
	after, readFileError := os.ReadFile(path)
	if readFileError != nil {
		test.Fatal(readFileError)
	}
	if !bytes.Equal(before, after) {
		test.Fatalf("a recording whose compute panicked rewrote %s", path)
	}
	if len(recording.errors) == 0 {
		test.Error("the golden went unwritten without a word, so a recovered panic would read as a recording")
	}

	// The control: a recording whose every compute returned writes the golden, so the check above is not a
	// write that never happens.
	control := &Golden{
		test:    &recordingTest{TB: test},
		path:    path,
		digest:  "embedded",
		update:  true,
		answers: map[string]Answer{},
	}
	if _, answerError := control.Answer(Key("given"), given); answerError != nil {
		test.Fatal(answerError)
	}
	control.write()
	answers, readError := read(path, "embedded")
	if readError != nil || len(answers) != 1 || answers[Key("given")].Output != "given" {
		test.Fatalf("a recording that finished wrote %v, %v", answers, readError)
	}
}

// TestAPlainEvaluateNeverBuildsTheRuntime: a plain run reads Evaluate's answer from the golden and never builds
// the runtime that records it, so no JavaScript runs and the bundles are never evaluated.
func TestAPlainEvaluateNeverBuildsTheRuntime(test *testing.T) {
	test.Parallel()
	golden := &Golden{
		test:    test,
		path:    filepath.Join(test.TempDir(), "golden.json.gz"),
		digest:  "embedded",
		answers: map[string]Answer{Key("k"): {Output: "recorded"}},
		loadRuntime: func() (*goja.Runtime, error) {
			test.Error("a plain Evaluate built the runtime")
			return nil, errors.New("a plain run has no runtime")
		},
	}
	answer, evaluateError := golden.Evaluate(Key("k"), `throw new Error("the script ran")`, "input")
	if evaluateError != nil || answer != "recorded" {
		test.Fatalf("a plain Evaluate answered %q, %v, not the recorded answer", answer, evaluateError)
	}
	if golden.runtime != nil {
		test.Error("a plain Evaluate left a runtime behind")
	}
}
