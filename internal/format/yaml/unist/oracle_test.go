package unist

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/yaml/upstream_testing"
)

// The parse oracle: yaml-unist-parser 3.2.0's parse(text, { uniqueKeys: false }), from the fork's
// node_modules, with the whole tree written as JSON, root.comments included, every object under its
// identity: an id per object (a comment reached from root.comments and from the node it is attached to
// is one object, one id), its _parent's id, and the keys it has. The test parses the same text with
// Parse and walks both trees in step, comparing every field of every node: type, position (offsets
// converted to bytes as the printer's tree loader converts them), value, chomping, indent, markers,
// name, parameters, every child list with null and [] told apart, and the comment slots. The ids prove
// the shape is the same graph: the same node reached twice on one side is the same node reached twice on
// the other, and every Parent is the node upstream's _parent is. Where upstream throws, Parse must fail
// with the same name and message, and for a YAMLSyntaxError the same position.
//
// Upstream's trees are recorded (see upstream_testing), so no node runs here: each answers file under
// testdata/yaml_unist_parser holds, per input, the text it was recorded for and upstream's tree or error.

// yamlUnistParserDirectory holds the inputs and upstream's recorded answers for them.
var yamlUnistParserDirectory = filepath.Join("testdata", "yaml_unist_parser")

// suiteDirectory holds the yaml-test-suite's cases, shared with the other YAML packages.
var suiteDirectory = filepath.Join("..", "testdata", "yaml_test_suite")

type jsonPoint struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

type jsonPosition struct {
	Start jsonPoint `json:"start"`
	End   jsonPoint `json:"end"`
}

// jsonNode is a node as the oracle writes it. Unknown fields fail the decode, so a field upstream adds
// that unist.Node does not carry is noticed rather than dropped.
type jsonNode struct {
	Id        int          `json:"id"`
	Parent    int          `json:"parent"`
	Keys      []string     `json:"keys"`
	Undefined bool         `json:"undefined"`
	Type      string       `json:"type"`
	Position  jsonPosition `json:"position"`

	Children            []*jsonNode `json:"children"`
	Comments            []*jsonNode `json:"comments"`
	Value               *string     `json:"value"`
	Tag                 *jsonNode   `json:"tag"`
	Anchor              *jsonNode   `json:"anchor"`
	MiddleComments      []*jsonNode `json:"middleComments"`
	LeadingComments     []*jsonNode `json:"leadingComments"`
	TrailingComment     *jsonNode   `json:"trailingComment"`
	EndComments         []*jsonNode `json:"endComments"`
	Chomping            *string     `json:"chomping"`
	Indent              *int        `json:"indent"`
	IndicatorComment    *jsonNode   `json:"indicatorComment"`
	DirectivesEndMarker *bool       `json:"directivesEndMarker"`
	DocumentEndMarker   *bool       `json:"documentEndMarker"`
	Name                *string     `json:"name"`
	Parameters          []string    `json:"parameters"`
}

type jsonError struct {
	Name     string        `json:"name"`
	Message  string        `json:"message"`
	Position *jsonPosition `json:"position"`
}

type oracleResult struct {
	Root  json.RawMessage `json:"root"`
	Error *jsonError      `json:"error"`
}

// parseAnswer is one recorded answer: the input's name and text, and upstream's tree or its error.
type parseAnswer struct {
	Name  string          `json:"name"`
	Text  string          `json:"text"`
	Root  json.RawMessage `json:"root"`
	Error *jsonError      `json:"error"`
}

