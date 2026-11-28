package unist

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/format/markdown/mdast"
	ecmascripttext "github.com/system-inc/cohere/internal/lint/ecmascript/text"
	"github.com/system-inc/cohere/internal/testing/recorded"
)

// TestParseMatchesUpstream compares every fixture: the hand-written fixtures of the deleted parse_test.go,
// which reach every transform and every branch of attach and updatePositions (each node type with and
// without tag, anchor and comments around it; comments in every slot; documents with and without markers,
// directives and trailing comments; the !!pairs and !!omap shapes upstream fails on; and the edges: end of
// input in each place, \r\n, tabs, non-ASCII and astral characters), then the control texts.
func TestParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	inputs, entries := readInputs(test, fixtureInputs)
	failures, errored := compareAll(test, "fixture", fixtureAnswers, inputs, entries, 40)
	test.Logf("%d of %d fixtures match upstream (%d of them errors)", len(inputs)-failures, len(inputs), errored)
}

// TestGeneratedParseMatchesUpstream compares inputs drawn from YAML's indicators, properties, scalar
// styles, spaces, tabs, line breaks, comments and a few letters and non-ASCII characters: one fixed seed's
// first 500, the slice that keeps the inputs and answers under 50 KB.
func TestGeneratedParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	inputs, entries := readInputs(test, filepath.Join(yamlUnistParserDirectory, "generated_inputs.json"))
	failures, errored := compareAll(
		test,
		"generated",
		filepath.Join(yamlUnistParserDirectory, "generated_answers.json"),
		inputs,
		entries,
		30,
	)
	test.Logf(
		"%d of %d generated inputs match upstream (%d of them errors)",
		len(inputs)-failures,
		len(inputs),
		errored,
	)
}

// TestOracleCanFail proves the comparison sees a difference: the port's tree for one text against
// upstream's for another, where the trees differ in a single position, value, comment slot or error.
func TestOracleCanFail(test *testing.T) {
	test.Parallel()
	answers := recordedFixtures(test)
	cases := []struct{ mine, theirs string }{
		{"a:  b", "a: b"},         // a position
		{"a: c", "a: b"},          // a value
		{"a: b\n# c", "a: b # c"}, // a comment's slot
		{"a: [b", "a: {b"},        // an error's message
		{"- a\n- b", "- a\n- b\n"},
	}
	for _, each := range cases {
		if difference := compareParse(each.mine, recordedFixture(test, answers, each.theirs)); difference == "" {
			test.Errorf("the comparison found no difference between %q and %q", each.mine, each.theirs)
		}
	}
}

// TestYamlTestSuiteParseMatchesUpstream compares every input of the yaml-test-suite, and each case's
// canonical dump, part by part.
func TestYamlTestSuiteParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	compared, failures, errored := 0, 0, 0
	paths, answersPaths := parts(test, suiteDirectory, "cases_", "suite_answers_")
	for part, path := range paths {
		// A part's dumps sit in dumps_<n>.json beside its cases, and are answered with them.
		dumps := filepath.Join(suiteDirectory, fmt.Sprintf("dumps_%d.json", part+1))
		inputs, entries := readInputs(test, path, dumps)
		partFailures, partErrored := compareAll(
			test,
			fmt.Sprintf("suite part %d", part+1),
			answersPaths[part],
			inputs,
			entries,
			30,
		)
		compared += len(inputs)
		failures += partFailures
		errored += partErrored
	}
	test.Logf(
		"%d of %d yaml-test-suite inputs match upstream (%d of them errors)",
		compared-failures,
		compared,
		errored,
	)
}

