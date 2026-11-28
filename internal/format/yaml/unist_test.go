package yaml

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/markdown/mdast"
	"github.com/system-inc/cohere/internal/format/yaml/unist"
	"github.com/system-inc/cohere/internal/testing/recorded"
)

// The tree loader: trees yaml-unist-parser 3.2.0 built, loaded for the printer, so the printer is measured
// on the tree upstream's parser built. Each tree was recorded (see package recorded): parse(text, {
// uniqueKeys: false }) with root.comments deleted as parser-yaml.js deletes them, written as JSON. _parent is
// non-enumerable, so JSON left it out and the loader set Parent the way defineParents does. Offsets came in
// UTF-16 units.
//
// Each answer keeps only the recorded.Fingerprint of the tree as treeDump writes it, so the tree loaded is
// the Go parser's, loaded as the recording carried a tree (loadTree), once its dump has the fingerprint
// recorded for upstream's: the same tree, field for field, so the printer reads what it read from upstream's.
// A tree that does not fails the test, naming the input and printing the Go parser's dump in full. Where the
// parser threw, the answer keeps its message, which no test compares, only prints.

// yamlUnistParserDirectory holds the loader's inputs and yaml-unist-parser's recorded trees for them.
var yamlUnistParserDirectory = filepath.Join("testdata", "yaml_unist_parser")

// corpusDirectory holds the corpus, Prettier's own format fixtures, shared with the parser packages.
var corpusDirectory = filepath.Join("testdata", "prettier_tests")

// loaderAnswer is one recorded tree: the input's name, its text or, for a vendored input, the text's
// digest, and the recorded.Fingerprint of the tree, or the parser's message when it threw.
type loaderAnswer struct {
	recorded.RecordedFor
	Root  string  `json:"root"`
	Error *string `json:"error"`
}

// parsed is one input's tree, or the parser's error, or, when the Go parser's tree is not upstream's, the
// difference.
type parsed struct {
	root         *unist.Node
	parseFailure string
	difference   string
}

// frontMatterFile is one of the corpus's markdown files with YAML front matter: the file, its front matter
// as mdast reads it, and the value the front matter embed formats, trimmed as the embed trims it.
type frontMatterFile struct {
	file        recorded.Input
	frontMatter *mdast.FrontMatter
	value       string
}

// readInputs reads inputs files, with the entries their answers are matched by.
func readInputs(test testing.TB, paths ...string) ([]recorded.Input, []recorded.Entry) {
	test.Helper()
	inputs, entries, readInputsError := recorded.ReadInputs(paths...)
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	return inputs, entries
}

// readFrontMatter reads the corpus's markdown files and their front matter. Every one must hold yaml
// front matter for mdast, as it did for Prettier's own parser when the corpus was recorded.
func readFrontMatter(test testing.TB) []frontMatterFile {
	test.Helper()
	files, _ := readInputs(test, filepath.Join(corpusDirectory, "front_matter_inputs.json"))
	frontMatters := make([]frontMatterFile, len(files))
	for index, file := range files {
		frontMatter, _ := mdast.ParseFrontMatter(file.Text)
		if frontMatter == nil || frontMatter.Language != "yaml" {
			test.Fatalf("%s holds no yaml front matter for mdast", file.Name)
		}
		frontMatters[index] = frontMatterFile{file: file, frontMatter: frontMatter, value: trim(frontMatter.Value)}
	}
	return frontMatters
}

// loaderInputs are the inputs loader_answers.json was recorded for, with their entries:
// TestTreeOffsetsSliceTheText's text, then the corpus's front matter values, each under its file's name and
// " (front matter)". The corpus is vendored, so a value's answer records its digest, which also holds
// mdast to reading the value Prettier's own front matter parser read.
func loaderInputs(test testing.TB) ([]recorded.Input, []recorded.Entry) {
	test.Helper()
	inputs, entries := readInputs(test, filepath.Join(yamlUnistParserDirectory, "loader_inputs.json"))
	for _, frontMatter := range readFrontMatter(test) {
		input := recorded.Input{Name: frontMatter.file.Name + " (front matter)", Text: frontMatter.value}
		inputs = append(inputs, input)
		entries = append(entries, recorded.Entry{Name: input.Name, Input: recorded.Digest([]byte(input.Text))})
	}
	return inputs, entries
}

