// Package upstream_testing reads the upstream answers the YAML parser tests compare against (#jddr94a):
// eemeli/yaml's composer and CST and yaml-unist-parser's trees, recorded by node once and committed, so the
// tests run no process and read nothing outside the repository.
//
// A test's inputs live in a plain JSON file, or in the yaml-test-suite's part files under
// internal/format/yaml/testdata/yaml_test_suite, and upstream's answers in a gzipped file beside them, each
// answer carrying the text it was recorded for. The recorder is record-yaml.mjs, attached to #jddr94a,
// usage in its header. An edited input, an input with no answer, or an answer with no input fails the test
// naming the recorder, so no answer is ever compared against a text it was not recorded for.
package upstream_testing

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// RecordAgain is what every mismatch between inputs and answers tells the reader to do.
const RecordAgain = "record them again with record-yaml.mjs, attached to #jddr94a"

// Input is one text a recording was made for, under a name unique in its file.
type Input struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// inputsFile is an inputs file: where its texts came from, and the texts.
type inputsFile struct {
	Source string  `json:"source"`
	Inputs []Input `json:"inputs"`
}

// ReadInputs reads an inputs file, refusing one that holds no inputs.
func ReadInputs(test testing.TB, path string) []Input {
	test.Helper()
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	var file inputsFile
	if decodeError := json.Unmarshal(contents, &file); decodeError != nil {
		test.Fatalf("%s: %v", path, decodeError)
	}
	if len(file.Inputs) == 0 {
		test.Fatalf("%s holds no inputs", path)
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
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	var file suitePart
	if decodeError := json.Unmarshal(contents, &file); decodeError != nil {
		test.Fatalf("%s: %v", path, decodeError)
	}
	if len(file.Cases) == 0 {
		test.Fatalf("%s holds no cases", path)
	}
	return file.Cases
}

// SuiteInputs are a part's cases as inputs, the recorder's way: each case's YAML under its name and, when
// withDumps, its dump under its name and " dump".
func SuiteInputs(cases []SuiteCase, withDumps bool) []Input {
	inputs := make([]Input, 0, 2*len(cases))
	for _, suiteCase := range cases {
		inputs = append(inputs, Input{Name: suiteCase.Name, Text: suiteCase.Yaml})
		if withDumps && suiteCase.Dump != nil {
			inputs = append(inputs, Input{Name: suiteCase.Name + " dump", Text: *suiteCase.Dump})
		}
	}
	return inputs
}

// CorpusHalfCount is how many halves the corpus's YAML files are split into, in path order, so every
// answers file recorded for one stays under 50 KB. The corpus is Prettier's own format fixtures, under
// internal/format/yaml/testdata/prettier_tests with Prettier's MIT notice.
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
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	var file struct {
		Inputs []FrontMatterInput `json:"inputs"`
	}
	if decodeError := json.Unmarshal(contents, &file); decodeError != nil {
		test.Fatalf("%s: %v", path, decodeError)
	}
	if len(file.Inputs) == 0 {
		test.Fatalf("%s holds no inputs", path)
	}
	return file.Inputs
}

// FrontMatterValues are the front matter values as inputs, the recorder's way: each under its file's
// name and " (front matter)".
func FrontMatterValues(inputs []FrontMatterInput) []Input {
	values := make([]Input, len(inputs))
	for index, input := range inputs {
		values[index] = Input{Name: input.Name + " (front matter)", Text: input.Value}
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

// ReadAnswers reads a gzipped answers file, refusing one whose gzip or JSON does not read whole.
func ReadAnswers[Answer any](test testing.TB, path string) Answers[Answer] {
	test.Helper()
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	// Ignored: the file is only read, so a failed close loses nothing already decoded.
	defer file.Close()
	unzipped, gzipError := gzip.NewReader(file)
	if gzipError != nil {
		test.Fatalf("%s: %v", path, gzipError)
	}
	var answers Answers[Answer]
	decoder := json.NewDecoder(unzipped)
	if decodeError := decoder.Decode(&answers); decodeError != nil {
		test.Fatalf("%s: %v", path, decodeError)
	}
	// The decoder stops at the end of the object; reading on to the end of the stream is what checks the
	// gzip trailer's checksum, so a corrupt file cannot pass as long as its first object decodes.
	var trailing json.RawMessage
	if trailingError := decoder.Decode(&trailing); trailingError == nil {
		test.Fatalf("%s holds more than one JSON value", path)
	} else if !errors.Is(trailingError, io.EOF) {
		test.Fatalf("%s: %v", path, trailingError)
	}
	if answers.Source == "" || answers.Recorded == "" {
		test.Fatalf("%s does not say where or when it was recorded", path)
	}
	return answers
}

// Match pairs every input with its answer, by name, and returns the answers by name. identify reads an
// answer's name and the text it was recorded for. Every input must have exactly one answer, recorded for
// its text, and every answer its input; anything else fails the test, naming the recorder.
func Match[Answer any](
	test testing.TB,
	path string,
	inputs []Input,
	answers []Answer,
	identify func(Answer) (string, string),
) map[string]Answer {
	test.Helper()
	byName := make(map[string]Answer, len(answers))
	for _, answer := range answers {
		name, _ := identify(answer)
		if _, repeated := byName[name]; repeated {
			test.Fatalf("%s answers %q twice: %s", path, name, RecordAgain)
		}
		byName[name] = answer
	}
	inputNames := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		if inputNames[input.Name] {
			test.Fatalf("the inputs of %s name %q twice", path, input.Name)
		}
		inputNames[input.Name] = true
		answer, answered := byName[input.Name]
		if !answered {
			test.Fatalf("%q has no answer in %s: %s", input.Name, path, RecordAgain)
		}
		if _, text := identify(answer); text != input.Text {
			test.Fatalf("%q was recorded in %s for another text, %q: %s", input.Name, path, text, RecordAgain)
		}
	}
	var orphans []string
	for name := range byName {
		if !inputNames[name] {
			orphans = append(orphans, name)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		test.Fatalf("%s answers %q, which no input names any more: %s", path, orphans, RecordAgain)
	}
	return byName
}
