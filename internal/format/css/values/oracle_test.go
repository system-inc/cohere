package values

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
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

// parseAnswers is parse_answers.json.gz: where the answers came from, and one answer per input, the
// library's result with { loose: true } and with { loose: false }, with the text it was recorded for.
type parseAnswers struct {
	Source   string `json:"source"`
	Recorded string `json:"recorded"`
	Answers  []struct {
		Name   string       `json:"name"`
		Text   string       `json:"text"`
		Loose  oracleResult `json:"loose"`
		Strict oracleResult `json:"strict"`
	} `json:"answers"`
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
// throw dumps as String(error).
//
// To add or change an input, edit parse_inputs.json and record the answers again with
// record-postcss-values-parser.mjs, attached to #yah3d69, its usage in its header. An answer recorded for
// another text than its input's fails here (see package recorded).
func TestParseAgreesWithPostcssValuesParser(test *testing.T) {
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
		if refused := answer.Loose.Error != nil; refused != input.Malformed {
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
			case mode.want.Error != nil && dumpValuesError != nil:
				if thrown, isThrown := asError(dumpValuesError); !isThrown || thrown.Error() != *mode.want.Error {
					fail(
						"%s %s: both refuse, with different messages:\n  library %s\n  Go      %v",
						label,
						mode.name,
						*mode.want.Error,
						dumpValuesError,
					)
					continue
				}
				refusedByBoth++
			case mode.want.Error != nil:
				fail("%s %s: the library refuses it (%s) and Go parsed it", label, mode.name, *mode.want.Error)
			case dumpValuesError != nil:
				fail("%s %s: the library parses it and Go refused: %v", label, mode.name, dumpValuesError)
			default:
				want, wantError := json_value.Parse(mode.want.Tree)
				if wantError != nil {
					fail("%s %s: the library's tree does not read as JSON: %v", label, mode.name, wantError)
					continue
				}
				if difference := firstValuesDifference(want, got, "root"); difference != "" {
					fail("%s %s: %s", label, mode.name, difference)
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

// oracleResult is what the oracle script reports for one value in one mode: { tree } for a parse, or
// { error } holding String(error) for a throw. The tree stays raw JSON until it is compared.
type oracleResult struct {
	Tree  json.RawMessage `json:"tree"`
	Error *string         `json:"error"`
}

// describe is the result as a failure message shows it.
func (result oracleResult) describe() string {
	if result.Error != nil {
		return "it throws " + *result.Error
	}
	return "it parses to " + string(result.Tree)
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

// firstValuesDifference names the first path where two JSON values differ, or "".
func firstValuesDifference(want json_value.Value, got json_value.Value, path string) string {
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
			if difference := firstValuesDifference(wantValue, gotValue, path+"."+key); difference != "" {
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
			if difference := firstValuesDifference(
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
