package yaml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/yaml/unist"
	"github.com/system-inc/cohere/internal/format/yaml/upstream_testing"
)

// The tree loader: trees yaml-unist-parser 3.2.0 built, loaded for the printer, so the printer is measured
// apart from the Go parser. Each tree is recorded (see upstream_testing): parse(text, { uniqueKeys: false })
// with root.comments deleted as parser-yaml.js deletes them, written as JSON. _parent is non-enumerable,
// so JSON leaves it out and the loader sets Parent the way defineParents does. Offsets come back in
// UTF-16 units and are converted to bytes against the text, the conversion layer 3 does once.

// yamlUnistParserDirectory holds the loader's inputs and yaml-unist-parser's recorded trees for them.
var yamlUnistParserDirectory = filepath.Join("testdata", "yaml_unist_parser")

// corpusDirectory holds the corpus, Prettier's own format fixtures, shared with the parser packages.
var corpusDirectory = filepath.Join("testdata", "prettier_tests")

// jsonPoint and jsonNode are the tree as it was recorded. Unknown fields fail the decode, so a field the
// parser adds that unist.Node does not carry is noticed rather than dropped.
type jsonPoint struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

type jsonNode struct {
	Type     string `json:"type"`
	Position struct {
		Start jsonPoint `json:"start"`
		End   jsonPoint `json:"end"`
	} `json:"position"`
	Children            []*jsonNode `json:"children"`
	Value               string      `json:"value"`
	Tag                 *jsonNode   `json:"tag"`
	Anchor              *jsonNode   `json:"anchor"`
	MiddleComments      []*jsonNode `json:"middleComments"`
	LeadingComments     []*jsonNode `json:"leadingComments"`
	TrailingComment     *jsonNode   `json:"trailingComment"`
	EndComments         []*jsonNode `json:"endComments"`
	Chomping            string      `json:"chomping"`
	Indent              *int        `json:"indent"`
	IndicatorComment    *jsonNode   `json:"indicatorComment"`
	DirectivesEndMarker bool        `json:"directivesEndMarker"`
	DocumentEndMarker   bool        `json:"documentEndMarker"`
	Name                string      `json:"name"`
	Parameters          []string    `json:"parameters"`
}

// loaderAnswer is one recorded tree: the input's name, its text or, for a vendored input, the text's
// digest, and the tree, or the parser's message when it threw.
type loaderAnswer struct {
	Name   string          `json:"name"`
	Text   string          `json:"text"`
	Sha256 string          `json:"sha256"`
	Root   json.RawMessage `json:"root"`
	Error  *string         `json:"error"`
}

// parsed is one input's tree, or the parser's error.
type parsed struct {
	root         *unist.Node
	parseFailure string
}

// loaderInputs are the inputs loader_answers.json.gz was recorded for: TestLoaderConvertsOffsets's text,
// then the corpus's front matter values.
func loaderInputs(test testing.TB) []upstream_testing.Input {
	test.Helper()
	inputs := upstream_testing.ReadInputs(test, filepath.Join(yamlUnistParserDirectory, "loader_inputs.json"))
	frontMatter := upstream_testing.ReadFrontMatter(test, corpusDirectory)
	return append(inputs, upstream_testing.FrontMatterValues(frontMatter)...)
}

