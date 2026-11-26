package micromark

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
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
// Each test's answers are testdata/micromark_js/<TestName>.json.gz, and each answer carries the text it was
// recorded for. The inputs are the fixture lists beside each construct, so an edited, added or removed fixture
// fails its test naming the recorder rather than comparing against a stale answer. To record them again, use the
// two files attached to #vs0zx54: record-micromark-inputs_test.go, copied into this package for one run, writes
// every recorded test's inputs to a JSON file, and record-micromark-events.mjs, run by Node in a sandbox with no
// network, reads that file and writes each test's answers, to gzip -9n into place. Their usage is in their
// headers.

// micromarkJsDirectory holds upstream's recorded answers, one file per test.
var micromarkJsDirectory = filepath.Join("testdata", "micromark_js")

// eventAnswers is one test's recorded answers: where they came from, which of the recorder's modes asked for
// them, and one answer per input in the test's order. An answer is the event descriptions portEvents writes
// (or one "error: " line when upstream threw), or for the table align mode each table's `_align`, joined.
type eventAnswers struct {
	Source   string `json:"source"`
	Recorded string `json:"recorded"`
	Mode     string `json:"mode"`
	Answers  []struct {
		Text   string   `json:"text"`
		Events []string `json:"events"`
		Aligns []string `json:"aligns"`
	} `json:"answers"`
}

// readEventAnswers reads the test's recorded answers and requires them to be for exactly inputs, in order, asked
// in mode. A file whose gzip or JSON does not read whole is refused.
func readEventAnswers(test *testing.T, mode string, inputs []string) eventAnswers {
	test.Helper()
	path := filepath.Join(micromarkJsDirectory, test.Name()+".json.gz")
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatalf("%v: record them with the recorder attached to #vs0zx54", openError)
	}
	// Ignored: the file is only read, so a failed close loses nothing already decoded.
	defer file.Close()
	unzipped, gzipError := gzip.NewReader(file)
	if gzipError != nil {
		test.Fatalf("%s: %v", path, gzipError)
	}
	var answers eventAnswers
	decoder := json.NewDecoder(unzipped)
	if decodeError := decoder.Decode(&answers); decodeError != nil {
		test.Fatalf("%s: %v", path, decodeError)
	}
	// The decoder stops at the end of the object; reading on to the end of the stream is what checks the gzip
	// trailer's checksum, so a corrupt file cannot pass as long as its first object decodes.
	var trailing json.RawMessage
	if trailingError := decoder.Decode(&trailing); trailingError == nil {
		test.Fatalf("%s holds more than one JSON value", path)
	} else if !errors.Is(trailingError, io.EOF) {
		test.Fatalf("%s: %v", path, trailingError)
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
		if answers.Answers[index].Text != input {
			test.Fatalf(
				"%s: input %d, %q, was recorded for another text, %q: record them again with the recorder attached "+
					"to #vs0zx54",
				path,
				index,
				truncateInput(input),
				truncateInput(answers.Answers[index].Text),
			)
		}
	}
	return answers
}

// compareEvents reports the first divergence per input from upstream's recorded events, through the full
// extension set.
func compareEvents(test *testing.T, inputs []string) (failures int) {
	test.Helper()
	answers := readEventAnswers(test, "Events", inputs)
	for index, input := range inputs {
		actual := portEvents(input)
		if difference := firstEventDifference(answers.Answers[index].Events, actual); difference != "" {
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
// different input must fail.
func TestEventOracleCanFail(test *testing.T) {
	test.Parallel()
	answers := readEventAnswers(test, "Events", []string{"*a*"})
	if firstEventDifference(answers.Answers[0].Events, portEvents("**a**")) == "" {
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
		if difference := firstEventDifference(answers.Answers[index].Events, portEvents(input)); difference != "" {
			test.Errorf("%s: %s", filepath.Base(paths[index]), difference)
		}
	}
}
