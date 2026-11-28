package media_query

import (
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/json_value"
	"github.com/system-inc/cohere/internal/testing/recorded"
)

// libraryDirectory holds the comparison's inputs and postcss-media-query-parser's recorded answers.
var libraryDirectory = filepath.Join("testdata", "postcss_media_query_parser")

// parseInputs is parse_inputs.json: each input's name, its params text, and throws, which says the library
// throws on it, so a recording that throws on an input meant to parse, or parses one meant to throw, fails
// the test rather than quietly agreeing.
type parseInputs struct {
	About  string `json:"about"`
	Inputs []struct {
		Name   string `json:"name"`
		Text   string `json:"text"`
		Throws bool   `json:"throws"`
	} `json:"inputs"`
}

// parseAnswer is one recorded answer: the recorded.Fingerprint of the tree the library dumped, as
// canonicalJson writes it, or of the message it threw, with the text it was recorded for. The test only
// compares them for equality with Go's, so the fingerprint is all of them it needs: the answers can never be
// recorded again, so a mismatch is a regression of ours, and the failure prints Go's side in full.
type parseAnswer struct {
	recorded.RecordedFor
	Tree  string `json:"tree"`
	Error string `json:"error"`
}

// describe is the recorded answer as a failure message shows it.
func (answer parseAnswer) describe() string {
	if answer.Error != "" {
		return "it throws the message " + answer.Error
	}
	return "it parses it to the tree " + answer.Tree
}

// TestParseAgreesWithPostcssMediaQueryParser is the parser's acceptance test: for every params string in
// parse_inputs.json, the Go tree must equal the library's, property for property, and a throw must be a
// refusal here with the same message.
//
// The library's answers are recorded, so the test runs no node and reads nothing outside this package. They
// are postcss-media-query-parser 0.2.3's, the version the Prettier fork pins, loaded as Prettier's
// parser-postcss.js loads it. Each node dumps with its own keys sorted, parent left out; undefined dumps as
// "<undefined>" so presence is compared too. sourceIndex is UTF-16 there; the Go side converts.
// parse_answers.json keeps each tree's recorded.Fingerprint, taken of it as canonicalJson writes it, and
// each message's, and Go's tree or message must have the same.
//
// To add or change an input, edit parse_inputs.json and record the answers again with
// record-postcss-media-query-parser.mjs, attached to #yah3d69, its usage in its header, writing them as
// parse_answers.json holds them. An answer recorded for another text than its input's fails here (see
// package recorded).
func TestParseAgreesWithPostcssMediaQueryParser(test *testing.T) {
	test.Parallel()
	inputs, readInputsError := recorded.ReadJson[parseInputs](filepath.Join(libraryDirectory, "parse_inputs.json"))
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	answersPath := filepath.Join(libraryDirectory, "parse_answers.json")
	answers, readAnswersError := recorded.ReadJson[recorded.Answers[parseAnswer]](answersPath)
	if readAnswersError != nil {
		test.Fatal(readAnswersError)
	}
	if len(inputs.Inputs) == 0 {
		test.Fatal("parse_inputs.json holds no inputs")
	}
	inputEntries := make([]recorded.Entry, len(inputs.Inputs))
	for index, input := range inputs.Inputs {
		inputEntries[index] = recorded.Entry{Name: input.Name, Input: input.Text}
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = answer.Entry()
	}
	answerIndexes, matchError := recorded.Match(inputEntries, answerEntries)
	if matchError != nil {
		test.Fatalf(
			"%s: %v: record them again with record-postcss-media-query-parser.mjs, attached to #yah3d69",
			answersPath,
			matchError,
		)
	}

	identical, refusedByBoth, different := 0, 0, 0
	fail := func(format string, arguments ...any) {
		different++
		if different <= 20 {
			test.Errorf(format, arguments...)
		}
	}
	for position, input := range inputs.Inputs {
		want := answers.Answers[answerIndexes[position]]
		recordedRefused := want.Error != ""
		if input.Throws != recordedRefused {
			fail("%s: the input says throws=%v and the library says %s", input.Name, input.Throws, want.describe())
			continue
		}
		got, dumpMediaQueryError := dumpMediaQuery(input.Text)
		switch {
		case recordedRefused && dumpMediaQueryError != nil:
			if gotFingerprint := recorded.Fingerprint(dumpMediaQueryError.Error()); gotFingerprint != want.Error {
				fail(
					"%s (%q): both refuse, differently: the library's message is %s, and Go's is %s, %s",
					input.Name,
					input.Text,
					want.Error,
					gotFingerprint,
					dumpMediaQueryError.Error(),
				)
				continue
			}
			refusedByBoth++
		case recordedRefused:
			fail(
				"%s (%q): the library throws (its message is %s) and Go parsed it to %s",
				input.Name,
				input.Text,
				want.Error,
				treeText(got),
			)
		case dumpMediaQueryError != nil:
			fail(
				"%s (%q): the library parses it (its tree is %s) and Go refused: %v",
				input.Name,
				input.Text,
				want.Tree,
				dumpMediaQueryError,
			)
		default:
			gotText := treeText(got)
			if gotFingerprint := recorded.Fingerprint(gotText); gotFingerprint != want.Tree {
				fail(
					"%s (%q): the library's tree is %s, and Go's is %s, %s",
					input.Name,
					input.Text,
					want.Tree,
					gotFingerprint,
					gotText,
				)
				continue
			}
			identical++
		}
	}
	test.Logf(
		"%d compared against %s, recorded %s: %d identical, %d refused by both with the same message, %d different",
		len(inputs.Inputs),
		answers.Source,
		answers.Recorded,
		identical,
		refusedByBoth,
		different,
	)
}