// recordedTrees reads the answers file at path, pairs it with the inputs' entries, and loads the Go parser's
// tree for every input upstream's parser parsed, once its dump has the fingerprint upstream's has, in the
// inputs' order. An input whose tree differs fails the test here, naming it and printing the Go parser's dump.
func recordedTrees(test testing.TB, path string, inputs []recorded.Input, entries []recorded.Entry) []parsed {
	test.Helper()
	answers, readJsonError := recorded.ReadJson[recorded.Answers[loaderAnswer]](path)
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
	results := make([]parsed, len(inputs))
	for index, input := range inputs {
		answer := answers.Answers[indexes[index]]
		switch {
		case (answer.Root == "") == (answer.Error == nil):
			test.Fatalf("%s: %q holds neither a tree nor an error, or both", path, input.Name)
		case answer.Error != nil:
			results[index] = parsed{parseFailure: *answer.Error}
			continue
		}
		root, parseError := unist.Parse(input.Text, nil)
		if parseError != nil {
			results[index] = parsed{difference: fmt.Sprintf(
				"yaml-unist-parser parses it (its tree is %s) and the Go parser fails: %v",
				answer.Root,
				parseError,
			)}
			test.Errorf("%s: %s: %s", path, input.Name, results[index].difference)
			continue
		}
		loaded := loadTree(root, nil)
		dump := treeDump(input.Text, loaded)
		if fingerprint := recorded.Fingerprint(dump); fingerprint != answer.Root {
			results[index] = parsed{difference: fmt.Sprintf(
				"yaml-unist-parser's tree is %s, and the Go parser's is %s:\n%s",
				answer.Root,
				fingerprint,
				dump,
			)}
			test.Errorf("%s: %s: %s", path, input.Name, results[index].difference)
			continue
		}
		results[index] = parsed{root: loaded}
	}
	return results
}

// loadTree is the tree as the recording carried one: the fields the JSON held, with Parent set as
// defineParents does and the root's comments left out, as parser-yaml.js deletes them.
func loadTree(node *unist.Node, parent *unist.Node) *unist.Node {
	if node == nil {
		return nil
	}
	loaded := &unist.Node{
		NodeType:            node.NodeType,
		Position:            node.Position,
		Parent:              parent,
		Value:               node.Value,
		Chomping:            node.Chomping,
		Indent:              node.Indent,
		DirectivesEndMarker: node.DirectivesEndMarker,
		DocumentEndMarker:   node.DocumentEndMarker,
		Name:                node.Name,
		Parameters:          node.Parameters,
	}
	list := func(nodes []*unist.Node) []*unist.Node {
		if nodes == nil {
			return nil
		}
		loadedNodes := make([]*unist.Node, len(nodes))
		for index, child := range nodes {
			loadedNodes[index] = loadTree(child, loaded)
		}
		return loadedNodes
	}
	loaded.Children = list(node.Children)
	loaded.Anchor = loadTree(node.Anchor, loaded)
	loaded.Tag = loadTree(node.Tag, loaded)
	loaded.LeadingComments = list(node.LeadingComments)
	loaded.MiddleComments = list(node.MiddleComments)
	loaded.IndicatorComment = loadTree(node.IndicatorComment, loaded)
	loaded.TrailingComment = loadTree(node.TrailingComment, loaded)
	loaded.EndComments = list(node.EndComments)
	return loaded
}

// unitsOf maps each byte offset of text to the UTF-16 unit upstream counts there, the inverse of the
// conversion the loader made from units to bytes. A byte inside a character has no unit, and is -1. When the
// answers were fingerprinted, an offset upstream gave inside a surrogate pair was read as the pair's start, as
// that conversion read it; none of these trees holds one.
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

// treeDumper writes a loaded tree as treeDump does.
type treeDumper struct {
	text    string
	units   []int
	builder strings.Builder
}

// unit is the byte offset in UTF-16 units, or b<offset> for one inside a character or outside the text,
// which no unit upstream recorded reads as.
func (dumper *treeDumper) unit(offset int) string {
	if offset < 0 || offset > len(dumper.text) || dumper.units[offset] < 0 {
		return "b" + strconv.Itoa(offset)
	}
	return strconv.Itoa(dumper.units[offset])
}

