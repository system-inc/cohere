// Package upstream_testing holds the shapes of the yaml packages' recorded comparisons (#jddr94a): the
// inputs files, the yaml-test-suite's part files under internal/format/yaml/testdata/yaml_test_suite, the
// corpus under internal/format/yaml/testdata/prettier_tests, and the answers files eemeli/yaml's composer
// and CST and yaml-unist-parser's trees were recorded into. Reading and pairing are internal/testing/recorded's;
// this package names the files, fails the test, and says how to record again.
//
// The recorder is record-yaml.mjs, attached to #jddr94a, usage in its header. An answer carries its input's
// name and the text it was recorded for, or, for a vendored input (a suite case or a corpus file), the
// text's SHA-256, so the tree holds one copy of each vendored text. An edited input, an input with no
// answer, or an answer with no input fails the test naming the recorder, so no answer is ever compared
// against a text it was not recorded for.
package upstream_testing

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/testing/recorded"
)

// RecordAgain is what every mismatch between inputs and answers tells the reader to do.
const RecordAgain = "record them again with record-yaml.mjs, attached to #jddr94a"

// Input is one text a recording was made for, under a name unique in its file. Vendored says its answer
// records the text's digest rather than the text.
type Input struct {
	Name     string `json:"name"`
	Text     string `json:"text"`
	Vendored bool   `json:"-"`
}

// inputsFile is an inputs file: where its texts came from, whether they are vendored, and the texts.
type inputsFile struct {
	Source   string  `json:"source"`
	Vendored bool    `json:"vendored"`
	Inputs   []Input `json:"inputs"`
}

// readJson is recorded.ReadJson, failing the test on an error.
func readJson[Contents any](test testing.TB, path string) Contents {
	test.Helper()
	contents, readError := recorded.ReadJson[Contents](path)
	if readError != nil {
		test.Fatal(readError)
	}
	return contents
}

// ReadInputs reads an inputs file, refusing one that holds no inputs.
func ReadInputs(test testing.TB, path string) []Input {
	test.Helper()
	file := readJson[inputsFile](test, path)
	if len(file.Inputs) == 0 {
		test.Fatalf("%s holds no inputs", path)
	}
	for index := range file.Inputs {
		file.Inputs[index].Vendored = file.Vendored
	}
	return file.Inputs
}

// SuiteCase is one case of the yaml-test-suite: its id (with the case's index when a test has several),
// title, tags, whether the suite calls it an error, its YAML and, for most cases, its canonical dump.
type SuiteCase struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
	Fail  bool     `json:"fail"`
	Yaml  string   `json:"yaml"`
	Dump  *string  `json:"dump"`
}

// suitePart is one of the suite's part files.
type suitePart struct {
	Source   string      `json:"source"`
	Recorded string      `json:"recorded"`
	Cases    []SuiteCase `json:"cases"`
}

// SuitePartCount is how many part files the suite's cases are split across, each under 50 KB.
const SuitePartCount = 6

// SuitePartPath is the path of part (1 to SuitePartCount) under directory, the caller's way to
// internal/format/yaml/testdata/yaml_test_suite.
func SuitePartPath(directory string, part int) string {
	return filepath.Join(directory, fmt.Sprintf("cases_%d.json", part))
}

// ReadSuitePart reads one part file's cases.
func ReadSuitePart(test testing.TB, directory string, part int) []SuiteCase {
	test.Helper()
	path := SuitePartPath(directory, part)
	file := readJson[suitePart](test, path)
	if len(file.Cases) == 0 {
		test.Fatalf("%s holds no cases", path)
	}
	return file.Cases
}

// SuiteInputs are a part's cases as inputs, the recorder's way: each case's YAML under its name and, when
// withDumps, its dump under its name and " dump". Every one is vendored.
func SuiteInputs(cases []SuiteCase, withDumps bool) []Input {
	inputs := make([]Input, 0, 2*len(cases))
	for _, suiteCase := range cases {
		inputs = append(inputs, Input{Name: suiteCase.Name, Text: suiteCase.Yaml, Vendored: true})
		if withDumps && suiteCase.Dump != nil {
			inputs = append(inputs, Input{Name: suiteCase.Name + " dump", Text: *suiteCase.Dump, Vendored: true})
		}
	}
	return inputs
}