// TestComparisonSeesEveryField corrupts the port's tree for a fixture one field at a time, after its dump was
// held to upstream's fingerprint, and checks that the comparison reports each corruption: every field the
// dump writes, positions, identities and parents, and null told apart from an empty array. Which of the
// comment and children fields a node has is written from its type, so the type's corruption covers it.
func TestComparisonSeesEveryField(test *testing.T) {
	test.Parallel()
	text := "%YAML 1.2 # d\n---\n- &a !t |2- # i\n   x\n- *a # t\n- ? k\n  : v\n  # e\n# l\n- [b, {c: d}]\n"
	answers := recordedFixtures(test)
	expected := recordedFixture(test, answers, text)
	if expected.Error != "" {
		test.Fatalf("upstream does not parse the fixture: its error is %s", expected.Error)
	}
	if difference := compareParse(text, expected); difference != "" {
		test.Fatalf("the uncorrupted tree differs: %s", difference)
	}
	// find returns the first node, depth first, of the type, through the fields the dump walks but the root's
	// comments, so a comment found is the one attached where the tree reaches it.
	var find func(node *Node, nodeType string) *Node
	find = func(node *Node, nodeType string) *Node {
		if node == nil {
			return nil
		}
		if node.NodeType == nodeType {
			return node
		}
		for _, nodes := range [][]*Node{
			node.Children,
			{node.Tag},
			{node.Anchor},
			node.MiddleComments,
			node.LeadingComments,
			{node.IndicatorComment},
			{node.TrailingComment},
			node.EndComments,
		} {
			for _, each := range nodes {
				if found := find(each, nodeType); found != nil {
					return found
				}
			}
		}
		return nil
	}
	corruptions := map[string]func(root *Node){
		"start offset":  func(root *Node) { find(root, "plain").Position.Start.Offset++ },
		"end line":      func(root *Node) { find(root, "mapping").Position.End.Line = 99 },
		"start column":  func(root *Node) { find(root, "comment").Position.Start.Column = 7 },
		"document end":  func(root *Node) { find(root, "document").Position.End.Offset = 3 },
		"value":         func(root *Node) { find(root, "alias").Value = "b" },
		"tag value":     func(root *Node) { find(root, "tag").Value = "!u" },
		"anchor value":  func(root *Node) { find(root, "anchor").Value = "" },
		"comment value": func(root *Node) { find(root, "comment").Value = " x" },
		"chomping":      func(root *Node) { find(root, "blockLiteral").Chomping = "keep" },
		"indent":        func(root *Node) { find(root, "blockLiteral").Indent = nil },
		"type":          func(root *Node) { find(root, "blockLiteral").NodeType = "blockFolded" },
		"marker":        func(root *Node) { find(root, "document").DirectivesEndMarker = false },
		"end marker":    func(root *Node) { find(root, "document").DocumentEndMarker = true },
		"name":          func(root *Node) { find(root, "directive").Name = "TAG" },
		"parameters":    func(root *Node) { find(root, "directive").Parameters = []string{} },
		"parent":        func(root *Node) { find(root, "plain").Parent = root },
		"no parent":     func(root *Node) { find(root, "plain").Parent = nil },
		// The root's comments are the comments the tree reaches where they are attached too, so a copy in
		// their place is a node the port reaches twice as two.
		"identity": func(root *Node) {
			copied := *root.Comments[0]
			root.Comments[0] = &copied
		},
		"null for []":  func(root *Node) { find(root, "mappingValue").LeadingComments = nil },
		"[] for null":  func(root *Node) { find(root, "plain").Children = []*Node{} },
		"no tag":       func(root *Node) { find(root, "blockLiteral").Tag = nil },
		"no indicator": func(root *Node) { find(root, "blockLiteral").IndicatorComment = nil },
		"trailing moved": func(root *Node) {
			alias := find(root, "alias")
			alias.LeadingComments = []*Node{alias.TrailingComment}
			alias.TrailingComment = nil
		},
		"end comment": func(root *Node) {
			var corrupt func(node *Node) bool
			corrupt = func(node *Node) bool {
				if node == nil {
					return false
				}
				if len(node.EndComments) > 0 {
					node.EndComments = []*Node{}
					return true
				}
				for _, child := range node.Children {
					if corrupt(child) {
						return true
					}
				}
				return corrupt(node.Tag) || corrupt(node.Anchor)
			}
			if !corrupt(root) {
				test.Error("the fixture has no end comment to corrupt")
			}
		},
		"root comments": func(root *Node) { root.Comments = root.Comments[1:] },
		"child dropped": func(root *Node) {
			flow := find(root, "flowSequence")
			flow.Children = flow.Children[:1]
		},
		"middle comments": func(root *Node) {
			find(root, "plain").MiddleComments = []*Node{find(root, "comment")}
		},
	}
	for name, corrupt := range corruptions {
		// Each corruption edits a tree of its own, parsed again.
		root, parseError := Parse(text, nil)
		if parseError != nil {
			test.Fatal(parseError)
		}
		corrupt(root)
		if difference := compareDump(parseDump(text, root, nil), false, expected); difference == "" {
			test.Errorf("corrupting the %s went unnoticed", name)
		}
	}
	// And the errors: a different message, name or position.
	errorText := "a: [b"
	expectedError := recordedFixture(test, answers, errorText)
	if expectedError.Error == "" {
		test.Fatalf("upstream parses %q, which the error corruptions need refused", errorText)
	}
	_, parseError := Parse(errorText, nil)
	var syntaxError *SyntaxError
	if !errors.As(parseError, &syntaxError) {
		test.Fatalf("Parse(%q) returned %v, not a SyntaxError", errorText, parseError)
	}
	if difference := compareDump(parseDump(errorText, nil, syntaxError), true, expectedError); difference != "" {
		test.Fatalf("the uncorrupted error differs: %s", difference)
	}
	for name, corrupt := range map[string]func(syntaxError SyntaxError) error{
		"message": func(syntaxError SyntaxError) error {
			corrupted := syntaxError
			corrupted.Message += "."
			return &corrupted
		},
		"name": func(syntaxError SyntaxError) error {
			return &ThrownError{Name: "TypeError", Message: syntaxError.Message}
		},
		"position": func(syntaxError SyntaxError) error {
			corrupted := syntaxError
			corrupted.Position.End.Offset++
			return &corrupted
		},
	} {
		corrupted := corrupt(*syntaxError)
		if difference := compareDump(parseDump(errorText, nil, corrupted), true, expectedError); difference == "" {
			test.Errorf("corrupting the error's %s went unnoticed", name)
		}
	}
}

