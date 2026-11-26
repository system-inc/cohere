package unist

import (
	"encoding/json"
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
	failures, errored := compareAll(test, "fixture", "fixture_answers.json.gz", inputs, entries, 40)
	test.Logf("%d of %d fixtures match upstream (%d of them errors)", len(inputs)-failures, len(inputs), errored)
}

// TestGeneratedParseMatchesUpstream compares inputs drawn from YAML's indicators, properties, scalar
// styles, spaces, tabs, line breaks, comments and a few letters and non-ASCII characters: one fixed seed's
// first 500, the slice that keeps the inputs and answers under 50 KB.
func TestGeneratedParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	inputs, entries := readInputs(test, filepath.Join(yamlUnistParserDirectory, "generated_inputs.json"))
	failures, errored := compareAll(test, "generated", "generated_answers.json.gz", inputs, entries, 30)
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
		if differences := compareParse(each.mine, recordedFixture(test, answers, each.theirs)); len(differences) == 0 {
			test.Errorf("the comparison found no difference between %q and %q", each.mine, each.theirs)
		}
	}
}

// TestYamlTestSuiteParseMatchesUpstream compares every input of the yaml-test-suite, and each case's
// canonical dump, part by part.
func TestYamlTestSuiteParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	compared, failures, errored := 0, 0, 0
	paths, answersFiles := parts(test, suiteDirectory, "cases_", "suite_answers_")
	for part, path := range paths {
		// A part's dumps sit in dumps_<n>.json beside its cases, and are answered with them.
		dumps := filepath.Join(suiteDirectory, fmt.Sprintf("dumps_%d.json", part+1))
		inputs, entries := readInputs(test, path, dumps)
		partFailures, partErrored := compareAll(
			test,
			fmt.Sprintf("suite part %d", part+1),
			answersFiles[part],
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

// TestComparisonSeesEveryField corrupts upstream's correct tree one field at a time and checks that the
// comparison reports each corruption: every field the comparison reads, positions, identities and
// parents, and null told apart from an empty array.
func TestComparisonSeesEveryField(test *testing.T) {
	test.Parallel()
	text := "%YAML 1.2 # d\n---\n- &a !t |2- # i\n   x\n- *a # t\n- ? k\n  : v\n  # e\n# l\n- [b, {c: d}]\n"
	answers := recordedFixtures(test)
	expected := recordedFixture(test, answers, text)
	if expected.Error != nil {
		test.Fatalf("the fixture does not parse: %s", expected.Error.Message)
	}
	if differences := compareParse(text, expected); len(differences) != 0 {
		test.Fatalf("the uncorrupted tree differs: %v", differences)
	}
	// find returns the first node, depth first, of the type, through the fields the comparison walks.
	var find func(node *jsonNode, nodeType string) *jsonNode
	find = func(node *jsonNode, nodeType string) *jsonNode {
		if node == nil {
			return nil
		}
		if node.Type == nodeType {
			return node
		}
		for _, nodes := range [][]*jsonNode{
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
	// Each corruption edits the tree decoded into jsonNode, the shape compareParse reads it back into, so
	// encoding it again keeps every field the comparison sees, null told apart from an empty array.
	corruptions := map[string]func(root *jsonNode){
		"start offset":  func(root *jsonNode) { find(root, "plain").Position.Start.Offset++ },
		"end line":      func(root *jsonNode) { find(root, "mapping").Position.End.Line = 99 },
		"start column":  func(root *jsonNode) { find(root, "comment").Position.Start.Column = 7 },
		"document end":  func(root *jsonNode) { find(root, "document").Position.End.Offset = 3 },
		"value":         func(root *jsonNode) { find(root, "alias").Value = new("b") },
		"tag value":     func(root *jsonNode) { find(root, "tag").Value = new("!u") },
		"anchor value":  func(root *jsonNode) { find(root, "anchor").Value = new("") },
		"comment value": func(root *jsonNode) { find(root, "comment").Value = new(" x") },
		"chomping":      func(root *jsonNode) { find(root, "blockLiteral").Chomping = new("keep") },
		"indent":        func(root *jsonNode) { find(root, "blockLiteral").Indent = nil },
		"type":          func(root *jsonNode) { find(root, "blockLiteral").Type = "blockFolded" },
		"marker":        func(root *jsonNode) { find(root, "document").DirectivesEndMarker = new(false) },
		"end marker":    func(root *jsonNode) { find(root, "document").DocumentEndMarker = new(true) },
		"name":          func(root *jsonNode) { find(root, "directive").Name = new("TAG") },
		"parameters":    func(root *jsonNode) { find(root, "directive").Parameters = []string{} },
		"parent":        func(root *jsonNode) { find(root, "plain").Parent = 0 },
		"identity":      func(root *jsonNode) { find(root, "comment").Id = 9999 },
		"keys":          func(root *jsonNode) { find(root, "plain").Keys = []string{"type"} },
		"null for []":   func(root *jsonNode) { find(root, "mappingValue").LeadingComments = nil },
		"no tag":        func(root *jsonNode) { find(root, "blockLiteral").Tag = nil },
		"no indicator":  func(root *jsonNode) { find(root, "blockLiteral").IndicatorComment = nil },
		"trailing moved": func(root *jsonNode) {
			alias := find(root, "alias")
			alias.LeadingComments = []*jsonNode{alias.TrailingComment}
			alias.TrailingComment = nil
		},
		"end comment": func(root *jsonNode) {
			var corrupt func(node *jsonNode) bool
			corrupt = func(node *jsonNode) bool {
				if node == nil {
					return false
				}
				if len(node.EndComments) > 0 {
					node.EndComments = []*jsonNode{}
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
		"root comments": func(root *jsonNode) { root.Comments = root.Comments[1:] },
		"child dropped": func(root *jsonNode) {
			flow := find(root, "flowSequence")
			flow.Children = flow.Children[:1]
		},
		"middle comments": func(root *jsonNode) {
			find(root, "plain").MiddleComments = []*jsonNode{find(root, "comment")}
		},
	}
	// The round trip alone must still read as upstream's tree, or every corruption would differ for it.
	untouched := &jsonNode{}
	if decodeError := json.Unmarshal(expected.Root, untouched); decodeError != nil {
		test.Fatal(decodeError)
	}
	reencoded, reencodeError := json.Marshal(untouched)
	if reencodeError != nil {
		test.Fatal(reencodeError)
	}
	if differences := compareParse(text, oracleResult{Root: reencoded}); len(differences) != 0 {
		test.Fatalf("the tree differs once decoded and encoded again: %v", differences)
	}
	for name, corrupt := range corruptions {
		root := &jsonNode{}
		if decodeError := json.Unmarshal(expected.Root, root); decodeError != nil {
			test.Fatal(decodeError)
		}
		corrupt(root)
		encoded, encodeError := json.Marshal(root)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		if differences := compareParse(text, oracleResult{Root: encoded}); len(differences) == 0 {
			test.Errorf("corrupting the %s went unnoticed", name)
		}
	}
	// And the errors: a different message, name or position.
	errorText := "a: [b"
	expectedError := recordedFixture(test, answers, errorText)
	if expectedError.Error == nil {
		test.Fatalf("upstream parses %q, which the error corruptions need refused", errorText)
	}
	for name, corrupt := range map[string]func(*jsonError){
		"message":  func(jsonError *jsonError) { jsonError.Message += "." },
		"name":     func(jsonError *jsonError) { jsonError.Name = "TypeError" },
		"position": func(jsonError *jsonError) { jsonError.Position.End.Offset++ },
	} {
		copied := *expectedError.Error
		position := *copied.Position
		copied.Position = &position
		corrupt(&copied)
		if differences := compareParse(errorText, oracleResult{Error: &copied}); len(differences) == 0 {
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
	measure := func(label string, answersFile string, inputs []recorded.Input, entries []recorded.Entry) {
		labelFailures, labelErrored := compareAll(test, label, answersFile, inputs, entries, 30)
		compared += len(inputs)
		failures += labelFailures
		errored += labelErrored
	}
	paths, answersFiles := parts(test, corpusDirectory, "yaml_inputs_", "corpus_answers_")
	for half, path := range paths {
		inputs, entries := readInputs(test, path)
		measure(fmt.Sprintf("corpus half %d", half+1), answersFiles[half], inputs, entries)
	}
	frontMatter, frontMatterEntries := frontMatterValues(test)
	measure("front matter", "front_matter_answers.json.gz", frontMatter, frontMatterEntries)
	test.Logf(
		"%d of %d inputs match upstream (%d front matter values; %d of them errors)",
		compared-failures,
		compared,
		len(frontMatter),
		errored,
	)
}
