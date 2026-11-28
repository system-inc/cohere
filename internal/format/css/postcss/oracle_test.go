package postcss

import (
	"errors"
	"fmt"
	"path/filepath"
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

// parseAnswer is one recorded answer: the recorded.Fingerprint of the tree the library dumped, or of the
// error it refused with, each as canonicalJson writes it, with what it was recorded for, the input's text
// or its Digest (see inputEntries). The tests only compare the two for equality with Go's dump, so the
// fingerprint is all of them they need: the answers can never be recorded again, so a mismatch is a
// regression of ours, which the failure shows in full on our side.
type parseAnswer struct {
	recorded.RecordedFor
	Root  string `json:"root"`
	Error string `json:"error"`
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
	answers, readError := recorded.ReadJson[recorded.Answers[parseAnswer]](path)
	if readError != nil {
		test.Fatal(readError)
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = answer.Entry()
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

// digestedTextLength is the length in bytes from which an answer records its input's Digest rather than its
// text: a digest is 64 bytes, so a shorter text repeated beside its name costs less and reads in review.
const digestedTextLength = 200

// inputEntries is each input as recorded.Match pairs it, by name and what its answer records: the text when
// it is shorter than digestedTextLength, and its Digest from there on.
func inputEntries(inputs []parseInput) []recorded.Entry {
	entries := make([]recorded.Entry, len(inputs))
	for index, input := range inputs {
		entries[index] = recorded.Entry{Name: input.Name, Input: input.Text}
		if len(input.Text) >= digestedTextLength {
			entries[index].Input = recorded.Digest([]byte(input.Text))
		}
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

// describeAnswer is a recorded answer as a failure message shows it.
func describeAnswer(answer parseAnswer) string {
	if answer.Error != "" {
		return "it refuses it with the error " + answer.Error
	}
	return "it parses it to the tree " + answer.Root
}

// compareParse parses text with parse and compares it with the library's recorded answer: Go's tree or
// refusal, as canonicalJson writes it, must have the fingerprint recorded for the library's, so the same
// tree, field for field and in creation order, or the same refusal. It returns Go's dump, the tree or the
// error, so a caller can read what was compared. A difference names both fingerprints and prints Go's dump
// in full, since the library's can never be recorded again and Go's is what to fix; library names the
// library in it.
func compareParse(
	parse func(string) (*estree.Node, error),
	text string,
	want parseAnswer,
	library string,
) (parseVerdict, json_value.Value, string) {
	got, gotError := dumpParseWith(parse, text)
	gotValue, gotWhat := got, "tree"
	if gotError != nil {
		gotValue, gotWhat = gotError, "error"
	}
	gotText, canonicalError := recorded.CanonicalJson(gotValue)
	if canonicalError != nil {
		return verdictDifferent, gotValue, fmt.Sprintf("Go's %s does not encode as JSON: %v", gotWhat, canonicalError)
	}
	gotFingerprint := recorded.Fingerprint(gotText)
	switch {
	case want.Error != "" && gotError != nil:
		if gotFingerprint != want.Error {
			return verdictDifferent, gotValue, fmt.Sprintf(
				"both refuse, differently: %s's error is %s, and Go's is %s, %s",
				library,
				want.Error,
				gotFingerprint,
				gotText,
			)
		}
		return verdictRefusedByBoth, gotValue, ""
	case want.Error != "":
		return verdictDifferent, gotValue, fmt.Sprintf(
			"%s refuses it (its error is %s) and Go parsed it to %s, %s",
			library,
			want.Error,
			gotFingerprint,
			gotText,
		)
	case gotError != nil:
		return verdictDifferent, gotValue, fmt.Sprintf(
			"%s parses it (its tree is %s) and Go refused it with %s, %s",
			library,
			want.Root,
			gotFingerprint,
			gotText,
		)
	}
	if gotFingerprint != want.Root {
		return verdictDifferent, gotValue, fmt.Sprintf(
			"%s's tree is %s, and Go's is %s, %s",
			library,
			want.Root,
			gotFingerprint,
			gotText,
		)
	}
	return verdictIdentical, gotValue, ""
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
// CssSyntaxError's name, reason, positions and input offsets. parse_answers.json keeps each dump's
// recorded.Fingerprint, taken of it as canonicalJson writes it, and its input's text or Digest.
//
// To add or change an input, edit parse_inputs.json and record the answers again with record-postcss.mjs,
// attached to #yah3d69, its usage in its header: it reads the inputs, runs postcss from the fork's
// node_modules under a sandbox with no network, and prints the answers, which are then written as
// parse_answers.json holds them. An answer recorded for another text than its input's fails here (see
// package recorded).
func TestParseAgreesWithPostcss(test *testing.T) {
	test.Parallel()
	inputs := readParseInputs(test, filepath.Join(postcssDirectory, "parse_inputs.json"))
	source, answers := readParseAnswers(
		test,
		filepath.Join(postcssDirectory, "parse_answers.json"),
		"record-postcss.mjs, attached to #yah3d69",
		inputEntries(inputs),
	)
	counts := map[parseVerdict]int{}
	for position, input := range inputs {
		want := answers[position]
		if input.Malformed != (want.Error != "") {
			counts[verdictDifferent]++
			test.Errorf("%s: the input says malformed=%v and postcss says %s", input.Name, input.Malformed,
				describeAnswer(want))
			continue
		}
		verdict, _, difference := compareParse(Parse, input.Text, want, "postcss")
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
