package cst

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/system-inc/cohere/internal/testing/recorded"
)

// The CST oracle: the fork's own eemeli/yaml, 2.9.0, parsing each input with
// `new Parser(lineCounter.addNewLine).parse(text)` the way yaml-unist-parser does, and every token it
// yields serialized whole. The Go port serializes its tokens to the same JSON, and the two strings must be
// identical: every field of every token at every depth, the fields' presence (an absent end against an
// empty one, an absent key against a null one, an indent a token does not have), every offset, and the
// LineCounter's lineStarts. Both sides sort object keys, so key insertion order, which carries no meaning,
// is the only thing not compared.
//
// An input can also be fed in chunks, parse(chunk, true) per chunk and parse('', false) to finish, which
// drives the lexer's incomplete-input paths (setNext and the buffer it keeps).
//
// Upstream's strings are recorded (see package recorded), so no node runs here: each answers set under
// testdata/eemeli_yaml, plain JSON in numbered parts (recorded.Parts) to keep each file under 50 KB, holds,
// per input and chunk size, what it was recorded for (the text, or its Digest when the text is vendored or
// longer than digestedTextLength) and the recorded.Fingerprint of the string upstream wrote. The test only
// compares that string for equality with the port's, so the fingerprint is all of it the test needs: the
// fork's bundles that answered are gone, so a mismatch is a regression of ours, and the failure prints the
// port's string in full. The recorder is record-yaml.mjs, attached to #jddr94a, usage in its header; its
// answers are then written as the parts hold them.

// eemeliYamlDirectory holds the inputs and upstream's recorded answers for them.
var eemeliYamlDirectory = filepath.Join("testdata", "eemeli_yaml")

// suiteDirectory holds the yaml-test-suite's cases, shared with the other YAML packages.
var suiteDirectory = filepath.Join("..", "testdata", "yaml_test_suite")

// corpusDirectory holds the corpus, Prettier's own format fixtures, shared with the other YAML packages.
var corpusDirectory = filepath.Join("..", "testdata", "prettier_tests")

// oracleInput is one parse: the whole text at once when Chunk is 0, else Chunk code units at a time.
type oracleInput struct {
	Name  string
	Text  string
	Chunk int
	// Recorded is what the parse's answer records of the text: the text, or a vendored text's digest.
	Recorded string
}

// digestedTextLength is the length in bytes past which an answer records its input's Digest rather than its
// text: a digest is 64 bytes, so a shorter text repeated beside its name costs less and reads in review.
const digestedTextLength = 200

// parseKey is a parse's name in an answers file: its input's name and its chunk size.
func parseKey(name string, chunk int) string { return fmt.Sprintf("%s, chunk %d", name, chunk) }

// tokensAnswer is one recorded answer: the input it was recorded for, the chunk size, and the
// recorded.Fingerprint of the string upstream's serializer wrote for it, or of "throw" when parsing threw.
type tokensAnswer struct {
	recorded.RecordedFor
	Chunk  int    `json:"chunk"`
	Output string `json:"output"`
}

// readParses reads an inputs file and parses each of its inputs at each chunk size, each recorded for its
// entry, or for its text's Digest when the text is longer than digestedTextLength.
func readParses(test *testing.T, path string, chunks ...int) []oracleInput {
	test.Helper()
	inputs, entries, readInputsError := recorded.ReadInputs(path)
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	parses := make([]oracleInput, 0, len(inputs)*len(chunks))
	for index, input := range inputs {
		recordedFor := entries[index].Input
		if len(input.Text) > digestedTextLength {
			recordedFor = recorded.Digest([]byte(input.Text))
		}
		for _, chunk := range chunks {
			parses = append(
				parses,
				oracleInput{Name: input.Name, Text: input.Text, Chunk: chunk, Recorded: recordedFor},
			)
		}
	}
	return parses
}

// answerParts are the numbered answers files of testdata/eemeli_yaml named prefix (recorded.Parts).
func answerParts(test *testing.T, prefix string) []string {
	test.Helper()
	paths, partsError := recorded.Parts(eemeliYamlDirectory, prefix)
	if partsError != nil {
		test.Fatal(partsError)
	}
	return paths
}

// parts are the numbered inputs files of directory named prefix (recorded.Parts), each with the answers file
// of the same number among answersPrefix's parts, so an inputs part and its answers part fail unless their
// counts agree.
func parts(test *testing.T, directory string, prefix string, answersPrefix string) ([]string, []string) {
	test.Helper()
	paths, partsError := recorded.Parts(directory, prefix)
	if partsError != nil {
		test.Fatal(partsError)
	}
	answersFiles := answerParts(test, answersPrefix)
	if len(answersFiles) != len(paths) {
		test.Fatalf("%s holds %d %s parts, and %s holds %d %s parts for them", directory, len(paths), prefix,
			eemeliYamlDirectory, len(answersFiles), answersPrefix)
	}
	return paths, answersFiles
}