// recordedParses reads an answers file and pairs it with the inputs, returning upstream's result for each
// input, in the inputs' order, and the file's source and date.
func recordedParses(test *testing.T, answersFile string, inputs []upstream_testing.Input) ([]oracleResult, string) {
	test.Helper()
	path := filepath.Join(yamlUnistParserDirectory, answersFile)
	answers := upstream_testing.ReadAnswers[parseAnswer](test, path)
	byName := upstream_testing.Match(test, path, inputs, answers.Answers, func(answer parseAnswer) (string, string) {
		return answer.Name, answer.Text
	})
	results := make([]oracleResult, len(inputs))
	for index, input := range inputs {
		answer := byName[input.Name]
		if (answer.Root == nil) == (answer.Error == nil) {
			test.Fatalf("%s: %q holds neither a tree nor an error, or both", path, input.Name)
		}
		results[index] = oracleResult{Root: answer.Root, Error: answer.Error}
	}
	return results, answers.Source + ", recorded " + answers.Recorded
}

// fixtureInputs are the hand-written fixtures of the deleted parse_test.go, then the texts
// TestOracleCanFail and TestComparisonSeesEveryField compare against.
func fixtureInputs(test *testing.T) []upstream_testing.Input {
	test.Helper()
	return upstream_testing.ReadInputs(test, filepath.Join(yamlUnistParserDirectory, "fixture_inputs.json"))
}

// recordedFixtures is upstream's recorded result for each fixture's text.
func recordedFixtures(test *testing.T) map[string]oracleResult {
	test.Helper()
	inputs := fixtureInputs(test)
	results, _ := recordedParses(test, "fixture_answers.json.gz", inputs)
	byText := make(map[string]oracleResult, len(inputs))
	for index, input := range inputs {
		byText[input.Text] = results[index]
	}
	return byText
}

// recordedFixture is upstream's recorded result for text, which must be one of the fixtures.
func recordedFixture(test *testing.T, byText map[string]oracleResult, text string) oracleResult {
	test.Helper()
	result, recorded := byText[text]
	if !recorded {
		test.Fatalf("fixture_inputs.json no longer holds %q", text)
	}
	return result
}

// comparison walks the two trees in step.
type comparison struct {
	offsets unitOffsets
	// byId is the port's node for each oracle id, and idOf the oracle id for each port node.
	byId map[int]*Node
	idOf map[*Node]int
	// parents are the port's nodes with the oracle's parent id for each.
	parents     map[*Node]int
	differences []string
}

func (comparison *comparison) differ(path string, format string, arguments ...any) {
	comparison.differences = append(comparison.differences, path+": "+fmt.Sprintf(format, arguments...))
}

// jsonString is the port's string as the oracle's arrives: JSON.stringify escapes a lone surrogate,
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

func (comparison *comparison) point(path string, name string, mine Point, theirs jsonPoint) {
	expected := Point{Line: theirs.Line, Column: theirs.Column, Offset: comparison.offsets.byteOffset(theirs.Offset)}
	if mine != expected {
		comparison.differ(path, "%s is %+v, upstream's %+v (unit offset %d)", name, mine, expected, theirs.Offset)
	}
}

func (comparison *comparison) list(path string, name string, mine []*Node, theirs []*jsonNode, present bool) {
	if !present {
		if mine != nil {
			comparison.differ(path, "%s is %d nodes where upstream has no such field", name, len(mine))
		}
		return
	}
	if mine == nil || theirs == nil {
		// encoding/json decodes null as a nil slice and [] as an empty one.
		if (mine == nil) != (theirs == nil) {
			comparison.differ(
				path,
				"%s: one side is null, the other an array (the port's is nil: %v)",
				name,
				mine == nil,
			)
		}
		return
	}
	if len(mine) != len(theirs) {
		comparison.differ(path, "%s has %d nodes, upstream's %d", name, len(mine), len(theirs))
		return
	}
	for index := range mine {
		comparison.node(fmt.Sprintf("%s.%s[%d]", path, name, index), mine[index], theirs[index])
	}
}