// treeText is Go's tree as canonicalJson writes it, the text its fingerprint is taken of, or why it does not
// encode, which no recorded fingerprint matches.
func treeText(tree json_value.Value) string {
	text, canonicalError := recorded.CanonicalJson(tree)
	if canonicalError != nil {
		return fmt.Sprintf("a tree that does not encode as JSON: %v", canonicalError)
	}
	return text
}

// dumpMediaQuery parses params and renders the tree the way the oracle script does, as the JSON value
// the script prints.
func dumpMediaQuery(parameters string) (json_value.Value, error) {
	tree, parseError := Parse(parameters)
	if parseError != nil {
		return nil, parseError
	}
	return dumpMediaQueryValue(tree, utf16OffsetsOf(parameters)), nil
}

// dumpMediaQueryValue is one value of the tree as JSON. Every string the parser builds is a piece of
// params cut at an ASCII character or around whitespace, and params come from the oracle's JSON, so
// each is valid UTF-8 and reads in JSON as it is.
func dumpMediaQueryValue(value estree.Value, offsets []int) json_value.Value {
	switch typed := value.(type) {
	case nil:
		return json_value.String("<undefined>")
	case *estree.Node:
		result := json_value.Object{"type": json_value.String(typed.Type())}
		if typed.Type() == "" {
			result["type"] = json_value.String("<undefined>")
		}
		if typed.Range != [2]int{0, 0} {
			result["range"] = json_value.String("a range other than [0, 0], which the library has no use for")
		}
		for _, key := range typed.Keys() {
			result[key] = dumpMediaQueryValue(typed.Get(key), offsets)
		}
		return result
	case estree.Nodes:
		if typed == nil {
			return json_value.String("a nil list, which no library field holds")
		}
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpMediaQueryValue(child, offsets)
		}
		return result
	case estree.Integer:
		// Only sourceIndex is an Integer: a byte offset here, a UTF-16 index there.
		if typed < 0 || int(typed) >= len(offsets) {
			return json_value.String(fmt.Sprintf("an offset %d outside the input", typed))
		}
		return json_value.Number(strconv.Itoa(offsets[typed]))
	case estree.String:
		return json_value.String(typed)
	case estree.Boolean:
		return json_value.Boolean(typed)
	default:
		return json_value.String(fmt.Sprintf("an unexpected %T", typed))
	}
}

// utf16OffsetsOf maps every byte offset that starts a character (and the end) to its UTF-16 index.
func utf16OffsetsOf(text string) []int {
	offsets := make([]int, len(text)+1)
	index := 0
	for position, character := range text {
		size := utf8.RuneLen(character)
		if character == utf8.RuneError {
			size = 1
		}
		for byteIndex := position; byteIndex < position+size; byteIndex++ {
			offsets[byteIndex] = index
		}
		index += len(utf16.Encode([]rune{character}))
	}
	offsets[len(text)] = index
	return offsets
}
