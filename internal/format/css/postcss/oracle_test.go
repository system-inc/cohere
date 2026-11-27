package postcss

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/json_value"
	"github.com/system-inc/cohere/internal/testing/recorded"
)

// postcssDirectory holds TestParseAgreesWithPostcss's inputs and postcss's recorded answers for them.
var postcssDirectory = filepath.Join("testdata", "postcss")

// parseInput is one input of a recorded comparison: its name, its text, and malformed, which says whether
// the library refuses the whole text, so a recording that refuses a valid input, or accepts a malformed
// one, fails the test rather than quietly agreeing. From names the Prettier test file an input was taken
// from, where it was.
type parseInput struct {
	Name      string `json:"name"`
	Text      string `json:"text"`
	Malformed bool   `json:"malformed"`
	From      string `json:"from"`
}

// parseInputs is an inputs file, parse_inputs.json.
type parseInputs struct {
	About  string       `json:"about"`
	Inputs []parseInput `json:"inputs"`
}

// parseAnswer is one recorded answer: the tree the library dumped, or the error it refused with, both raw
// JSON until they are compared, with what it was recorded for, the input's text or, for a vendored file,
// its digest.
type parseAnswer struct {
	Name   string          `json:"name"`
	Text   string          `json:"text"`
	Sha256 string          `json:"sha256"`
	Root   json.RawMessage `json:"root"`
	Error  json.RawMessage `json:"error"`
}

// parseAnswers is an answers file: where the answers came from, and the answers.
type parseAnswers struct {
	Source   string        `json:"source"`
	Recorded string        `json:"recorded"`
	Answers  []parseAnswer `json:"answers"`
}

// readParseInputs reads an inputs file, failing the test when it does not read or holds no inputs.
func readParseInputs(test *testing.T, path string) []parseInput {
	test.Helper()
	inputs, readError := recorded.ReadJson[parseInputs](path)
	if readError != nil {
		test.Fatal(readError)
	}
	if len(inputs.Inputs) == 0 {
		test.Fatalf("%s holds no inputs", path)
	}
	return inputs.Inputs
}

// readParseAnswers reads an answers file and returns its source and, for each entry in order, its answer.
// An entry with no answer, or one recorded for another input, fails the test, naming the recorder to run
// again.
func readParseAnswers(test *testing.T, path string, recorder string, entries []recorded.Entry) (string, []parseAnswer) {
	test.Helper()
	answers, readError := recorded.ReadJson[parseAnswers](path)
	if readError != nil {
		test.Fatal(readError)
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = recorded.Entry{Name: answer.Name, Input: answer.Text + answer.Sha256}
	}
	indexes, matchError := recorded.Match(entries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: record them again with %s", path, matchError, recorder)
	}
	aligned := make([]parseAnswer, len(entries))
	for position, index := range indexes {
		aligned[position] = answers.Answers[index]
	}
	return answers.Source + ", recorded " + answers.Recorded, aligned
}

// inputEntries is each input as recorded.Match pairs it, by name and text.
func inputEntries(inputs []parseInput) []recorded.Entry {
	entries := make([]recorded.Entry, len(inputs))
	for index, input := range inputs {
		entries[index] = recorded.Entry{Name: input.Name, Input: input.Text}
	}
	return entries
}

// parseVerdict is how one comparison came out.
type parseVerdict int

const (
	verdictIdentical parseVerdict = iota
	verdictRefusedByBoth
	verdictDifferent
)

// compareParse parses text with parse and compares it with the library's recorded answer: the same tree,
// field for field and in creation order, or the same refusal. A difference is described in the returned
// string; library names the library in it.
func compareParse(
	parse func(string) (*estree.Node, error),
	text string,
	want parseAnswer,
	library string,
) (parseVerdict, string) {
	got, gotError := dumpParseWith(parse, text)
	switch {
	case want.Error != nil && gotError != nil:
		wantError, readError := json_value.Parse(want.Error)
		if readError != nil {
			return verdictDifferent, fmt.Sprintf("%s's error does not read as JSON: %v", library, readError)
		}
		if difference := firstParseDifference(wantError, gotError, "error"); difference != "" {
			return verdictDifferent, "both refuse, differently: " + difference
		}
		return verdictRefusedByBoth, ""
	case want.Error != nil:
		return verdictDifferent, fmt.Sprintf("%s refuses it (%s) and Go parsed it", library, want.Error)
	case gotError != nil:
		return verdictDifferent, fmt.Sprintf("%s parses it and Go refused: %v", library, gotError)
	}
	wantRoot, readError := json_value.Parse(want.Root)
	if readError != nil {
		return verdictDifferent, fmt.Sprintf("%s's tree does not read as JSON: %v", library, readError)
	}
	if difference := firstParseDifference(wantRoot, got, "root"); difference != "" {
		return verdictDifferent, difference
	}
	return verdictIdentical, ""
}

