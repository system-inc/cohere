package tailwind

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// An index target picks an argument as written and reads a spread as what it spreads, and the call keeps its own
// arguments. The pick was once unspread in place, into what targetItems handed back, a window onto the call's own
// argument list, so the call itself lost its spread for every rule and checker that read the tree after
// (#qb5xxj1, found by TestRulesNeverWriteTheSyntaxTree).
func TestAnIndexTargetLeavesTheCallsArgumentsAsWritten(test *testing.T) {
	test.Parallel()
	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: tspath.RootedFilePath("/repository/source/Component.ts"),
		PathKey:  tspath.PathKey("/repository/source/Component.ts"),
	}, "cn('flex', ...['px-4']);\n", core.ScriptKindTS)
	if sourceFile == nil || len(sourceFile.Statements.Nodes) != 1 ||
		sourceFile.Statements.Nodes[0].Kind != ast.KindExpressionStatement {
		test.Fatal("the fixture did not parse to one statement")
	}
	call := sourceFile.Statements.Nodes[0].AsExpressionStatement().Expression.AsCallExpression()
	spread := call.Arguments.Nodes[1]
	if spread.Kind != ast.KindSpreadElement {
		test.Fatalf("the call's second argument is a %v, not the spread this proves anything about", spread.Kind)
	}

	picked := targetArguments(call.Arguments, &SelectorTarget{Index: 1})
	if len(picked) != 1 || picked[0] != spread.AsSpreadElement().Expression {
		test.Fatalf("the index target picked %v, want the array the spread spreads", picked)
	}
	if call.Arguments.Nodes[1] != spread {
		test.Errorf("after the pick, the call's second argument was a %v, not its own spread",
			call.Arguments.Nodes[1].Kind)
	}
}