func (dumper *treeDumper) line(depth int, text string) {
	dumper.builder.WriteString(strings.Repeat("  ", depth))
	dumper.builder.WriteString(text)
	dumper.builder.WriteByte('\n')
}

// list writes a child list: nothing for null or no such field, its length and each node for an array.
func (dumper *treeDumper) list(depth int, name string, nodes []*unist.Node) {
	if nodes == nil {
		return
	}
	dumper.line(depth, fmt.Sprintf("%s: [%d]", name, len(nodes)))
	for index, each := range nodes {
		dumper.node(depth+1, fmt.Sprintf("[%d]", index), each)
	}
}

// slot writes a field holding one node, or nothing for null.
func (dumper *treeDumper) slot(depth int, name string, node *unist.Node) {
	if node != nil {
		dumper.node(depth, name, node)
	}
}

// node writes the node on one line, then its lists and slots indented under it, in loadTree's order.
func (dumper *treeDumper) node(depth int, label string, node *unist.Node) {
	if node == nil {
		dumper.line(depth, label+": null")
		return
	}
	text := fmt.Sprintf(
		"%s: %s %d:%d@%s-%d:%d@%s",
		label,
		node.NodeType,
		node.Position.Start.Line,
		node.Position.Start.Column,
		dumper.unit(node.Position.Start.Offset),
		node.Position.End.Line,
		node.Position.End.Column,
		dumper.unit(node.Position.End.Offset),
	)
	for _, field := range []struct{ name, value string }{
		{"value", node.Value},
		{"chomping", node.Chomping},
		{"name", node.Name},
	} {
		if field.value != "" {
			text += " " + field.name + "=" + strconv.Quote(field.value)
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
	dumper.slot(depth+1, "anchor", node.Anchor)
	dumper.slot(depth+1, "tag", node.Tag)
	dumper.list(depth+1, "leadingComments", node.LeadingComments)
	dumper.list(depth+1, "middleComments", node.MiddleComments)
	dumper.slot(depth+1, "indicatorComment", node.IndicatorComment)
	dumper.slot(depth+1, "trailingComment", node.TrailingComment)
	dumper.list(depth+1, "endComments", node.EndComments)
}

// treeDump is a loaded tree of text as its fingerprint is compared: every node on a line of its own with its
// type, position and the fields loadTree carries, its lists and slots under it, null told apart from an
// empty list. Offsets are in UTF-16 units, as upstream recorded them, so a recorded tree was dumped as it came.
func treeDump(text string, root *unist.Node) string {
	dumper := &treeDumper{text: text, units: unitsOf(text)}
	dumper.node(0, "root", root)
	return dumper.builder.String()
}

// TestTreeOffsetsSliceTheText checks the loaded tree's offsets on astral and multi-byte text, since every
// position the printer slices by is one: its dump, offsets in UTF-16 units, has upstream's fingerprint, and
// its byte offsets slice each scalar out of the text.
func TestTreeOffsetsSliceTheText(test *testing.T) {
	test.Parallel()
	inputs, entries := loaderInputs(test)
	trees := recordedTrees(test, filepath.Join(yamlUnistParserDirectory, "loader_answers.json"), inputs, entries)
	text := inputs[0].Text
	if text != "é: 🍐\n'中': \"x\"\n" {
		test.Fatalf("loader_inputs.json's first text is %q, not the astral and multi-byte one", text)
	}
	if trees[0].parseFailure != "" || trees[0].difference != "" {
		test.Fatal(trees[0].parseFailure, trees[0].difference)
	}
	var values []string
	var walk func(node *unist.Node)
	walk = func(node *unist.Node) {
		switch node.NodeType {
		case "plain", "quoteSingle", "quoteDouble":
			values = append(values, text[node.Position.Start.Offset:node.Position.End.Offset])
		}
		for _, child := range node.Children {
			if child.Parent != node {
				test.Errorf("%s's parent is not set", child.NodeType)
			}
			walk(child)
		}
	}
	walk(trees[0].root)
	expected := []string{"é", "🍐", "'中'", "\"x\""}
	if strings.Join(values, "|") != strings.Join(expected, "|") {
		test.Errorf("scalars sliced by the loaded positions are %q, want %q", values, expected)
	}
	if !utf8.ValidString(text) {
		test.Fatal("the fixture is not UTF-8")
	}
}
