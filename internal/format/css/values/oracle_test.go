package values

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/json_value"
	"github.com/system-inc/cohere/internal/testing/recorded"
)

// libraryDirectory holds the comparison's inputs and postcss-values-parser's recorded answers.
var libraryDirectory = filepath.Join("testdata", "postcss_values_parser")

// parseInputs is parse_inputs.json: each input's name, its text, and malformed, which says the library
// refuses it with { loose: true }, the way Prettier runs it, so a recording that refuses a valid input, or
// accepts a malformed one, fails the test rather than quietly agreeing.
type parseInputs struct {
	About  string `json:"about"`
	Inputs []struct {
		Name      string `json:"name"`
		Text      string `json:"text"`
		Malformed bool   `json:"malformed"`
	} `json:"inputs"`
}

// parseAnswer is one answer of parse_answers.json: the library's result with { loose: true } and with
// { loose: false }, with the text it was recorded for.
type parseAnswer struct {
	recorded.RecordedFor
	Loose  oracleResult `json:"loose"`
	Strict oracleResult `json:"strict"`
}

// TestParseAgreesWithPostcssValuesParser is the parser's acceptance test: for every value in
// parse_inputs.json, the Go tree must equal postcss-values-parser's, field for field, under both
// { loose: true } and { loose: false }, and a refusal must be a refusal on both sides with the same
// message.
//
// The library's answers are recorded, so the test runs no node and reads nothing outside this package. They
// are postcss-values-parser 2.0.1's (with an s, the package Prettier's parse-value.js loads, lib/parser.js),
// the version the Prettier fork pins. A node dumps as its own enumerable fields minus parent, sorted; NaN
// dumps as "NaN" and undefined as "<undefined>"; sourceIndex is UTF-16 there, and the Go side converts. A
// throw dumps as String(error). parse_answers.json keeps each tree's recorded.Fingerprint, taken of it as
// canonicalJson writes it, and each message's, and Go's tree or message must have the same.
//
// To add or change an input, edit parse_inputs.json and record the answers again with
// record-postcss-values-parser.mjs, attached to #yah3d69, its usage in its header, writing them as
// parse_answers.json holds them. An answer recorded for another text than its input's fails here (see
// package recorded).
func TestParseAgreesWithPostcssValuesParser(test *testing.T) {
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
			"%s: %v: record them again with record-postcss-values-parser.mjs, attached to #yah3d69",
			answersPath,
			matchError,
		)
	}

	compared, identical, refusedByBoth, different := 0, 0, 0, 0
	fail := func(format string, arguments ...any) {
		different++
		if different <= 25 {
			test.Errorf(format, arguments...)
		}
	}
	for position, input := range inputs.Inputs {
		answer := answers.Answers[answerIndexes[position]]
		label := fmt.Sprintf("%s %q", input.Name, input.Text)
		if refused := answer.Loose.Error != ""; refused != input.Malformed {
			fail("%s: the input says malformed=%v and the library says %s", label, input.Malformed,
				answer.Loose.describe())
			continue
		}
		for _, mode := range []struct {
			name  string
			want  oracleResult
			loose bool
		}{{"loose", answer.Loose, true}, {"strict", answer.Strict, false}} {
			compared++
			got, dumpValuesError := dumpValues(input.Text, Options{Loose: mode.loose})
			switch {
			case mode.want.Error != "" && dumpValuesError != nil:
				thrown, isThrown := asError(dumpValuesError)
				if !isThrown {
					fail(
						"%s %s: both refuse, and Go's refusal is no *Error (the library's message is %s): %v",
						label,
						mode.name,
						mode.want.Error,
						dumpValuesError,
					)
					continue
				}
				if gotFingerprint := recorded.Fingerprint(thrown.Error()); gotFingerprint != mode.want.Error {
					fail(
						"%s %s: both refuse, differently: the library's message is %s, and Go's is %s, %s",
						label,
						mode.name,
						mode.want.Error,
						gotFingerprint,
						thrown.Error(),
					)
					continue
				}
				refusedByBoth++
			case mode.want.Error != "":
				fail(
					"%s %s: the library refuses it (its message is %s) and Go parsed it to %s",
					label,
					mode.name,
					mode.want.Error,
					treeText(got),
				)
			case dumpValuesError != nil:
				fail(
					"%s %s: the library parses it (its tree is %s) and Go refused: %v",
					label,
					mode.name,
					mode.want.Tree,
					dumpValuesError,
				)
			default:
				gotText := treeText(got)
				if gotFingerprint := recorded.Fingerprint(gotText); gotFingerprint != mode.want.Tree {
					fail(
						"%s %s: the library's tree is %s, and Go's is %s, %s",
						label,
						mode.name,
						mode.want.Tree,
						gotFingerprint,
						gotText,
					)
					continue
				}
				identical++
			}
		}
	}
	test.Logf(
		"%d inputs, %d comparisons (loose and strict) against %s, recorded %s: %d identical, %d refused by both "+
			"with the same message, %d different",
		len(inputs.Inputs),
		compared,
		answers.Source,
		answers.Recorded,
		identical,
		refusedByBoth,
		different,
	)
}

