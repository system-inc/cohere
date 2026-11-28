package unist

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/testing/recorded"
)

// The parse oracle: yaml-unist-parser 3.2.0's parse(text, { uniqueKeys: false }), from the fork's
// node_modules, with the whole tree written as JSON, root.comments included, every object under its
// identity: an id per object (a comment reached from root.comments and from the node it is attached to
// is one object, one id), its _parent's id, and the keys it has. Where upstream throws, the error's name,
// message and position were written instead.
//
// Upstream's trees are recorded (see package recorded), so no node runs here, and each answer keeps only the
// recorded.Fingerprint of upstream's tree or error as parseDump writes it: every field of every node (type,
// position with offsets in UTF-16 units, value, chomping, indent, markers, name, parameters, every child
// list with null and [] told apart, and the comment slots), each node's identity numbered in the order the
// dump first reaches it, its parent by that number, and which of the fields upstream's keys say the node's
// type has. The test dumps Parse's tree the same way and compares the fingerprints, so the two are the same
// graph field for field: the same node reached twice on one side is the same node reached twice on the
// other, and every Parent is the node upstream's _parent is. Where upstream throws, Parse must fail with the
// same name and message, and for a YAMLSyntaxError the same position. An answer can never be recorded
// again, so a mismatch is a regression of ours, and the failure prints our dump in full.
//
// Each answers file under testdata/yaml_unist_parser holds, per input, the text it was recorded for (or,
// for a vendored input, the text's digest) and the fingerprint of upstream's tree or error. The recorder is
// record-yaml.mjs, attached to #jddr94a, usage in its header; its trees were dumped and fingerprinted once,
// when the answers became plain JSON, each offset upstream gave inside a surrogate pair (one error's end,
// in the generated inputs) read as the pair's start, as unitOffsets.byteOffset reads it.

// yamlUnistParserDirectory holds the inputs and upstream's recorded answers for them.
var yamlUnistParserDirectory = filepath.Join("testdata", "yaml_unist_parser")

// suiteDirectory holds the yaml-test-suite's cases, shared with the other YAML packages.
var suiteDirectory = filepath.Join("..", "testdata", "yaml_test_suite")

// parseAnswer is one recorded answer: the input it was recorded for, and the recorded.Fingerprint of
// upstream's tree or of its error, as parseDump writes them.
type parseAnswer struct {
	recorded.RecordedFor
	Root  string `json:"root"`
	Error string `json:"error"`
}

// readInputs reads inputs files, with the entries their answers are matched by.
func readInputs(test *testing.T, paths ...string) ([]recorded.Input, []recorded.Entry) {
	test.Helper()
	inputs, entries, readInputsError := recorded.ReadInputs(paths...)
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	return inputs, entries
}

// recordedParses reads the answers file at path and pairs it with the inputs' entries, returning upstream's
// answer for each input, in the inputs' order, and the file's source and date.
func recordedParses(test *testing.T, path string, entries []recorded.Entry) ([]parseAnswer, string) {
	test.Helper()
	answers, readJsonError := recorded.ReadJson[recorded.Answers[parseAnswer]](path)
	if readJsonError != nil {
		test.Fatal(readJsonError)
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = answer.Entry()
	}
	indexes, matchError := recorded.Match(entries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: record them again with record-yaml.mjs, attached to #jddr94a", path, matchError)
	}
	results := make([]parseAnswer, len(entries))
	for position, index := range indexes {
		answer := answers.Answers[index]
		if (answer.Root == "") == (answer.Error == "") {
			test.Fatalf("%s: %q holds neither a tree nor an error, or both", path, answer.Name)
		}
		results[position] = answer
	}
	return results, answers.Source + ", recorded " + answers.Recorded
}