// describeAnswer is a recorded answer as a failure message shows it.
func describeAnswer(answer parseAnswer) string {
	if answer.Error != nil {
		return "it refuses it with " + string(answer.Error)
	}
	return "it parses it to " + string(answer.Root)
}

// TestParseAgreesWithPostcss is the parser's acceptance test: for every input in
// testdata/postcss/parse_inputs.json, the Go tree must equal postcss's, field for field and in creation
// order, and a refusal must be a refusal on both sides with the same CssSyntaxError.
//
// postcss's answers are recorded, so the test runs no node and reads nothing outside this package. They are
// postcss 8.5.16's, the version the Prettier fork pins, called the way Prettier's parser-postcss.js calls
// it, postcssParse.default(text, { map: false }). Every node dumps its type, every own enumerable field but
// parent (source without input) with sorted keys, plus "keys", the field order Object.keys gives, so
// creation order is compared too. Offsets are UTF-16 there; the Go side converts. A refusal dumps the
// CssSyntaxError's name, reason, positions and input offsets.
//
// To add or change an input, edit parse_inputs.json and record the answers again with record-postcss.mjs,
// attached to #yah3d69, its usage in its header: it reads the inputs, runs postcss from the fork's
// node_modules under a sandbox with no network, and prints the answers to gzip into parse_answers.json.gz.
// An answer recorded for another text than its input's fails here (see package recorded).
func TestParseAgreesWithPostcss(test *testing.T) {
	test.Parallel()
	inputs := readParseInputs(test, filepath.Join(postcssDirectory, "parse_inputs.json"))
	source, answers := readParseAnswers(
		test,
		filepath.Join(postcssDirectory, "parse_answers.json.gz"),
		"record-postcss.mjs, attached to #yah3d69",
		inputEntries(inputs),
	)
	counts := map[parseVerdict]int{}
	for position, input := range inputs {
		want := answers[position]
		if input.Malformed != (want.Error != nil) {
			counts[verdictDifferent]++
			test.Errorf("%s: the input says malformed=%v and postcss says %s", input.Name, input.Malformed,
				describeAnswer(want))
			continue
		}
		verdict, difference := compareParse(Parse, input.Text, want, "postcss")
		counts[verdict]++
		if verdict == verdictDifferent && counts[verdictDifferent] <= 20 {
			test.Errorf("%s: %s", input.Name, difference)
		}
	}
	test.Logf(
		"%d compared against %s: %d identical, %d refused by both with the same error, %d different",
		len(inputs),
		source,
		counts[verdictIdentical],
		counts[verdictRefusedByBoth],
		counts[verdictDifferent],
	)
}

// dumpParseWith parses one text with parse, Parse or ParseScss, and renders it the way the recorders dump
// the library's answer, as JSON values: the tree, or a non-nil error dump.
func dumpParseWith(parse func(string) (*estree.Node, error), text string) (json_value.Value, json_value.Value) {
	offsets := utf16OffsetsOf(stripByteOrderMark(text))
	root, parseError := parse(text)
	if parseError != nil {
		var syntaxError *CssSyntaxError
		if !errors.As(parseError, &syntaxError) {
			return nil, json_value.Object{"notCssSyntaxError": json_value.String(parseError.Error())}
		}
		dumped := json_value.Object{
			"name":   jsonString(syntaxError.Name()),
			"reason": jsonString(syntaxError.Reason),
			"line":   jsonInteger(syntaxError.Line),
			"column": jsonInteger(syntaxError.Column),
			"offset": jsonInteger(offsets(syntaxError.Offset)),
		}
		if syntaxError.HasEnd {
			dumped["endLine"] = jsonInteger(syntaxError.EndLine)
			dumped["endColumn"] = jsonInteger(syntaxError.EndColumn)
			dumped["endOffset"] = jsonInteger(offsets(syntaxError.EndOffset))
		}
		return nil, dumped
	}
	return dumpParseValue(root, offsets), nil
}

