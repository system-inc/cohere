package media_query

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
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

// parseAnswers is parse_answers.json.gz: where the answers came from, and one answer per input, the tree
// the library dumped or the message it threw, with the text it was recorded for.
type parseAnswers struct {
	Source   string `json:"source"`
	Recorded string `json:"recorded"`
	Answers  []struct {
		Name  string          `json:"name"`
		Text  string          `json:"text"`
		Tree  json.RawMessage `json:"tree"`
		Error *string         `json:"error"`
	} `json:"answers"`
}

// TestParseAgreesWithPostcssMediaQueryParser is the parser's acceptance test: for every params string in
// parse_inputs.json, the Go tree must equal the library's, property for property, and a throw must be a
// refusal here with the same message.
//
// The library's answers are recorded, so the test runs no node and reads nothing outside this package. They
// are postcss-media-query-parser 0.2.3's, the version the Prettier fork pins, loaded as Prettier's
// parser-postcss.js loads it. Each node dumps with its own keys sorted, parent left out; undefined dumps as
// "<undefined>" so presence is compared too. sourceIndex is UTF-16 there; the Go side converts.
//
// To add or change an input, edit parse_inputs.json and record the answers again with
// record-postcss-media-query-parser.mjs, attached to #yah3d69, its usage in its header. An answer recorded
// for another text than its input's fails here (see package recorded).
func TestParseAgreesWithPostcssMediaQueryParser(test *testing.T) {
	test.Parallel()
	inputs, readInputsError := recorded.ReadJson[parseInputs](filepath.Join(libraryDirectory, "parse_inputs.json"))
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	answersPath := filepath.Join(libraryDirectory, "parse_answers.json.gz")
	answers, readAnswersError := recorded.ReadJson[parseAnswers](answersPath)
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
		answerEntries[index] = recorded.Entry{Name: answer.Name, Input: answer.Text}
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
		wantError, recordedRefused := "", want.Error != nil
		if recordedRefused {
			wantError = *want.Error
		}
		if input.Throws != recordedRefused {
			fail(
				"%s: the input says throws=%v and the library says error %q, tree %s",
				input.Name,
				input.Throws,
				wantError,
				want.Tree,
			)
			continue
		}
		got, dumpMediaQueryError := dumpMediaQuery(input.Text)
		switch {
		case recordedRefused && dumpMediaQueryError != nil:
			if dumpMediaQueryError.Error() != wantError {
				fail(
					"%s: both refuse, with different messages:\n  library %s\n  Go      %s",
					input.Name,
					wantError,
					dumpMediaQueryError.Error(),
				)
				continue
			}
			refusedByBoth++
		case recordedRefused:
			fail("%s: the library throws (%s) on %q and Go parsed it", input.Name, wantError, input.Text)
		case dumpMediaQueryError != nil:
			fail("%s: the library parses %q and Go refused: %v", input.Name, input.Text, dumpMediaQueryError)
		default:
			wantTree, treeError := json_value.Parse(want.Tree)
			if treeError != nil {
				fail("%s (%q): the library's tree does not read as JSON: %v", input.Name, input.Text, treeError)
				continue
			}
			if difference := firstMediaQueryDifference(wantTree, got, "tree"); difference != "" {
				fail("%s (%q): %s", input.Name, input.Text, difference)
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

// firstMediaQueryDifference names the first path where two JSON values differ, or "".
func firstMediaQueryDifference(want json_value.Value, got json_value.Value, path string) string {
	switch typed := want.(type) {
	case json_value.Object:
		other, isMap := got.(json_value.Object)
		if !isMap {
			return fmt.Sprintf("%s: want an object, got %v", path, got)
		}
		keys := map[string]bool{}
		for key := range typed {
			keys[key] = true
		}
		for key := range other {
			keys[key] = true
		}
		sorted := make([]string, 0, len(keys))
		for key := range keys {
			sorted = append(sorted, key)
		}
		sort.Strings(sorted)
		for _, key := range sorted {
			wantValue, wantPresent := typed[key]
			gotValue, gotPresent := other[key]
			if wantPresent != gotPresent {
				return fmt.Sprintf(
					"%s.%s: present in the library %v, in Go %v (%v)",
					path,
					key,
					wantPresent,
					gotPresent,
					typed["type"],
				)
			}
			if difference := firstMediaQueryDifference(wantValue, gotValue, path+"."+key); difference != "" {
				return difference
			}
		}
		return ""
	case json_value.Array:
		other, isList := got.(json_value.Array)
		if !isList || len(other) != len(typed) {
			return fmt.Sprintf("%s: want %d elements, got %v", path, len(typed), got)
		}
		for index := range typed {
			if difference := firstMediaQueryDifference(
				typed[index],
				other[index],
				fmt.Sprintf("%s[%d]", path, index),
			); difference != "" {
				return difference
			}
		}
		return ""
	}
	if jsonLeafText(want) != jsonLeafText(got) {
		return fmt.Sprintf("%s: want %s, got %s", path, jsonLeafText(want), jsonLeafText(got))
	}
	return ""
}

// jsonLeafText is a JSON value as Go's untyped decode would hold it, its type and its value, so two
// leaves are equal exactly when they decode to the same thing: a number is read as a float64, so 1
// and 1.0 are one number, as they are in JavaScript.
func jsonLeafText(value json_value.Value) string {
	switch typed := value.(type) {
	case json_value.Number:
		number, parseError := json.Number(typed).Float64()
		if parseError != nil {
			return fmt.Sprintf("a number that does not read as a float64, %q", string(typed))
		}
		return fmt.Sprintf("float64 %v", number)
	case json_value.String:
		return fmt.Sprintf("string %q", string(typed))
	case json_value.Boolean:
		return fmt.Sprintf("bool %v", bool(typed))
	case json_value.Null:
		return "null"
	}
	return fmt.Sprintf("%T %v", value, value)
}
