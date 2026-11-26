package postcss

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/json_value"
	"github.com/system-inc/cohere/internal/testing/recorded"
)

// postcssScssDirectory holds TestParseScssAgreesWithPostcssScss's inputs and postcss-scss's recorded
// answers for them and for the vendored scss fixtures.
var postcssScssDirectory = filepath.Join("testdata", "postcss_scss")

// vendoredScssDirectory is Prettier's own scss fixtures, vendored for the css package's tests.
var vendoredScssDirectory = filepath.Join("..", "testdata", "prettier", "scss")

// TestParseScssAgreesWithPostcssScss is the scss parser's acceptance test, TestParseAgreesWithPostcss's
// twin: every scss fixture Prettier tests itself with (vendored in ../testdata/prettier/scss), the css-in-js
// templates of Prettier's styled-components and styled-jsx tests (built as Prettier's embed builds them,
// @prettier-placeholder-N-id and all, in template_inputs.json), and every inline input in
// parse_inputs.json must parse to postcss-scss's tree or be refused by both with the same error.
//
// postcss-scss's answers are recorded, so the test runs no node and reads nothing outside this repository.
// They are postcss-scss 4.0.9's, the version the Prettier fork pins, called as Prettier's parseScss calls
// it, scssParse(text, { map: false }), dumped as TestParseAgreesWithPostcss's are. A refusal that is not a
// CssSyntaxError (postcss-scss's TypeError) dumps as notCssSyntaxError, "name: message". An answer for a
// vendored file carries the file's SHA-256, so a changed file fails until it is recorded again.
//
// To add or change an input, or after the vendored fixtures change, record the answers again with
// record-postcss-scss.mjs, attached to #yah3d69, its usage in its header.
func TestParseScssAgreesWithPostcssScss(test *testing.T) {
	test.Parallel()
	const recorder = "record-postcss-scss.mjs, attached to #yah3d69"
	inputs := append(
		readParseInputs(test, filepath.Join(postcssScssDirectory, "parse_inputs.json")),
		readParseInputs(test, filepath.Join(postcssScssDirectory, "template_inputs.json"))...,
	)
	source, answers := readParseAnswers(
		test,
		filepath.Join(postcssScssDirectory, "parse_answers.json.gz"),
		recorder,
		inputEntries(inputs),
	)

	var vendored []parseInput
	var vendoredEntries []recorded.Entry
	walkError := filepath.WalkDir(vendoredScssDirectory, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() || filepath.Ext(path) != ".scss" {
			return pathError
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		if !utf8.Valid(contents) {
			test.Fatalf("%s is not UTF-8, which postcss-scss's answers cannot be compared with", path)
		}
		relative, relativeError := filepath.Rel(vendoredScssDirectory, path)
		if relativeError != nil {
			return relativeError
		}
		vendored = append(vendored, parseInput{Name: filepath.ToSlash(relative), Text: string(contents)})
		vendoredEntries = append(
			vendoredEntries,
			recorded.Entry{Name: filepath.ToSlash(relative), Input: recorded.Digest(contents)},
		)
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	if len(vendored) == 0 {
		test.Fatalf("no .scss files under %s", vendoredScssDirectory)
	}
	_, vendoredAnswers := readParseAnswers(
		test,
		filepath.Join(postcssScssDirectory, "vendored_answers.json.gz"),
		recorder,
		vendoredEntries,
	)

	counts := map[parseVerdict]int{}
	vendoredRefused := 0
	compare := func(input parseInput, want parseAnswer, holdToMalformed bool) {
		if holdToMalformed && input.Malformed != (want.Error != nil) {
			counts[verdictDifferent]++
			test.Errorf("%s: the input says malformed=%v and postcss-scss says %s", input.Name, input.Malformed,
				describeAnswer(want))
			return
		}
		verdict, difference := compareParse(ParseScss, input.Text, want, "postcss-scss")
		counts[verdict]++
		if verdict == verdictDifferent && counts[verdictDifferent] <= 20 {
			test.Errorf("%s: %s", input.Name, difference)
		}
	}
	for position, input := range inputs {
		compare(input, answers[position], true)
	}
	for position, input := range vendored {
		if vendoredAnswers[position].Error != nil {
			vendoredRefused++
		}
		compare(input, vendoredAnswers[position], false)
	}
	test.Logf(
		"%d compared (%d vendored .scss files, %d inputs) against %s: %d identical, %d refused by both with "+
			"the same error, %d different; %d vendored files refused",
		len(vendored)+len(inputs),
		len(vendored),
		len(inputs),
		source,
		counts[verdictIdentical],
		counts[verdictRefusedByBoth],
		counts[verdictDifferent],
		vendoredRefused,
	)
	if vendoredRefused > len(vendored)/10 {
		test.Errorf(
			"postcss-scss refused %d of %d vendored .scss files; the comparison is thinner than it looks",
			vendoredRefused,
			len(vendored),
		)
	}

	// What postcss-scss adds must be in the trees compared, or agreement says nothing about it.
	features := map[string]int{}
	for _, want := range append(answers, vendoredAnswers...) {
		if want.Root != nil {
			root, readError := json_value.Parse(want.Root)
			if readError != nil {
				test.Fatalf("%s: postcss-scss's tree does not read as JSON: %v", want.Name, readError)
			}
			countScssFeatures(root, features)
		} else if want.Error != nil {
			wantError, readError := json_value.Parse(want.Error)
			if readError != nil {
				test.Fatalf("%s: postcss-scss's error does not read as JSON: %v", want.Name, readError)
			}
			// A refusal that is not a CssSyntaxError dumps its message as a string under notCssSyntaxError.
			if object, isObject := wantError.(json_value.Object); isObject {
				if message, present := object["notCssSyntaxError"]; present && message != (json_value.Null{}) {
					features["TypeError refusal"]++
				}
			}
		}
	}
	test.Logf("postcss-scss features compared: %v", features)
	for _, feature := range []string{
		"inline comment",
		"inline comment raws.text",
		"nested declaration",
		"raws.selector.scss",
		"raws.value.scss",
		"raws.params.scss",
		"TypeError refusal",
	} {
		if features[feature] == 0 {
			test.Errorf("no input exercises %s", feature)
		}
	}
}

// countScssFeatures counts, in an oracle dump, the nodes and raws only postcss-scss makes.
//
// Mutates: features the walk adds every feature it finds to the count, so one map gathers a whole corpus.
func countScssFeatures(value json_value.Value, features map[string]int) {
	switch typed := value.(type) {
	case json_value.Array:
		for _, each := range typed {
			countScssFeatures(each, features)
		}
	case json_value.Object:
		raws, _ := typed["raws"].(json_value.Object)
		if typed["type"] == json_value.String("comment") && raws["inline"] == json_value.Boolean(true) {
			features["inline comment"]++
			if _, hasText := raws["text"]; hasText {
				features["inline comment raws.text"]++
			}
		}
		if typed["type"] == json_value.String("decl") && typed["isNested"] == json_value.Boolean(true) {
			features["nested declaration"]++
		}
		for _, field := range []string{"selector", "value", "params"} {
			if raw, isMap := raws[field].(json_value.Object); isMap {
				if _, hasScss := raw["scss"]; hasScss {
					features["raws."+field+".scss"]++
				}
			}
		}
		countScssFeatures(typed["nodes"], features)
	}
}