// jsonString is text as it reads back from JSON: encoding/json writes each byte that is not UTF-8 as
// U+FFFD, and converting to runes and back does the same, so a string compares as the trip through JSON
// the oracle's tree takes would leave it.
func jsonString(text string) json_value.String {
	return json_value.String(string([]rune(text)))
}

// jsonInteger is an int as a JSON number.
func jsonInteger(number int) json_value.Number {
	return json_value.Number(strconv.Itoa(number))
}

// dumpParseValue is one of a node's properties as the JSON value the oracle script prints for it, with
// every offset converted to UTF-16.
func dumpParseValue(value estree.Value, offsets func(int) int) json_value.Value {
	switch typed := value.(type) {
	case nil:
		return json_value.String("<undefined>")
	case *estree.Node:
		result := json_value.Object{"type": jsonString(typed.Type())}
		keys := json_value.Array{}
		for _, key := range typed.Keys() {
			keys = append(keys, jsonString(key))
			result[key] = dumpParseValue(typed.Get(key), offsets)
		}
		result["keys"] = keys
		if source, isMap := typed.Get("source").(estree.Record); isMap {
			start, _ := source["start"].(estree.Record)
			end, hasEnd := source["end"].(estree.Record)
			wantRange := [2]int{0, 0}
			if start != nil && hasEnd {
				startOffset, startIsInt := start["offset"].(estree.Integer)
				endOffset, endIsInt := end["offset"].(estree.Integer)
				if !startIsInt || !endIsInt {
					result["range"] = jsonString(fmt.Sprintf(
						"source offsets %#v and %#v are not both ints",
						start["offset"],
						end["offset"],
					))
					return result
				}
				wantRange = [2]int{int(startOffset), int(endOffset)}
			}
			if typed.Range != wantRange {
				result["range"] = jsonString(fmt.Sprintf("Range %v is not source's %v", typed.Range, wantRange))
			}
		}
		return result
	case estree.Nodes:
		if typed == nil {
			return json_value.String("a nil list, which no postcss field holds")
		}
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpParseValue(child, offsets)
		}
		return result
	case estree.Record:
		result := json_value.Object{}
		for key, each := range typed {
			if key == "offset" {
				if offset, isInt := each.(estree.Integer); isInt {
					result[key] = jsonInteger(offsets(int(offset)))
					continue
				}
			}
			result[key] = dumpParseValue(each, offsets)
		}
		return result
	case estree.String:
		return jsonString(string(typed))
	case estree.Boolean:
		return json_value.Boolean(typed)
	case estree.Integer:
		return jsonInteger(int(typed))
	default:
		return json_value.String(fmt.Sprintf("an unexpected %T", typed))
	}
}

// stripByteOrderMark drops what Input drops, so offsets convert against the text postcss parsed.
func stripByteOrderMark(text string) string {
	if strings.HasPrefix(text, "\xef\xbb\xbf") || strings.HasPrefix(text, "\xef\xbf\xbe") {
		return text[3:]
	}
	return text
}

// utf16OffsetsOf converts a byte offset into the text to its UTF-16 index, an offset past the end
// keeping its distance past the end.
func utf16OffsetsOf(text string) func(int) int {
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
		if character > 0xFFFF {
			index += 2
		} else {
			index++
		}
	}
	offsets[len(text)] = index
	return func(offset int) int {
		if offset > len(text) {
			return index + offset - len(text)
		}
		if offset < 0 {
			return offset
		}
		return offsets[offset]
	}
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
					"%s.%s: present in postcss %v (%v), in Go %v (%v)",
					path,
					key,
					wantPresent,
					wantValue,
					gotPresent,
					gotValue,
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
			return fmt.Sprintf("%s: want %d elements %v, got %v", path, len(typed), typed, got)
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