// recordedOutputs reads the answers files, one set's parts, and pairs their answers with the parses,
// returning the fingerprint of upstream's string for each, in the parses' order, and the files' source and
// date, which every part must give alike.
func recordedOutputs(test *testing.T, answersFiles []string, parses []oracleInput) ([]string, string) {
	test.Helper()
	var answers []tokensAnswer
	source := ""
	for index, path := range answersFiles {
		file, readJsonError := recorded.ReadJson[recorded.Answers[tokensAnswer]](path)
		if readJsonError != nil {
			test.Fatal(readJsonError)
		}
		fileSource := file.Source + ", recorded " + file.Recorded
		if index > 0 && fileSource != source {
			test.Fatalf("%s says its answers are %s, and %s says %s", path, fileSource, answersFiles[0], source)
		}
		source = fileSource
		answers = append(answers, file.Answers...)
	}
	parseEntries := make([]recorded.Entry, len(parses))
	for index, parse := range parses {
		parseEntries[index] = recorded.Entry{Name: parseKey(parse.Name, parse.Chunk), Input: parse.Recorded}
	}
	answerEntries := make([]recorded.Entry, len(answers))
	for index, answer := range answers {
		answerEntries[index] = recorded.Entry{Name: parseKey(answer.Name, answer.Chunk), Input: answer.Entry().Input}
	}
	indexes, matchError := recorded.Match(parseEntries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: record them again with record-yaml.mjs, attached to #jddr94a",
			strings.Join(answersFiles, ", "), matchError)
	}
	outputs := make([]string, len(parses))
	for position, index := range indexes {
		outputs[position] = answers[index].Output
	}
	return outputs, source
}

// sourceUnits is the text as a JavaScript string holds it.
func sourceUnits(text string) []uint16 {
	return utf16.Encode([]rune(text))
}

// portOutput parses like the oracle and serializes like it.
func portOutput(input oracleInput) (output string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			output = "throw"
		}
	}()
	text := sourceUnits(input.Text)
	lineCounter := NewLineCounter()
	parser := NewParser(lineCounter.AddNewLine)
	tokens := []*Token{}
	if input.Chunk == 0 {
		for token := range parser.Parse(text, false) {
			tokens = append(tokens, token)
		}
	} else {
		for index := 0; index < len(text); index += input.Chunk {
			for token := range parser.Parse(slice(text, index, index+input.Chunk), true) {
				tokens = append(tokens, token)
			}
		}
		for token := range parser.Parse([]uint16{}, false) {
			tokens = append(tokens, token)
		}
	}
	var builder strings.Builder
	builder.WriteString(`{"lineStarts":[`)
	for index, offset := range lineCounter.LineStarts {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.Itoa(offset))
	}
	builder.WriteString(`],"tokens":`)
	writeTokenList(&builder, tokens)
	builder.WriteByte('}')
	return builder.String()
}

// The serializer writes what JSON.stringify writes for the canonicalized upstream object: keys sorted by
// code unit, absent fields left out, an absent list (nil) never confused with an empty one.

func writeTokenList(builder *strings.Builder, tokens []*Token) {
	if tokens == nil {
		builder.WriteString("null")
		return
	}
	builder.WriteByte('[')
	for index, token := range tokens {
		if index > 0 {
			builder.WriteByte(',')
		}
		writeToken(builder, token)
	}
	builder.WriteByte(']')
}

type objectWriter struct {
	builder *strings.Builder
	first   bool
}

func (writer *objectWriter) key(name string) {
	if !writer.first {
		writer.builder.WriteByte(',')
	}
	writer.first = false
	writer.builder.WriteString(`"` + name + `":`)
}