func (comparison *comparison) node(path string, mine *Node, theirs *jsonNode) {
	if theirs == nil || theirs.Undefined {
		if mine != nil {
			comparison.differ(path, "is a %s where upstream's is null", mine.NodeType)
		}
		return
	}
	if mine == nil {
		comparison.differ(path, "is nil where upstream's is a %s", theirs.Type)
		return
	}
	typedPath := path + "<" + theirs.Type + ">"

	// Identity: one object upstream is one node here, and the other way round.
	if known, seen := comparison.byId[theirs.Id]; seen {
		if known != mine {
			comparison.differ(typedPath, "upstream reaches its object #%d again, the port a different node", theirs.Id)
		}
	} else {
		comparison.byId[theirs.Id] = mine
	}
	if known, seen := comparison.idOf[mine]; seen {
		if known != theirs.Id {
			comparison.differ(
				typedPath,
				"the port reaches one node as upstream's objects #%d and #%d",
				known,
				theirs.Id,
			)
		}
	} else {
		comparison.idOf[mine] = theirs.Id
	}
	comparison.parents[mine] = theirs.Parent

	has := func(key string) bool { return slices.Contains(theirs.Keys, key) }
	if mine.NodeType != theirs.Type {
		comparison.differ(typedPath, "type is %q", mine.NodeType)
	}
	comparison.point(typedPath, "start", mine.Position.Start, theirs.Position.Start)
	comparison.point(typedPath, "end", mine.Position.End, theirs.Position.End)
	if hasLeadingCommentsField(mine.NodeType) != has("leadingComments") ||
		hasTrailingCommentField(mine.NodeType) != has("trailingComment") ||
		hasChildrenField(mine.NodeType) != has("children") {
		comparison.differ(typedPath, "the port's field predicates disagree with upstream's keys %v", theirs.Keys)
	}

	stringField := func(name string, mineValue string, theirsValue *string) {
		expected := ""
		if theirsValue != nil {
			expected = *theirsValue
		}
		if jsonString(mineValue) != expected {
			comparison.differ(typedPath, "%s is %q, upstream's %q", name, mineValue, expected)
		}
	}
	stringField("value", mine.Value, theirs.Value)
	stringField("chomping", mine.Chomping, theirs.Chomping)
	stringField("name", mine.Name, theirs.Name)
	if (mine.Indent == nil) != (theirs.Indent == nil) || mine.Indent != nil && *mine.Indent != *theirs.Indent {
		comparison.differ(typedPath, "indent differs")
	}
	boolField := func(name string, mineValue bool, theirsValue *bool) {
		if mineValue != (theirsValue != nil && *theirsValue) {
			comparison.differ(typedPath, "%s is %v", name, mineValue)
		}
	}
	boolField("directivesEndMarker", mine.DirectivesEndMarker, theirs.DirectivesEndMarker)
	boolField("documentEndMarker", mine.DocumentEndMarker, theirs.DocumentEndMarker)
	if has("parameters") != (mine.Parameters != nil) || !slices.Equal(mine.Parameters, theirs.Parameters) {
		comparison.differ(typedPath, "parameters are %q, upstream's %q", mine.Parameters, theirs.Parameters)
	}

	comparison.list(typedPath, "children", mine.Children, theirs.Children, has("children"))
	comparison.node(typedPath+".tag", mine.Tag, theirs.Tag)
	comparison.node(typedPath+".anchor", mine.Anchor, theirs.Anchor)
	comparison.list(typedPath, "middleComments", mine.MiddleComments, theirs.MiddleComments, has("middleComments"))
	comparison.list(typedPath, "leadingComments", mine.LeadingComments, theirs.LeadingComments, has("leadingComments"))
	comparison.node(typedPath+".indicatorComment", mine.IndicatorComment, theirs.IndicatorComment)
	comparison.node(typedPath+".trailingComment", mine.TrailingComment, theirs.TrailingComment)
	comparison.list(typedPath, "endComments", mine.EndComments, theirs.EndComments, has("endComments"))
	comparison.list(typedPath, "comments", mine.Comments, theirs.Comments, has("comments"))
}

