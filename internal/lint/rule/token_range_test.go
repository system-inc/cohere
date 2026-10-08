package rule

import (
	"fmt"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// TestTokenRangeOfAZeroWidthNodeIsEmptyWhereItSits holds TokenRange to never start past the node's end, on
// every node of sources that each hold a zero-width node followed by trivia (#p86b9pw).
//
// A zero-width node has no token of its own, and scanning forward from it finds the next one, past whatever
// trivia follows. Cut at the node's end, that start lands after the end, and a caller slicing the source
// with the range panics: no-self-compare did on `<a />>0`, valid TSX, with "slice bounds out of range
// [3:2]". The empty JSX attribute list there is the zero-width node and the space before `/>` the trivia.
// Without the space (`<a/>`) the next token starts where the node does, which is why nothing caught it.
//
// Each source must hold at least one zero-width node with trivia after it, or this test could pass on a
// shape that cannot fail.
func TestTokenRangeOfAZeroWidthNodeIsEmptyWhereItSits(t *testing.T) {
	t.Parallel()
	for _, source := range []struct {
		name, fileName, text string
		kind                 core.ScriptKind
	}{
		{"self-closing element with a space", "/repository/source/Element.tsx", "<a />>0", core.ScriptKindTSX},
		{"opening element with a space", "/repository/source/Opening.tsx", "const node = <a ></a>;", core.ScriptKindTSX},
		{"upstream's comparison", "/repository/source/Render.tsx", "ReactDOM.render(<div />, x) === null;", core.ScriptKindTSX},
		{"a comment after the tag name", "/repository/source/Comment.tsx", "const node = <a/* note */ />;", core.ScriptKindTSX},
		{"a missing condition in recovery", "/repository/source/Recovery.ts", "if ( ) {}", core.ScriptKindTS},
	} {
		t.Run(source.name, func(t *testing.T) {
			t.Parallel()
			sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: tspath.RootedFilePath(source.fileName),
				PathKey: tspath.PathKey(source.fileName)}, source.text, source.kind)
			problems, zeroWidthBeforeTrivia := zeroWidthRangeProblems(sourceFile, TokenRange)
			for _, problem := range problems {
				t.Error(problem)
			}
			if zeroWidthBeforeTrivia == 0 {
				t.Fatalf("%q holds no zero-width node followed by trivia, so this case could not fail", source.text)
			}
			// The mutant: the scanner's start cut at the node's end, which TokenRange returned before the fix.
			inverted, _ := zeroWidthRangeProblems(sourceFile, func(sourceFile *ast.SourceFile, node *ast.Node) core.TextRange {
				if ast.NodeIsMissing(node) {
					return scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos()).WithEnd(node.End())
				}
				return TokenRange(sourceFile, node)
			})
			if len(inverted) == 0 {
				t.Errorf("%q: the inverted range passed, so this case could not have failed", source.text)
			}
		})
	}
}

// zeroWidthRangeProblems walks every node of sourceFile and names each range that starts past its end, and
// each zero-width node whose range is not the empty one where it sits. It also counts the zero-width nodes
// followed by trivia, the shape that inverted the range.
func zeroWidthRangeProblems(sourceFile *ast.SourceFile, rangeOf func(*ast.SourceFile, *ast.Node) core.TextRange) ([]string, int) {
	text := sourceFile.Text()
	var problems []string
	zeroWidthBeforeTrivia := 0
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		span := rangeOf(sourceFile, node)
		if span.Pos() > span.End() {
			problems = append(problems, fmt.Sprintf("%s at %d..%d: range %d..%d starts past its end", node.Kind, node.Pos(), node.End(), span.Pos(), span.End()))
		}
		if node.Pos() == node.End() {
			if span != node.Loc {
				problems = append(problems, fmt.Sprintf("zero-width %s at %d: range %d..%d, want the empty range where it sits", node.Kind, node.Pos(), span.Pos(), span.End()))
			}
			if node.End() < len(text) && (text[node.End()] == ' ' || text[node.End()] == '/') {
				zeroWidthBeforeTrivia++
			}
		}
		node.ForEachChild(visit)
		return false
	}
	sourceFile.AsNode().ForEachChild(visit)
	return problems, zeroWidthBeforeTrivia
}