func writeToken(builder *strings.Builder, token *Token) {
	if token == nil {
		builder.WriteString("null")
		return
	}
	builder.WriteByte('{')
	writer := &objectWriter{builder: builder, first: true}
	isCollection := token.Type == "block-map" || token.Type == "block-seq" || token.Type == "flow-collection"
	if token.End != nil {
		writer.key("end")
		writeTokenList(builder, token.End)
	}
	if token.HasIndent() {
		writer.key("indent")
		builder.WriteString(strconv.Itoa(token.Indent))
	}
	if isCollection || token.Items != nil {
		writer.key("items")
		if token.Items == nil {
			builder.WriteString("null")
		} else {
			builder.WriteByte('[')
			for index, item := range token.Items {
				if index > 0 {
					builder.WriteByte(',')
				}
				writeItem(builder, item)
			}
			builder.WriteByte(']')
		}
	}
	if token.Type == "error" {
		writer.key("message")
		writeString(builder, sourceUnits(token.Message))
	}
	writer.key("offset")
	builder.WriteString(strconv.Itoa(token.Offset))
	if token.Type == "block-scalar" || token.Properties != nil {
		writer.key("props")
		writeTokenList(builder, token.Properties)
	}
	if token.HasSource() {
		writer.key("source")
		writeString(builder, token.Source)
	}
	if token.Type == "document" {
		writer.key("start")
		writeTokenList(builder, token.Start)
	} else if token.Type == "flow-collection" || token.FlowStart != nil {
		writer.key("start")
		writeToken(builder, token.FlowStart)
	}
	writer.key("type")
	writeString(builder, sourceUnits(token.Type))
	if token.Value != nil {
		writer.key("value")
		writeToken(builder, token.Value)
	}
	builder.WriteByte('}')
}

func writeItem(builder *strings.Builder, item *CollectionItem) {
	builder.WriteByte('{')
	writer := &objectWriter{builder: builder, first: true}
	if item.ExplicitKey {
		writer.key("explicitKey")
		builder.WriteString("true")
	}
	if item.HasKey() {
		writer.key("key")
		writeToken(builder, item.Key)
	} else if item.Key != nil {
		// A key the port holds without upstream's field: shown so the comparison fails.
		writer.key("key!")
		writeToken(builder, item.Key)
	}
	if item.Sep != nil {
		writer.key("sep")
		writeTokenList(builder, item.Sep)
	}
	writer.key("start")
	writeTokenList(builder, item.Start)
	if item.Value != nil {
		writer.key("value")
		writeToken(builder, item.Value)
	}
	builder.WriteByte('}')
}

// writeString is JSON.stringify of a string: the short escapes, other controls as \u00xx, lone
// surrogates as \udxxx, everything else as itself.
func writeString(builder *strings.Builder, units []uint16) {
	builder.WriteByte('"')
	for index := 0; index < len(units); index++ {
		unit := units[index]
		switch {
		case unit == '"':
			builder.WriteString(`\"`)
		case unit == '\\':
			builder.WriteString(`\\`)
		case unit == '\b':
			builder.WriteString(`\b`)
		case unit == '\f':
			builder.WriteString(`\f`)
		case unit == '\n':
			builder.WriteString(`\n`)
		case unit == '\r':
			builder.WriteString(`\r`)
		case unit == '\t':
			builder.WriteString(`\t`)
		case unit < 0x20:
			fmt.Fprintf(builder, `\u%04x`, unit)
		case isHighSurrogate(unit) && index+1 < len(units) && isLowSurrogate(units[index+1]):
			builder.WriteRune(utf16.DecodeRune(rune(unit), rune(units[index+1])))
			index++
		case isHighSurrogate(unit) || isLowSurrogate(unit):
			fmt.Fprintf(builder, `\u%04x`, unit)
		default:
			builder.WriteRune(rune(unit))
		}
	}
	builder.WriteByte('"')
}

// threwFingerprint is what an answer records when upstream's parse threw.
var threwFingerprint = recorded.Fingerprint("throw")

// compareOutputs reports each parse whose port output's fingerprint differs from upstream's recorded one, up
// to limit of them, each naming the input and printing the port's output in full beside both fingerprints.
func compareOutputs(
	test *testing.T,
	label string,
	answersFiles []string,
	parses []oracleInput,
	limit int,
) (failures int) {
	test.Helper()
	expected, source := recordedOutputs(test, answersFiles, parses)
	threw, errorTokens := 0, 0
	for index, input := range parses {
		actual := portOutput(input)
		actualFingerprint := recorded.Fingerprint(actual)
		if actualFingerprint != expected[index] {
			failures++
			if failures <= limit {
				test.Errorf("%s %s %q (chunk %d): upstream's tokens are %s, and the port's are %s:\n%s", label,
					input.Name, truncateInput(input.Text), input.Chunk, expected[index], actualFingerprint, actual)
			}
		}
		// Upstream's string is kept only as its fingerprint, so its error tokens are counted in the port's
		// output once it matched, which makes it the same string.
		if expected[index] == threwFingerprint {
			threw++
		} else if actualFingerprint == expected[index] && strings.Contains(actual, `"type":"error"`) {
			errorTokens++
		}
	}
	// A parse that throws is compared only as having thrown, so say how many did.
	test.Logf("%s: upstream (%s) threw on %d of %d and yielded error tokens on %d",
		label, source, threw, len(parses), errorTokens)
	return failures
}