// recordedTrees reads an answers file, pairs it with the inputs, and loads every tree, in the inputs'
// order.
func recordedTrees(test testing.TB, answersFile string, inputs []upstream_testing.Input) []parsed {
	test.Helper()
	path := filepath.Join(yamlUnistParserDirectory, answersFile)
	answers := upstream_testing.ReadAnswers[loaderAnswer](test, path)
	byName := upstream_testing.Match(test, path, inputs, answers.Answers, func(answer loaderAnswer) (string, string) {
		return answer.Name, upstream_testing.RecordedFor(answer.Text, answer.Sha256)
	})
	results := make([]parsed, len(inputs))
	for index, input := range inputs {
		answer := byName[input.Name]
		switch {
		case (answer.Root == nil) == (answer.Error == nil):
			test.Fatalf("%s: %q holds neither a tree nor an error, or both", path, input.Name)
		case answer.Error != nil:
			results[index] = parsed{parseFailure: *answer.Error}
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(answer.Root))
		decoder.DisallowUnknownFields()
		var root jsonNode
		if decodeError := decoder.Decode(&root); decodeError != nil {
			test.Fatalf("decoding the tree of %q: %v", input.Text, decodeError)
		}
		results[index] = parsed{root: loadTree(&root, newUnitOffsets(input.Text), nil)}
	}
	return results
}

// unitOffsets maps a UTF-16 unit index to its byte offset in the text.
type unitOffsets []int

func newUnitOffsets(text string) unitOffsets {
	offsets := make(unitOffsets, 0, len(text)+1)
	for index, character := range text {
		offsets = append(offsets, index)
		if character > 0xFFFF {
			// The low surrogate of a pair sits at the pair's start too; no position upstream records
			// falls between the two units of one character.
			offsets = append(offsets, index)
		}
	}
	return append(offsets, len(text))
}

func (offsets unitOffsets) byteOffset(unit int) int {
	if unit < 0 || unit >= len(offsets) {
		panic(fmt.Sprintf("offset %d is outside the text's %d units", unit, len(offsets)-1))
	}
	return offsets[unit]
}

// loadTree converts the JSON tree, setting Parent as defineParents does.
func loadTree(node *jsonNode, offsets unitOffsets, parent *unist.Node) *unist.Node {
	if node == nil {
		return nil
	}
	loaded := &unist.Node{
		NodeType: node.Type,
		Position: unist.Position{
			Start: unist.Point{
				Line:   node.Position.Start.Line,
				Column: node.Position.Start.Column,
				Offset: offsets.byteOffset(node.Position.Start.Offset),
			},
			End: unist.Point{
				Line:   node.Position.End.Line,
				Column: node.Position.End.Column,
				Offset: offsets.byteOffset(node.Position.End.Offset),
			},
		},
		Parent:              parent,
		Value:               node.Value,
		Chomping:            node.Chomping,
		Indent:              node.Indent,
		DirectivesEndMarker: node.DirectivesEndMarker,
		DocumentEndMarker:   node.DocumentEndMarker,
		Name:                node.Name,
		Parameters:          node.Parameters,
	}
	list := func(nodes []*jsonNode) []*unist.Node {
		if nodes == nil {
			return nil
		}
		loadedNodes := make([]*unist.Node, len(nodes))
		for index, child := range nodes {
			loadedNodes[index] = loadTree(child, offsets, loaded)
		}
		return loadedNodes
	}
	loaded.Children = list(node.Children)
	loaded.Anchor = loadTree(node.Anchor, offsets, loaded)
	loaded.Tag = loadTree(node.Tag, offsets, loaded)
	loaded.LeadingComments = list(node.LeadingComments)
	loaded.MiddleComments = list(node.MiddleComments)
	loaded.IndicatorComment = loadTree(node.IndicatorComment, offsets, loaded)
	loaded.TrailingComment = loadTree(node.TrailingComment, offsets, loaded)
	loaded.EndComments = list(node.EndComments)
	return loaded
}

// TestLoaderConvertsOffsets checks the loader on astral and multi-byte text, since every position the
// printer slices by goes through it.
func TestLoaderConvertsOffsets(test *testing.T) {
	test.Parallel()
	inputs := loaderInputs(test)
	trees := recordedTrees(test, "loader_answers.json.gz", inputs)
	text := inputs[0].Text
	if text != "é: 🍐\n'中': \"x\"\n" {
		test.Fatalf("loader_inputs.json's first text is %q, not the astral and multi-byte one", text)
	}
	if trees[0].parseFailure != "" {
		test.Fatal(trees[0].parseFailure)
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