// CorpusHalfCount is how many halves the corpus's YAML files are split into, in path order, so every
// answers file recorded for one stays under 50 KB. The corpus is Prettier's own format fixtures, under
// internal/format/yaml/testdata/prettier_tests with Prettier's MIT notice: tests/format/yaml but its
// yaml-test-suite directory and the spec examples the yaml-test-suite holds, so each text is in the tree
// once, and the markdown fixtures' YAML front matter.
const CorpusHalfCount = 2

// ReadCorpusHalf reads one half (1 to CorpusHalfCount) of the corpus's YAML files under directory, the
// caller's way to internal/format/yaml/testdata/prettier_tests, each named by its path under the fork's
// tests/format.
func ReadCorpusHalf(test testing.TB, directory string, half int) []Input {
	test.Helper()
	return ReadInputs(test, filepath.Join(directory, fmt.Sprintf("yaml_inputs_%d.json", half)))
}

// FrontMatterInput is one of the corpus's markdown files with YAML front matter: its path under the fork's
// tests/format, its text, and the value Prettier's front matter parser reads from it, trimmed as the
// front matter embed trims it.
type FrontMatterInput struct {
	Name  string `json:"name"`
	Text  string `json:"text"`
	Value string `json:"value"`
}

// ReadFrontMatter reads the corpus's front matter file under directory.
func ReadFrontMatter(test testing.TB, directory string) []FrontMatterInput {
	test.Helper()
	path := filepath.Join(directory, "front_matter_inputs.json")
	file := readJson[struct {
		Source   string             `json:"source"`
		Vendored bool               `json:"vendored"`
		Inputs   []FrontMatterInput `json:"inputs"`
	}](test, path)
	if !file.Vendored {
		test.Fatalf("%s does not say its texts are vendored", path)
	}
	if len(file.Inputs) == 0 {
		test.Fatalf("%s holds no inputs", path)
	}
	return file.Inputs
}

// FrontMatterValues are the front matter values as inputs, the recorder's way: each under its file's
// name and " (front matter)", vendored.
func FrontMatterValues(inputs []FrontMatterInput) []Input {
	values := make([]Input, len(inputs))
	for index, input := range inputs {
		values[index] = Input{Name: input.Name + " (front matter)", Text: input.Value, Vendored: true}
	}
	return values
}

// Answers is an answers file: where the answers came from (package, version, the path in the Prettier
// fork's node_modules, the node that ran it), the date, and one answer per input.
type Answers[Answer any] struct {
	Source   string   `json:"source"`
	Recorded string   `json:"recorded"`
	Answers  []Answer `json:"answers"`
}

// ReadAnswers reads an answers file, refusing one that does not say where or when it was recorded.
func ReadAnswers[Answer any](test testing.TB, path string) Answers[Answer] {
	test.Helper()
	answers := readJson[Answers[Answer]](test, path)
	if answers.Source == "" || answers.Recorded == "" {
		test.Fatalf("%s does not say where or when it was recorded", path)
	}
	return answers
}

// RecordedFor is what an answer was recorded for, as its file holds it: the input's digest when the answer
// has one, else its text.
func RecordedFor(text string, sha256 string) string {
	if sha256 != "" {
		return sha256
	}
	return text
}

// Match pairs every input with its answer by name (recorded.Match), and returns the answers by name.
// identify reads an answer's name and what it was recorded for (RecordedFor): an input's text, or a vendored
// input's digest. Every input must have exactly one answer, recorded for its text, and every answer its
// input; anything else fails the test, naming the recorder.
func Match[Answer any](
	test testing.TB,
	path string,
	inputs []Input,
	answers []Answer,
	identify func(Answer) (string, string),
) map[string]Answer {
	test.Helper()
	inputEntries := make([]recorded.Entry, len(inputs))
	for index, input := range inputs {
		inputEntries[index] = recorded.Entry{Name: input.Name, Input: input.Text}
		if input.Vendored {
			inputEntries[index].Input = recorded.Digest([]byte(input.Text))
		}
	}
	answerEntries := make([]recorded.Entry, len(answers))
	for index, answer := range answers {
		answerEntries[index].Name, answerEntries[index].Input = identify(answer)
	}
	indexes, matchError := recorded.Match(inputEntries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: %s", path, matchError, RecordAgain)
	}
	byName := make(map[string]Answer, len(inputs))
	for position, input := range inputs {
		byName[input.Name] = answers[indexes[position]]
	}
	return byName
}