// corpusDirectory holds the corpus, Prettier's own format fixtures, shared with the other YAML packages.
var corpusDirectory = filepath.Join("..", "testdata", "prettier_tests")

// frontMatterValues are the corpus's front matter values as inputs: each markdown file's YAML front matter
// value, read by mdast and trimmed as the front matter embed trims it, under the file's name and
// " (front matter)". The file is vendored, so each value's answer records its digest, which also holds
// mdast to reading the value Prettier's own parser read when the answer was recorded.
func frontMatterValues(test *testing.T) ([]recorded.Input, []recorded.Entry) {
	test.Helper()
	files, _ := readInputs(test, filepath.Join(corpusDirectory, "front_matter_inputs.json"))
	values := make([]recorded.Input, len(files))
	entries := make([]recorded.Entry, len(files))
	for index, file := range files {
		frontMatter, _ := mdast.ParseFrontMatter(file.Text)
		if frontMatter == nil || frontMatter.Language != "yaml" {
			test.Fatalf("%s holds no yaml front matter for mdast", file.Name)
		}
		value := ecmascripttext.TrimWhitespace(frontMatter.Value)
		values[index] = recorded.Input{Name: file.Name + " (front matter)", Text: value}
		entries[index] = recorded.Entry{Name: values[index].Name, Input: recorded.Digest([]byte(value))}
	}
	return values, entries
}

// TestCorpusParseMatchesUpstream compares every YAML file of the corpus, Prettier's own YAML format
// fixtures but the spec examples the yaml-test-suite holds, half by half, and the YAML front matter value
// of every markdown fixture of Prettier's that has one.
func TestCorpusParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	compared, failures, errored := 0, 0, 0
	measure := func(label string, answersPath string, inputs []recorded.Input, entries []recorded.Entry) {
		labelFailures, labelErrored := compareAll(test, label, answersPath, inputs, entries, 30)
		compared += len(inputs)
		failures += labelFailures
		errored += labelErrored
	}
	paths, answersPaths := parts(test, corpusDirectory, "yaml_inputs_", "corpus_answers_")
	for half, path := range paths {
		inputs, entries := readInputs(test, path)
		measure(fmt.Sprintf("corpus half %d", half+1), answersPaths[half], inputs, entries)
	}
	frontMatter, frontMatterEntries := frontMatterValues(test)
	measure(
		"front matter",
		filepath.Join(yamlUnistParserDirectory, "front_matter_answers.json"),
		frontMatter,
		frontMatterEntries,
	)
	test.Logf(
		"%d of %d inputs match upstream (%d front matter values; %d of them errors)",
		compared-failures,
		compared,
		len(frontMatter),
		errored,
	)
}