func truncateInput(input string) string {
	if len(input) > 80 {
		return input[:80] + "…"
	}
	return input
}

// fixtureInputs is fixture_inputs.json: the hand-written fixtures of the deleted fixtures_test.go, then the
// controls TestOracleCanFail compares against.
var fixtureInputs = filepath.Join(eemeliYamlDirectory, "fixture_inputs.json")

// TestTokensMatchUpstream runs every fixture through both sides, whole.
func TestTokensMatchUpstream(test *testing.T) {
	test.Parallel()
	parses := readParses(test, fixtureInputs, 0)
	failures := compareOutputs(test, "fixture", answerParts(test, "fixture_answers_"), parses, 60)
	test.Logf("%d of %d fixtures match upstream", len(parses)-failures, len(parses))
}

// TestChunkedTokensMatchUpstream feeds every fixture in chunks of 1, 2, 3, 5 and 8 code units, which
// splits lines, tokens and surrogate pairs at every point the lexer has to buffer across.
func TestChunkedTokensMatchUpstream(test *testing.T) {
	test.Parallel()
	parses := readParses(test, fixtureInputs, 1, 2, 3, 5, 8)
	failures := compareOutputs(test, "fixture", answerParts(test, "chunked_answers_"), parses, 60)
	test.Logf("%d of %d chunked parses match upstream", len(parses)-failures, len(parses))
}

// TestGeneratedTokensMatchUpstream compares inputs drawn from YAML's indicator characters, spaces,
// tabs, line breaks and a few letters, where the hand-written fixtures cannot reach every combination:
// one fixed seed's first 500, the slice that keeps the inputs and answers under 50 KB.
func TestGeneratedTokensMatchUpstream(test *testing.T) {
	test.Parallel()
	parses := readParses(test, filepath.Join(eemeliYamlDirectory, "generated_inputs.json"), 0)
	failures := compareOutputs(test, "generated", answerParts(test, "generated_answers_"), parses, 40)
	test.Logf("%d of %d generated inputs match upstream", len(parses)-failures, len(parses))
}

// TestOracleCanFail proves the comparison sees a difference: the fingerprint of the tokens of one input
// compared against upstream's for another must differ.
func TestOracleCanFail(test *testing.T) {
	test.Parallel()
	parses := readParses(test, fixtureInputs, 0)
	outputs, _ := recordedOutputs(test, answerParts(test, "fixture_answers_"), parses)
	byText := map[string]string{}
	for index, parse := range parses {
		byText[parse.Text] = outputs[index]
	}
	for _, each := range []struct{ mine, theirs string }{{"a:  b", "a: b"}, {"a: [b,]", "a: [b]"}} {
		expected, answered := byText[each.theirs]
		if !answered {
			test.Fatalf("fixture_inputs.json no longer holds %q", each.theirs)
		}
		if recorded.Fingerprint(portOutput(oracleInput{Text: each.mine})) == expected {
			test.Errorf("the comparison found no difference between the tokens of %s and %s", each.theirs, each.mine)
		}
	}
}

// TestYamlTestSuiteMatchesUpstream compares every case of the yaml-test-suite, whole and in chunks of 3,
// part by part.
func TestYamlTestSuiteMatchesUpstream(test *testing.T) {
	test.Parallel()
	compared, failures := 0, 0
	// The cases' yaml alone, as the deleted test compared; the dumps are the unist and format tests'.
	paths, answersFiles := parts(test, suiteDirectory, "cases_", "suite_answers_")
	for part, path := range paths {
		parses := readParses(test, path, 0, 3)
		failures += compareOutputs(
			test,
			fmt.Sprintf("suite part %d", part+1),
			[]string{answersFiles[part]},
			parses,
			40,
		)
		compared += len(parses)
	}
	test.Logf("%d of %d yaml-test-suite parses (%d cases, whole and chunked) match upstream",
		compared-failures, compared, compared/2)
}

// TestCorpusMatchesUpstream compares every YAML file of the corpus, whole: Prettier's own YAML format
// fixtures but the spec examples the yaml-test-suite holds, half by half.
func TestCorpusMatchesUpstream(test *testing.T) {
	test.Parallel()
	compared, failures := 0, 0
	paths, answersFiles := parts(test, corpusDirectory, "yaml_inputs_", "corpus_answers_")
	for half, path := range paths {
		parses := readParses(test, path, 0)
		failures += compareOutputs(
			test,
			fmt.Sprintf("corpus half %d", half+1),
			[]string{answersFiles[half]},
			parses,
			40,
		)
		compared += len(parses)
	}
	test.Logf("%d of %d files match upstream's tokens", compared-failures, compared)
}
