package micromark

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/recorded"
)

// The event oracle: the fork's own micromark, run by Node with the extensions parse-markdown.js passes, asked
// for the event stream of each input. The Go port must produce the same stream, every token's type, direction
// and both positions, before mdast-util-from-markdown is ever involved, so a construct is measured at the
// layer it was written in.
//
// Upstream's answers are recorded, so the tests run no node and read nothing outside this repository. They are
// micromark 4.0.2's with micromark-extension-gfm 3.0.0, micromark-extension-math 3.1.0,
// @braindb/micromark-extension-wiki-link 0.1.0 and the fork's own liquid and html-text extensions, from the
// Prettier fork's node_modules and src, each package's production build, which is what the bundle carries.
// The one thing Node cannot reproduce is the bundle's rewritten Unicode punctuation class, which is why the Go
// side generates that table from the bundle instead; inputs here should not depend on a code point whose
// classification changed between the bundle's Unicode and the recording Node's.
//
// Each test's answers are testdata/micromark_js/<TestName>.json, plain JSON, and each answer carries what it was
// recorded for, the input's text or, past digestedTextLength, its recorded.Digest. The inputs are the fixture
// lists beside each construct, so an edited, added or removed fixture fails its test naming the recorder rather
// than comparing against a stale answer. An answer keeps upstream's events as their eventsFingerprint: the tests
// only compare them for equality with the port's, and upstream's can never be recorded again, since the packages
// that answered are gone from this repository, so a mismatch is a regression of ours, and its failure prints the
// port's events in full. The recorders are the two files attached to #vs0zx54: record-micromark-inputs_test.go,
// copied into this package for one run, writes every recorded test's inputs to a JSON file, and
// record-micromark-events.mjs, run by Node in a sandbox with no network, reads that file and writes each test's
// answers with their events whole, which were then written as these files hold them. Their usage is in their
// headers.

// micromarkJsDirectory holds upstream's recorded answers, one file per test.
var micromarkJsDirectory = filepath.Join("testdata", "micromark_js")

// digestedTextLength is the length in bytes past which an answer records its input's recorded.Digest rather
// than its text: a digest is 64 bytes, so a shorter text repeated beside its answer costs less and reads in
// review.
const digestedTextLength = 200

// eventAnswers is one test's recorded answers: where they came from, which of the recorder's modes asked for
// them, and one answer per input in the test's order.
type eventAnswers struct {
	Source   string        `json:"source"`
	Recorded string        `json:"recorded"`
	Mode     string        `json:"mode"`
	Answers  []eventAnswer `json:"answers"`
}

// eventAnswer is one input's answer: what it was recorded for (inputRecordedAs), and in the Events modes the
// eventsFingerprint of the event descriptions upstream gave, as portEvents writes them (or one "error: " line
// when upstream threw), or in the table align mode each table's `_align`, joined by commas. The aligns are kept
// whole: each is a few bytes, shorter than a fingerprint, and reads in review.
type eventAnswer struct {
	Text   string   `json:"text,omitempty"`
	Sha256 string   `json:"sha256,omitempty"`
	Events string   `json:"events"`
	Aligns []string `json:"aligns"`
}

// recordedFor is what the answer says it was recorded for: its digest, or else its text.
func (answer eventAnswer) recordedFor() string {
	if answer.Sha256 != "" {
		return answer.Sha256
	}
	return answer.Text
}

// inputRecordedAs is what an answer for input records of it: the text, or its recorded.Digest once the text is
// longer than digestedTextLength.
func inputRecordedAs(input string) string {
	if len(input) > digestedTextLength {
		return recorded.Digest([]byte(input))
	}
	return input
}

// eventsFingerprint is the recorded.Fingerprint an answer keeps of an event list: its descriptions joined by
// "\n", which no description holds.
func eventsFingerprint(events []string) string {
	return recorded.Fingerprint(strings.Join(events, "\n"))
}

// eventsDifference compares the port's events for an input with the fingerprint answer recorded of upstream's,
// and on a mismatch names both fingerprints and prints the port's events in full, one per line: upstream's can
// never be recorded again, so the port's are what to fix, and the length in each fingerprint says how far apart
// the two are.
func eventsDifference(answer eventAnswer, actual []string) string {
	fingerprint := eventsFingerprint(actual)
	if fingerprint == answer.Events {
		return ""
	}
	return fmt.Sprintf(
		"upstream's events are %s, and the port's are %s:\n%s",
		answer.Events,
		fingerprint,
		strings.Join(actual, "\n"),
	)
}