// parts are the numbered inputs files of directory named prefix (recorded.Parts), each with its answers
// file under yamlUnistParserDirectory, named answersPrefix and the same number, numbered the same way.
func parts(test *testing.T, directory string, prefix string, answersPrefix string) ([]string, []string) {
	test.Helper()
	paths, partsError := recorded.Parts(directory, prefix)
	if partsError != nil {
		test.Fatal(partsError)
	}
	answersPaths, answersPartsError := recorded.Parts(yamlUnistParserDirectory, answersPrefix)
	if answersPartsError != nil {
		test.Fatal(answersPartsError)
	}
	if len(answersPaths) != len(paths) {
		test.Fatalf(
			"%s holds %d %s*.json parts and %s %d %s*.json parts answering them",
			directory,
			len(paths),
			prefix,
			yamlUnistParserDirectory,
			len(answersPaths),
			answersPrefix,
		)
	}
	return paths, answersPaths
}

// fixtureInputs is fixture_inputs.json: the hand-written fixtures of the deleted parse_test.go, then the
// texts TestOracleCanFail and TestComparisonSeesEveryField compare against.
var fixtureInputs = filepath.Join(yamlUnistParserDirectory, "fixture_inputs.json")

// fixtureAnswers is upstream's answer for each of fixtureInputs.
var fixtureAnswers = filepath.Join(yamlUnistParserDirectory, "fixture_answers.json")

// recordedFixtures is upstream's recorded answer for each fixture's text.
func recordedFixtures(test *testing.T) map[string]parseAnswer {
	test.Helper()
	inputs, entries := readInputs(test, fixtureInputs)
	results, _ := recordedParses(test, fixtureAnswers, entries)
	byText := make(map[string]parseAnswer, len(inputs))
	for index, input := range inputs {
		byText[input.Text] = results[index]
	}
	return byText
}

// recordedFixture is upstream's recorded answer for text, which must be one of the fixtures.
func recordedFixture(test *testing.T, byText map[string]parseAnswer, text string) parseAnswer {
	test.Helper()
	result, answered := byText[text]
	if !answered {
		test.Fatalf("fixture_inputs.json no longer holds %q", text)
	}
	return result
}

// jsonString is the port's string as the oracle's arrived: JSON.stringify escapes a lone surrogate,
// which encoding/json decodes as U+FFFD. The port's strings hold one as WTF-8 (three bytes, ED then A0
// to BF), so those become U+FFFD too; any other invalid byte is left for the comparison to catch.
func jsonString(text string) string {
	if utf8.ValidString(text) {
		return text
	}
	var builder strings.Builder
	for index := 0; index < len(text); {
		if index+2 < len(text) && text[index] == 0xED && text[index+1] >= 0xA0 && text[index+1] <= 0xBF {
			builder.WriteRune(utf8.RuneError)
			index += 3
			continue
		}
		builder.WriteByte(text[index])
		index++
	}
	return builder.String()
}

// unitsOf maps each byte offset of text to the UTF-16 unit upstream counts there: the inverse of
// unitOffsets.byteOffset, which the recorded offsets went through when the tree was compared whole. A byte
// inside a character has no unit, and is -1. An invalid byte is one unit, as newUnitOffsets counts it.
func unitsOf(text string) []int {
	units := make([]int, len(text)+1)
	for index := range units {
		units[index] = -1
	}
	unit := 0
	for index, character := range text {
		units[index] = unit
		unit += utf16.RuneLen(character)
	}
	units[len(text)] = unit
	return units
}

// dumper writes a tree or an error as parseDump does.
type dumper struct {
	text  string
	units []int
	// ids number the nodes in the order the dump first reaches them.
	ids     map[*Node]int
	builder strings.Builder
}

// unit is the byte offset in UTF-16 units: past either end of the text a unit is a byte, as byteOffset
// counts there, and a byte inside a character is written b<offset>, which no unit upstream records reads as.
func (dumper *dumper) unit(offset int) string {
	switch {
	case offset < 0:
		return strconv.Itoa(offset)
	case offset > len(dumper.text):
		return strconv.Itoa(dumper.units[len(dumper.text)] + offset - len(dumper.text))
	case dumper.units[offset] < 0:
		return "b" + strconv.Itoa(offset)
	}
	return strconv.Itoa(dumper.units[offset])
}

