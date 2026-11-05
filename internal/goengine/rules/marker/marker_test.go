package marker_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/goengine/rules/marker"
)

// checkedMarker is a marker no rule uses, so the cases read as the package's own and not as any one rule's.
const checkedMarker = "Checked:"

// Read gives a reason only to a marker that opens a comment line and is spelled as written, tells an empty reason
// apart, and reads a block comment's lines the way it reads a line comment's.
func TestReadFindsTheMarkerOnlyWhereItOpensALine(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name     string
		comments []string
		expected marker.Excuse
	}{
		{"marker opening a line", []string{"// Checked: the input is a constant."}, marker.WithReason},
		{"marker against the slashes", []string{"//Checked: the input is a constant."}, marker.WithReason},
		{"marker on a later comment", []string{"// Says what it does.", "//", "// Checked: why."}, marker.WithReason},
		{"marker mid-sentence", []string{"// This was once Checked: by hand, so it excuses nothing."}, marker.None},
		{"marker in the wrong case", []string{"// checked: the input is a constant."}, marker.None},
		{"marker without its colon", []string{"// Checked the input by hand."}, marker.None},
		{"empty reason", []string{"// Checked:"}, marker.WithoutReason},
		{"empty reason followed by spaces", []string{"// Checked:   "}, marker.WithoutReason},
		{"block comment with a reason", []string{"/* Checked: the input is a constant. */"}, marker.WithReason},
		{"block comment without a reason", []string{"/* Checked: */"}, marker.WithoutReason},
		{"block comment closed against the colon", []string{"/*Checked:*/"}, marker.WithoutReason},
		{"starred continuation line", []string{"/*\n * Says what it does.\n * Checked: why.\n */"}, marker.WithReason},
		{"later block comment line", []string{"/* Says what it does.\n   Checked: why. */"}, marker.WithReason},
		// The reason is what follows the marker on its own line, so a reason on the line below is no reason.
		{"reason on the next line", []string{"// Checked:", "// the input is a constant."}, marker.WithoutReason},
		{"empty marker beside a reasoned one", []string{"// Checked:", "// Checked: why."}, marker.WithReason},
		{"multi-byte text before the marker", []string{"// Überprüft, Checked: nothing."}, marker.None},
		{"multi-byte reason", []string{"// Checked: the café's input is a constant."}, marker.WithReason},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			group := &ast.CommentGroup{}
			for _, text := range testCase.comments {
				group.List = append(group.List, &ast.Comment{Text: text})
			}
			if found := marker.Read(group, checkedMarker); found != testCase.expected {
				subtest.Errorf("Read of %q is %d, expected %d", testCase.comments, found, testCase.expected)
			}
		})
	}
}

// A nil group, a node with no comment above it, says nothing.
func TestReadOfNoGroupIsNone(test *testing.T) {
	test.Parallel()
	if found := marker.Read(nil, checkedMarker); found != marker.None {
		test.Errorf("Read of a nil group is %d, expected None", found)
	}
}

// The index finds a comment for a node only directly above it on lines of its own, or at the end of its last line,
// and never a comment that speaks for the code beside it.
func TestIndexFindsTheCommentsThatSpeakForANode(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string
		// body is a function body holding one call to target, the node under test, as its statement.
		body          string
		expectAbove   bool
		trailingCount int
		expected      marker.Excuse
	}{
		{"comment directly above", "// Checked: why.\ntarget()", true, 0, marker.WithReason},
		{"block comment directly above", "/* Checked: why. */\ntarget()", true, 0, marker.WithReason},
		{"marker ending a group above", "// Says what.\n//\n// Checked: why.\ntarget()", true, 0, marker.WithReason},
		{"empty marker directly above", "// Checked:\ntarget()", true, 0, marker.WithoutReason},
		{"comment trailing the line above", "other() // Checked: why.\ntarget()", false, 0, marker.None},
		{"comment trailing a closing brace above", "if true {\n} // Checked: why.\ntarget()", false, 0, marker.None},
		{"comment against the code on the line above", "other()// Checked: why.\ntarget()", false, 0, marker.None},
		{"comment before code on the line above", "/* Checked: why. */ other()\ntarget()", false, 0, marker.None},
		{"comment a blank line away", "// Checked: why.\n\ntarget()", false, 0, marker.None},
		{"comment trailing the node", "target() // Checked: why.", false, 1, marker.WithReason},
		{"block comment trailing the node", "target() /* Checked: why. */", false, 1, marker.WithReason},
		{"comment trailing the node's last line", "target(\n\t1,\n) // Checked: why.", false, 1, marker.WithReason},
		{"comment trailing the node's first line", "target( // Checked: why.\n\t1,\n)", false, 0, marker.None},
		{"comment before the node on its line", "/* Checked: why. */ target()", false, 0, marker.None},
		{"comment trailing the node without a reason", "target() // Checked:", false, 1, marker.WithoutReason},
		{"reasons above and at the end", "// Checked:\ntarget() // Checked: why.", true, 1, marker.WithReason},
		{"comment below the node", "target()\n// Checked: why.\nother()", false, 0, marker.None},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			index, node := parseTarget(subtest, testCase.body)
			if above := index.Above(node); (above != nil) != testCase.expectAbove {
				subtest.Errorf("Above is %v, expected a group: %v", groupText(above), testCase.expectAbove)
			}
			if trailing := index.Trailing(node); len(trailing) != testCase.trailingCount {
				subtest.Errorf("Trailing found %d groups, expected %d", len(trailing), testCase.trailingCount)
			}
			if found := index.ExcuseFor(node, checkedMarker); found != testCase.expected {
				subtest.Errorf("ExcuseFor is %d, expected %d", found, testCase.expected)
			}
		})
	}
}

// Lines are the file's own, so a //line directive that renumbers the node, here closing the group above it and
// moving the node to line 1 of another file, leaves the comment directly above the node it sits over.
func TestIndexReadsTheFilesOwnLines(test *testing.T) {
	test.Parallel()
	index, node := parseTarget(test, "// Checked: why.\n//line renumbered.go:1\ntarget()")
	if found := index.ExcuseFor(node, checkedMarker); found != marker.WithReason {
		test.Errorf("ExcuseFor under a //line directive is %d, expected WithReason", found)
	}
}

// parseTarget parses body inside a function and returns the file's index with the statement that calls target.
func parseTarget(test *testing.T, body string) (*marker.Index, ast.Node) {
	test.Helper()
	fileSet := token.NewFileSet()
	source := "package fixture\n\nfunc target(values ...int) {}\n\nfunc other() {}\n\n" +
		"func function() {\n" + body + "\n}\n"
	file, parseError := parser.ParseFile(fileSet, "fixture.go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing %q: %v", body, parseError)
	}
	var statement ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		expression, isExpression := node.(*ast.ExprStmt)
		if !isExpression {
			return true
		}
		call, isCall := expression.X.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if name, isIdentifier := call.Fun.(*ast.Ident); isIdentifier && name.Name == "target" {
			statement = expression
		}
		return true
	})
	if statement == nil {
		test.Fatalf("finding the call to target in %q", body)
	}
	return marker.NewIndex(fileSet, file), statement
}

// groupText is a group's comments as one line for a failure message, or "nil".
func groupText(group *ast.CommentGroup) string {
	if group == nil {
		return "nil"
	}
	texts := []string{}
	for _, comment := range group.List {
		texts = append(texts, comment.Text)
	}
	return strings.Join(texts, " ")
}