// compareParse compares Parse's result for the text with the oracle's and returns the differences.
func compareParse(text string, expected oracleResult) []string {
	root, parseError := Parse(text, nil)
	offsets := newUnitOffsets(text, nil)
	if expected.Error != nil {
		if parseError == nil {
			return []string{
				fmt.Sprintf("upstream throws %s: %s; the port parses", expected.Error.Name, expected.Error.Message),
			}
		}
		name, message := "", parseError.Error()
		var syntaxError *SyntaxError
		var thrown *ThrownError
		switch {
		case errors.As(parseError, &syntaxError):
			name, message = "YAMLSyntaxError", syntaxError.Message
			if expected.Error.Position == nil {
				return []string{"upstream's YAMLSyntaxError has no position"}
			}
			want := Position{
				Start: Point{
					Line:   expected.Error.Position.Start.Line,
					Column: expected.Error.Position.Start.Column,
					Offset: offsets.byteOffset(expected.Error.Position.Start.Offset),
				},
				End: Point{
					Line:   expected.Error.Position.End.Line,
					Column: expected.Error.Position.End.Column,
					Offset: offsets.byteOffset(expected.Error.Position.End.Offset),
				},
			}
			if syntaxError.Position != want {
				return []string{fmt.Sprintf("the error's position is %+v, upstream's %+v", syntaxError.Position, want)}
			}
		case errors.As(parseError, &thrown):
			name, message = thrown.Name, thrown.Message
		}
		if name != expected.Error.Name || message != expected.Error.Message {
			return []string{fmt.Sprintf(
				"the port fails with %s: %s; upstream throws %s: %s",
				name,
				message,
				expected.Error.Name,
				expected.Error.Message,
			)}
		}
		return nil
	}
	if parseError != nil {
		return []string{fmt.Sprintf("the port fails (%v); upstream parses", parseError)}
	}
	decoder := json.NewDecoder(bytes.NewReader(expected.Root))
	decoder.DisallowUnknownFields()
	var theirs jsonNode
	if decodeError := decoder.Decode(&theirs); decodeError != nil {
		return []string{fmt.Sprintf("decoding upstream's tree: %v", decodeError)}
	}
	comparison := &comparison{offsets: offsets, byId: map[int]*Node{}, idOf: map[*Node]int{}, parents: map[*Node]int{}}
	comparison.node("root", root, &theirs)
	for node, parentId := range comparison.parents {
		switch parentId {
		case -1, -2:
			// null (the root), or never defined: nil either way here.
			if node.Parent != nil {
				comparison.differ(node.NodeType, "has a parent where upstream's has none")
			}
		default:
			if expected, known := comparison.byId[parentId]; !known || node.Parent != expected {
				comparison.differ(node.NodeType+"@"+fmt.Sprint(node.Position.Start), "parent is not upstream's _parent")
			}
		}
	}
	return comparison.differences
}

// compareAll compares Parse with upstream's recorded result for every input and reports the first few
// that differ.
func compareAll(
	test *testing.T,
	label string,
	answersFile string,
	inputs []upstream_testing.Input,
	maximumReported int,
) (failures int, errored int) {
	test.Helper()
	expected, source := recordedParses(test, answersFile, inputs)
	for index, input := range inputs {
		if expected[index].Error != nil {
			errored++
		}
		differences := compareParse(input.Text, expected[index])
		if len(differences) == 0 {
			continue
		}
		failures++
		if failures <= maximumReported {
			text := fmt.Sprintf("%q", input.Text)
			if len(text) > 200 {
				text = text[:200] + "..."
			}
			if len(differences) > 5 {
				differences = differences[:5]
			}
			test.Errorf("%s %s %s:\n  %s", label, input.Name, text, strings.Join(differences, "\n  "))
		}
	}
	test.Logf("%s: %d inputs against %s", label, len(inputs), source)
	return failures, errored
}