// readEventAnswers reads the test's recorded answers and requires them to be for exactly inputs, in order, asked
// in mode. A file whose JSON does not read whole, or that declares a field eventAnswers does not, is refused, and
// so is a compressed copy beside it, since compressed fixtures never land on a public repository.
func readEventAnswers(test *testing.T, mode string, inputs []string) eventAnswers {
	test.Helper()
	path := filepath.Join(micromarkJsDirectory, test.Name()+".json")
	switch _, statError := os.Stat(path + ".gz"); {
	case statError == nil:
		test.Fatalf("%s.gz is compressed, and compressed fixtures never land on a public repository", path)
	case !errors.Is(statError, fs.ErrNotExist):
		test.Fatal(statError)
	}
	answers, readJsonError := recorded.ReadJson[eventAnswers](path)
	if readJsonError != nil {
		test.Fatalf("%v: record them with the recorder attached to #vs0zx54", readJsonError)
	}
	if answers.Mode != mode {
		test.Fatalf("%s was recorded in mode %q, and this test asks %q", path, answers.Mode, mode)
	}
	if len(answers.Answers) != len(inputs) {
		test.Fatalf(
			"%s holds %d answers for %d inputs: record them again with the recorder attached to #vs0zx54",
			path,
			len(answers.Answers),
			len(inputs),
		)
	}
	for index, input := range inputs {
		answer := answers.Answers[index]
		if answer.Text != "" && answer.Sha256 != "" {
			test.Fatalf("%s: answer %d records both a text and a digest", path, index)
		}
		if answer.recordedFor() != inputRecordedAs(input) {
			test.Fatalf(
				"%s: input %d, %q, was recorded for another text, %q: record them again with the recorder attached "+
					"to #vs0zx54",
				path,
				index,
				truncateInput(input),
				truncateInput(answer.recordedFor()),
			)
		}
	}
	return answers
}

// compareEvents reports each input whose events, through the full extension set, differ from upstream's
// recorded ones.
func compareEvents(test *testing.T, inputs []string) (failures int) {
	test.Helper()
	answers := readEventAnswers(test, "Events", inputs)
	for index, input := range inputs {
		actual := portEvents(input)
		if difference := eventsDifference(answers.Answers[index], actual); difference != "" {
			failures++
			test.Errorf("input %q: %s", truncateInput(input), difference)
		}
	}
	return failures
}

// TestEventsMatchUpstream runs every fixture through both sides.
func TestEventsMatchUpstream(test *testing.T) {
	test.Parallel()
	compareEvents(test, eventFixtures)
}

// TestEventOracleCanFail proves the comparison sees a difference: a fixture compared against the events of a
// different input must fail. Since an answer is a fingerprint, any wrong one would fail too, so the control
// first holds the answer to the events of its own input.
func TestEventOracleCanFail(test *testing.T) {
	test.Parallel()
	answers := readEventAnswers(test, "Events", []string{"*a*"})
	if difference := eventsDifference(answers.Answers[0], portEvents("*a*")); difference != "" {
		test.Fatalf("input %q: %s", "*a*", difference)
	}
	if eventsDifference(answers.Answers[0], portEvents("**a**")) == "" {
		test.Fatal("the comparison found no difference between the events of *a* and **a**")
	}
}

// documentsDirectory is the markdown package's documents (see its documents_test.go): whole files built around
// tables, nested lists and embedded code, the constructs the corpus harnesses reported most.
var documentsDirectory = filepath.Join("..", "testdata", "documents")

// TestDocumentEventsMatchUpstream compares the events of every document with upstream's. It replaces
// TestCorpusEventsMatchUpstream, which walked whatever directory a variable named with a live Node; these
// documents are in the repository, and put many constructs side by side in one file, as the fixtures do not.
func TestDocumentEventsMatchUpstream(test *testing.T) {
	test.Parallel()
	paths, globError := filepath.Glob(filepath.Join(documentsDirectory, "*.md"))
	if globError != nil {
		test.Fatal(globError)
	}
	if len(paths) < 6 {
		test.Fatalf("%s holds %d documents, fewer than the six it was written with", documentsDirectory, len(paths))
	}
	inputs := make([]string, len(paths))
	for index, path := range paths {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		inputs[index] = string(contents)
	}
	answers := readEventAnswers(test, "Events", inputs)
	for index, input := range inputs {
		if difference := eventsDifference(answers.Answers[index], portEvents(input)); difference != "" {
			test.Errorf("%s: %s", filepath.Base(paths[index]), difference)
		}
	}
}
