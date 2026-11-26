package graphql

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

// graphqlJsDirectory holds the comparison's inputs and graphql-js's recorded answers for them.
var graphqlJsDirectory = filepath.Join("testdata", "graphql_js")

// parseInputs is parse_inputs.json: each input's name, its text, and malformed, which says which way
// graphql-js must go on it, so a recording that refuses a valid input, or accepts a malformed one, fails
// the test rather than quietly agreeing.
type parseInputs struct {
	About  string `json:"about"`
	Inputs []struct {
		Name      string `json:"name"`
		Text      string `json:"text"`
		Malformed bool   `json:"malformed"`
	} `json:"inputs"`
}

// parseAnswers is parse_answers.json.gz: where the answers came from, and one answer per input, the dumped
// tree and comment list for a parse or the message for a refusal. Each carries the text it was recorded
// for. The dumps stay raw JSON until they are compared.
type parseAnswers struct {
	Source   string `json:"source"`
	Recorded string `json:"recorded"`
	Answers  []struct {
		Name     string          `json:"name"`
		Text     string          `json:"text"`
		Ast      json.RawMessage `json:"ast"`
		Comments json.RawMessage `json:"comments"`
		Error    *string         `json:"error"`
	} `json:"answers"`
}

// TestParseAgreesWithGraphqlJs is the parser's acceptance test: for every input in parse_inputs.json, the
// Go tree and comment list must equal graphql-js's, property for property, and a refusal must be a refusal
// on both sides with the same message.
//
// graphql-js's answers are recorded, so the test runs no node and reads nothing outside this package. They
// are graphql-js 17.0.2's, the version the Prettier fork pins, called the way Prettier's parser-graphql.js
// calls it: parse with experimentalFragmentArguments, then the comment walk over the token list. Its offsets
// are UTF-16; the Go side converts. A field graphql-js wrote as undefined dumps as "<undefined>" and a field
// it never wrote is absent, so property presence is compared too. A refusal is Prettier's createError
// message, graphql-js's message and its first location.
//
// To add or change an input, edit parse_inputs.json and record the answers again with the recorder, which
// is attached to #bahm7k3 (record-graphql-js.mjs, its usage in its header): it reads the inputs, runs
// graphql-js from the fork's node_modules under a sandbox with no network, and prints the answers to gzip
// into parse_answers.json.gz. An answer recorded for another text than its input's fails here, naming the
// recorder (see package recorded).
func TestParseAgreesWithGraphqlJs(test *testing.T) {
	test.Parallel()
	inputsPath := filepath.Join(graphqlJsDirectory, "parse_inputs.json")
	inputs, readInputsError := recorded.ReadJson[parseInputs](inputsPath)
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	answersPath := filepath.Join(graphqlJsDirectory, "parse_answers.json.gz")
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
		test.Fatalf("%s: %v: record them again with the recorder attached to #bahm7k3", answersPath, matchError)
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
		recordedRefused := want.Error != nil
		if input.Malformed != recordedRefused {
			described := "it parses it to " + string(want.Ast)
			if recordedRefused {
				described = "it refuses it: " + *want.Error
			}
			fail("%s: the input says malformed=%v and graphql-js says %s", input.Name, input.Malformed, described)
			continue
		}
		gotAst, gotComments, dumpParseError := dumpParse(input.Text)
		switch {
		case recordedRefused && dumpParseError != nil:
			if dumpParseError.Error() != *want.Error {
				fail(
					"%s: both refuse, with different messages:\n  graphql-js %s\n  Go         %s",
					input.Name,
					*want.Error,
					dumpParseError.Error(),
				)
				continue
			}
			refusedByBoth++
		case recordedRefused:
			fail("%s: graphql-js refuses it (%s) and Go parsed it", input.Name, *want.Error)
		case dumpParseError != nil:
			fail("%s: graphql-js parses it and Go refused: %v", input.Name, dumpParseError)
		default:
			wantAst, wantAstError := json_value.Parse(want.Ast)
			wantComments, wantCommentsError := json_value.Parse(want.Comments)
			if wantAstError != nil || wantCommentsError != nil {
				fail("%s: graphql-js's dump does not read as JSON: %v, %v", input.Name, wantAstError, wantCommentsError)
				continue
			}
			difference := firstParseDifference(wantAst, gotAst, "ast") +
				firstParseDifference(wantComments, gotComments, "comments")
			if difference != "" {
				fail("%s: %s", input.Name, difference)
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

// dumpParse parses one text and renders its tree and its comment list the way the oracle script does,
// as the JSON values the script prints, with offsets converted to UTF-16.
func dumpParse(text string) (json_value.Value, json_value.Value, error) {
	document, comments, parseError := Parse(text)
	if parseError != nil {
		return nil, nil, parseError
	}
	offsets := utf16OffsetsOf(text)
	dumped := make(json_value.Array, len(comments))
	for index, comment := range comments {
		if comment.Type() != "Comment" {
			return nil, nil, fmt.Errorf("a comment of type %q", comment.Type())
		}
		dumped[index] = json_value.Object{
			"loc":   jsonRange(offsets, comment.Range),
			"value": jsonString(comment.String("value")),
		}
	}
	return dumpParseValue(document, offsets), dumped, nil
}

// dumpParseValue is one of a node's properties as the JSON value the oracle script prints for it.
func dumpParseValue(value estree.Value, offsets []int) json_value.Value {
	switch typed := value.(type) {
	case nil:
		return json_value.String("<undefined>")
	case *estree.Node:
		result := json_value.Object{"kind": jsonString(typed.Type()), "loc": jsonRange(offsets, typed.Range)}
		for _, key := range typed.Keys() {
			result[key] = dumpParseValue(typed.Get(key), offsets)
		}
		return result
	case estree.Nodes:
		if typed == nil {
			return json_value.String("a nil list, which no graphql-js field holds")
		}
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpParseValue(child, offsets)
		}
		return result
	case estree.String:
		return jsonString(string(typed))
	case estree.Boolean:
		return json_value.Boolean(typed)
	default:
		return json_value.String(fmt.Sprintf("an unexpected %T", typed))
	}
}

// jsonRange is a byte range as the script's loc, its two ends in UTF-16 units.
func jsonRange(offsets []int, byteRange [2]int) json_value.Array {
	return json_value.Array{
		json_value.Number(strconv.Itoa(offsets[byteRange[0]])),
		json_value.Number(strconv.Itoa(offsets[byteRange[1]])),
	}
}

// jsonString is text as it reads back from JSON. json.Marshal writes each byte that starts no UTF-8
// character as U+FFFD, and converting to runes and back does the same, byte for byte, so the Go side
// compares as it did when it reached the comparison through json.Marshal.
func jsonString(text string) json_value.String {
	return json_value.String(string([]rune(text)))
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

// firstParseDifference names the first path where two JSON values differ, or "".
func firstParseDifference(want json_value.Value, got json_value.Value, path string) string {
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
					"%s.%s: present in graphql-js %v, in Go %v (%v)",
					path,
					key,
					wantPresent,
					gotPresent,
					typed["kind"],
				)
			}
			if difference := firstParseDifference(wantValue, gotValue, path+"."+key); difference != "" {
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
			if difference := firstParseDifference(
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