func (dumper *dumper) position(position Position) string {
	return fmt.Sprintf(
		"%d:%d@%s-%d:%d@%s",
		position.Start.Line,
		position.Start.Column,
		dumper.unit(position.Start.Offset),
		position.End.Line,
		position.End.Column,
		dumper.unit(position.End.Offset),
	)
}

func (dumper *dumper) line(depth int, text string) {
	dumper.builder.WriteString(strings.Repeat("  ", depth))
	dumper.builder.WriteString(text)
	dumper.builder.WriteByte('\n')
}

// walked are the node's fields the dump walks, in its order.
func walked(node *Node) [][]*Node {
	return [][]*Node{
		node.Children,
		{node.Tag},
		{node.Anchor},
		node.MiddleComments,
		node.LeadingComments,
		{node.IndicatorComment},
		{node.TrailingComment},
		node.EndComments,
		node.Comments,
	}
}

// number gives every node the dump reaches its id, before any is written, so a parent reached later than its
// child is written by its number too.
func (dumper *dumper) number(node *Node) {
	if node == nil {
		return
	}
	if _, seen := dumper.ids[node]; !seen {
		dumper.ids[node] = len(dumper.ids)
	}
	for _, nodes := range walked(node) {
		for _, each := range nodes {
			dumper.number(each)
		}
	}
}

// list writes a child list: nothing for null or no such field, its length and each node for an array.
func (dumper *dumper) list(depth int, name string, nodes []*Node) {
	if nodes == nil {
		return
	}
	dumper.line(depth, fmt.Sprintf("%s: [%d]", name, len(nodes)))
	for index, each := range nodes {
		dumper.node(depth+1, fmt.Sprintf("[%d]", index), each)
	}
}

// slot writes a field holding one node, or nothing for null.
func (dumper *dumper) slot(depth int, name string, node *Node) {
	if node != nil {
		dumper.node(depth, name, node)
	}
}

// node writes the node on one line, then its lists and slots indented under it. The fields flags say which
// of leadingComments, trailingComment and children the node's type has, as upstream's keys say.
func (dumper *dumper) node(depth int, label string, node *Node) {
	if node == nil {
		dumper.line(depth, label+": null")
		return
	}
	parent := "-"
	if node.Parent != nil {
		parent = "?"
		if id, reached := dumper.ids[node.Parent]; reached {
			parent = strconv.Itoa(id)
		}
	}
	fields := []byte("---")
	if hasLeadingCommentsField(node.NodeType) {
		fields[0] = 'l'
	}
	if hasTrailingCommentField(node.NodeType) {
		fields[1] = 't'
	}
	if hasChildrenField(node.NodeType) {
		fields[2] = 'c'
	}
	text := fmt.Sprintf(
		"%s: %s #%d parent=%s fields=%s %s",
		label,
		node.NodeType,
		dumper.ids[node],
		parent,
		fields,
		dumper.position(node.Position),
	)
	for _, field := range []struct{ name, value string }{
		{"value", node.Value},
		{"chomping", node.Chomping},
		{"name", node.Name},
	} {
		if field.value != "" {
			text += " " + field.name + "=" + strconv.Quote(jsonString(field.value))
		}
	}
	if node.Indent != nil {
		text += fmt.Sprintf(" indent=%d", *node.Indent)
	}
	if node.DirectivesEndMarker {
		text += " directivesEndMarker"
	}
	if node.DocumentEndMarker {
		text += " documentEndMarker"
	}
	if node.Parameters != nil {
		quoted := make([]string, len(node.Parameters))
		for index, parameter := range node.Parameters {
			quoted[index] = strconv.Quote(parameter)
		}
		text += " parameters=[" + strings.Join(quoted, " ") + "]"
	}
	dumper.line(depth, text)
	dumper.list(depth+1, "children", node.Children)
	dumper.slot(depth+1, "tag", node.Tag)
	dumper.slot(depth+1, "anchor", node.Anchor)
	dumper.list(depth+1, "middleComments", node.MiddleComments)
	dumper.list(depth+1, "leadingComments", node.LeadingComments)
	dumper.slot(depth+1, "indicatorComment", node.IndicatorComment)
	dumper.slot(depth+1, "trailingComment", node.TrailingComment)
	dumper.list(depth+1, "endComments", node.EndComments)
	dumper.list(depth+1, "comments", node.Comments)
}

