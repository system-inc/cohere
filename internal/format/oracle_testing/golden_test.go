package oracle_testing

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
// bundles other than the ones named are never compared against.
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

// TestAMissingGoldenIsRefused: no golden is a failure, never an empty pass.
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

// recordingTest is a testing.TB whose errors are kept rather than failing the test that holds it, so a
// golden's own complaint can be asserted.
type recordingTest struct {
	testing.TB
	errors []string
}

func (recording *recordingTest) Errorf(format string, arguments ...any) {
	recording.errors = append(recording.errors, fmt.Sprintf(format, arguments...))
}

// TestAnAnswerIsWhatWasRecorded: a recorded output comes back as the output and a recorded refusal as the
// error, while an answer never recorded fails the test and is ErrNotRecorded, never an empty pass.
func TestAnAnswerIsWhatWasRecorded(test *testing.T) {
	test.Parallel()
	recording := &recordingTest{TB: test}
	golden := &Golden{
		test:    recording,
		path:    "golden.json.gz",
		answers: map[string]Answer{Key("printed"): {Output: "x"}, Key("refused"): {Error: "SyntaxError"}},
	}
	if output, answerError := golden.Answer(Key("printed")); answerError != nil || output != "x" {
		test.Errorf("a recorded output answered %q, %v", output, answerError)
	}
	if _, answerError := golden.Answer(Key("refused")); answerError == nil || answerError.Error() != "SyntaxError" {
		test.Errorf("a recorded refusal answered %v", answerError)
	}
	if len(recording.errors) != 0 {
		test.Fatalf("recorded answers failed the test: %v", recording.errors)
	}
	if _, answerError := golden.Answer(Key("never")); !errors.Is(answerError, ErrNotRecorded) ||
		len(recording.errors) != 1 {
		test.Errorf("an answer never recorded gave %v and failed the test %d times", answerError, len(recording.errors))
	}
}

// TestAGoldenStampedOtherwiseIsRefused: Open holds every golden to the pinned digest, so one stamped with any
// other is refused, not compared.
func TestAGoldenStampedOtherwiseIsRefused(test *testing.T) {
	test.Parallel()
	path := writeGolden(test, record{Digest: "recorded", Answers: map[string]Answer{"k": {Output: "x"}}})
	if _, openError := openAt(test, path); openError == nil ||
		!strings.Contains(openError.Error(), "pinned to "+pinnedBundlesDigest) {
		test.Fatalf("a golden stamped with another digest was opened: %v", openError)
	}
	// The control: the same answers stamped with the pinned digest open.
	pinned := writeGolden(test, record{Digest: pinnedBundlesDigest, Answers: map[string]Answer{"k": {Output: "x"}}})
	golden, openError := openAt(test, pinned)
	if openError != nil || golden.answers["k"].Output != "x" {
		test.Fatalf("a golden stamped with the pinned digest was refused: %v", openError)
	}
}