// oracleResult is what the oracle script reports for one value in one mode, as recorded.Fingerprint keeps
// it: the fingerprint of the tree for a parse, as canonicalJson writes it, or of String(error) for a throw.
// The test only compares them for equality with Go's, so the fingerprint is all of them it needs: the
// answers can never be recorded again, so a mismatch is a regression of ours, and the failure prints Go's
// side in full.
type oracleResult struct {
	Tree  string `json:"tree"`
	Error string `json:"error"`
}

// describe is the result as a failure message shows it.
func (result oracleResult) describe() string {
	if result.Error != "" {
		return "it throws the message " + result.Error
	}
	return "it parses to the tree " + result.Tree
}

// asError is the first *Error in the chain dumpValuesError unwraps to, and whether there is one.
func asError(dumpValuesError error) (*Error, bool) {
	for current := dumpValuesError; current != nil; {
		if thrown, isThrown := current.(*Error); isThrown {
			return thrown, true
		}
		unwrapper, isUnwrapper := current.(interface{ Unwrap() error })
		if !isUnwrapper {
			return nil, false
		}
		current = unwrapper.Unwrap()
	}
	return nil, false
}

// dumpValues parses one value and renders it the way the oracle script does, as the JSON value the
// script prints, with sourceIndex converted back to UTF-16.
func dumpValues(text string, options Options) (json_value.Value, error) {
	tree, parseError := Parse(text, options)
	if parseError != nil {
		return nil, parseError
	}
	return dumpValuesValue(tree, utf16IndexOf(text)), nil
}

// dumpValuesValue is one of a node's properties as the JSON value the oracle script prints for it.
// Every string the parser builds is decoded from UTF-16, so it is valid UTF-8 and reads in JSON as
// it is. A float64 JSON cannot carry, which json.Marshal would refuse, becomes a description no
// oracle prints, so the comparison still fails on it.
func dumpValuesValue(value estree.Value, toUtf16 func(int) int) json_value.Value {
	switch typed := value.(type) {
	case nil:
		return json_value.String("<undefined>")
	case *estree.Node:
		if typed.Range != [2]int{0, 0} {
			return json_value.String(
				fmt.Sprintf("a node with range %v, which the port leaves at [0, 0]", typed.Range),
			)
		}
		result := json_value.Object{"type": json_value.String(typed.Type())}
		for _, key := range typed.Keys() {
			if key == "sourceIndex" {
				index, isInt := typed.Get(key).(estree.Integer)
				if !isInt {
					result[key] = json_value.String(fmt.Sprintf("a sourceIndex of type %T", typed.Get(key)))
					continue
				}
				result[key] = json_value.Number(strconv.Itoa(toUtf16(int(index))))
				continue
			}
			result[key] = dumpValuesValue(typed.Get(key), toUtf16)
		}
		return result
	case estree.Nodes:
		if typed == nil {
			return json_value.String("a nil list, which no field of the library holds")
		}
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpValuesValue(child, toUtf16)
		}
		return result
	case estree.Record:
		result := json_value.Object{}
		for key, each := range typed {
			result[key] = dumpValuesValue(each, toUtf16)
		}
		return result
	case estree.Number:
		if math.IsNaN(float64(typed)) {
			return json_value.String("NaN")
		}
		if math.IsInf(float64(typed), 0) {
			return json_value.String(fmt.Sprintf("a float64 of %v, which JSON cannot carry", float64(typed)))
		}
		return json_value.Number(strconv.FormatFloat(float64(typed), 'g', -1, 64))
	case estree.String:
		return json_value.String(typed)
	case estree.Boolean:
		return json_value.Boolean(typed)
	case estree.Integer:
		return json_value.Number(strconv.Itoa(int(typed)))
	default:
		return json_value.String(fmt.Sprintf("an unexpected %T", typed))
	}
}

// utf16IndexOf inverts the byte offsets utf16Units assigns, extrapolating one unit per byte past the
// end the way Parser.sourceIndex does.
func utf16IndexOf(text string) func(int) int {
	_, offsets := utf16Units(text)
	byByte := map[int]int{}
	for index, offset := range offsets {
		if _, present := byByte[offset]; !present {
			byByte[offset] = index
		}
	}
	return func(offset int) int {
		if index, present := byByte[offset]; present {
			return index
		}
		if offset > len(text) {
			return len(offsets) - 1 + offset - len(text)
		}
		return -1000000 - offset
	}
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
