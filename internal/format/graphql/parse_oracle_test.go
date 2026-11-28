package graphql

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

// parseAnswer is graphql-js's answer for one input: the recorded.Fingerprint of the dumped tree and of the
// comment list for a parse, each as canonicalJson writes it, or of the message for a refusal, with what it
// was recorded for, the input's text or its Digest (see inputEntries). The test only compares each with Go's
// for equality, so the fingerprint is all of it the test needs: the answers can never be recorded again, so
// a mismatch is a regression of ours, which the failure shows in full on our side.
type parseAnswer struct {
	recorded.RecordedFor
	Ast      string `json:"ast"`
	Comments string `json:"comments"`
	Error    string `json:"error"`
}

// digestedTextLength is the length in bytes past which an answer records its input's Digest rather than its
// text: a digest is 64 bytes, so a shorter text repeated beside its name costs less and reads in review.
const digestedTextLength = 200

// TestParseAgreesWithGraphqlJs is the parser's acceptance test: for every input in parse_inputs.json, the
// Go tree and comment list must equal graphql-js's, property for property, and a refusal must be a refusal
// on both sides with the same message.
//
// graphql-js's answers are recorded, so the test runs no node and reads nothing outside this package. They
// are graphql-js 17.0.2's, the version the Prettier fork pins, called the way Prettier's parser-graphql.js
// calls it: parse with experimentalFragmentArguments, then the comment walk over the token list. Its offsets
// are UTF-16; the Go side converts. A field graphql-js wrote as undefined dumps as "<undefined>" and a field
// it never wrote is absent, so property presence is compared too. A refusal is Prettier's createError
// message, graphql-js's message and its first location. parse_answers.json keeps each dump's and message's
// recorded.Fingerprint, and Go's are compared with them.
//
// To add or change an input, edit parse_inputs.json and record the answers again with the recorder, which
// is attached to #bahm7k3 (record-graphql-js.mjs, its usage in its header): it reads the inputs, runs
// graphql-js from the fork's node_modules under a sandbox with no network, and prints the answers, which
// are then written as parse_answers.json holds them. An answer recorded for another text than its input's
// fails here, naming the recorder (see package recorded).
func TestParseAgreesWithGraphqlJs(test *testing.T) {
	test.Parallel()
	inputsPath := filepath.Join(graphqlJsDirectory, "parse_inputs.json")
	inputs, readInputsError := recorded.ReadJson[parseInputs](inputsPath)
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	answersPath := filepath.Join(graphqlJsDirectory, "parse_answers.json")
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
		if len(input.Text) > digestedTextLength {
			inputEntries[index].Input = recorded.Digest([]byte(input.Text))
		}
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = answer.Entry()
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
		recordedRefused := want.Error != ""
		if input.Malformed != recordedRefused {
			described := "it parses it to the tree " + want.Ast + " and the comments " + want.Comments
			if recordedRefused {
				described = "it refuses it with the message " + want.Error
			}
			fail("%s: the input says malformed=%v and graphql-js says %s", input.Name, input.Malformed, described)
			continue
		}
		gotAst, gotComments, dumpParseError := dumpParse(input.Text)
		switch {
		case recordedRefused && dumpParseError != nil:
			if got := recorded.Fingerprint(dumpParseError.Error()); got != want.Error {
				fail(
					"%s: both refuse, with different messages: graphql-js's is %s, and Go's is %s, %s",
					input.Name,
					want.Error,
					got,
					dumpParseError.Error(),
				)
				continue
			}
			refusedByBoth++
		case recordedRefused:
			fail("%s: graphql-js refuses it (its message is %s) and Go parsed it", input.Name, want.Error)
		case dumpParseError != nil:
			fail("%s: graphql-js parses it and Go refused: %v", input.Name, dumpParseError)
		default:
			gotAstText, gotAstError := recorded.CanonicalJson(gotAst)
			gotCommentsText, gotCommentsError := recorded.CanonicalJson(gotComments)
			if gotAstError != nil || gotCommentsError != nil {
				fail("%s: Go's dump does not encode as JSON: %v, %v", input.Name, gotAstError, gotCommentsError)
				continue
			}
			if got := recorded.Fingerprint(gotAstText); got != want.Ast {
				fail("%s: graphql-js's tree is %s, and Go's is %s, %s", input.Name, want.Ast, got, gotAstText)
				continue
			}
			if got := recorded.Fingerprint(gotCommentsText); got != want.Comments {
				fail(
					"%s: graphql-js's comments are %s, and Go's are %s, %s",
					input.Name,
					want.Comments,
					got,
					gotCommentsText,
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