// parseDump is a parse of text as its fingerprint is compared: the tree, every node on a line of its own
// with its fields and position, its lists and slots under it, or, when parseError is not nil, the error's
// name and message, and for a YAMLSyntaxError its position. Offsets are in UTF-16 units, as upstream
// recorded them, so a recorded tree was dumped as it came.
func parseDump(text string, root *Node, parseError error) string {
	dumper := &dumper{text: text, units: unitsOf(text), ids: map[*Node]int{}}
	if parseError != nil {
		var syntaxError *SyntaxError
		var thrown *ThrownError
		switch {
		case errors.As(parseError, &syntaxError):
			dumper.line(0, fmt.Sprintf(
				"error YAMLSyntaxError %s %s",
				strconv.Quote(syntaxError.Message),
				dumper.position(syntaxError.Position),
			))
		case errors.As(parseError, &thrown):
			dumper.line(0, fmt.Sprintf("error %s %s", thrown.Name, strconv.Quote(thrown.Message)))
		default:
			dumper.line(0, "error  "+strconv.Quote(parseError.Error()))
		}
		return dumper.builder.String()
	}
	dumper.number(root)
	dumper.node(0, "root", root)
	return dumper.builder.String()
}

// compareParse compares Parse's result for the text with upstream's recorded answer and returns the
// difference, or "" when there is none.
func compareParse(text string, expected parseAnswer) string {
	root, parseError := Parse(text, nil)
	return compareDump(parseDump(text, root, parseError), parseError != nil, expected)
}

// compareDump compares the port's dump, of an error when failed, with upstream's recorded answer by
// fingerprint, and returns the difference, or "" when there is none. A difference prints both fingerprints
// and the port's dump in full, since upstream's can never be recorded again and the port's is what to fix.
func compareDump(dump string, failed bool, expected parseAnswer) string {
	fingerprint := recorded.Fingerprint(dump)
	switch {
	case expected.Error != "" && !failed:
		return fmt.Sprintf(
			"upstream throws (its error is %s) and the port parses it to %s:\n%s",
			expected.Error,
			fingerprint,
			dump,
		)
	case expected.Error == "" && failed:
		return fmt.Sprintf(
			"upstream parses it (its tree is %s) and the port fails with %s:\n%s",
			expected.Root,
			fingerprint,
			dump,
		)
	case failed && fingerprint != expected.Error:
		return fmt.Sprintf(
			"both fail, differently: upstream's error is %s, and the port's is %s:\n%s",
			expected.Error,
			fingerprint,
			dump,
		)
	case !failed && fingerprint != expected.Root:
		return fmt.Sprintf("upstream's tree is %s, and the port's is %s:\n%s", expected.Root, fingerprint, dump)
	}
	return ""
}

// compareAll compares Parse with upstream's recorded answer for every input and reports the first few
// that differ.
func compareAll(
	test *testing.T,
	label string,
	answersPath string,
	inputs []recorded.Input,
	entries []recorded.Entry,
	maximumReported int,
) (failures int, errored int) {
	test.Helper()
	expected, source := recordedParses(test, answersPath, entries)
	for index, input := range inputs {
		if expected[index].Error != "" {
			errored++
		}
		difference := compareParse(input.Text, expected[index])
		if difference == "" {
			continue
		}
		failures++
		if failures <= maximumReported {
			text := fmt.Sprintf("%q", input.Text)
			if len(text) > 200 {
				text = text[:200] + "..."
			}
			test.Errorf("%s %s %s: %s", label, input.Name, text, difference)
		}
	}
	test.Logf("%s: %d inputs against %s", label, len(inputs), source)
	return failures, errored
}
