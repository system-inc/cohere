// Package consistency_no_lonely_if is the Go engine's rule that an else block does not hold an if and nothing else.
//
// It is the Go form of ESLint core's no-lonely-if. An else whose whole body is one if is an else-if written with an
// extra pair of braces: the braces add a level of indentation and no branch, and every condition after the first
// steps right instead of lining up with it. Go allows an init statement after else if as after if, so every such
// block can be written as an else-if with nothing lost.
//
// The core port carries two guards that cannot arise in Go. Its dangling-else guard exists because JavaScript lets
// an if go without braces, so removing a block can hand a later else to a different if; Go requires the braces, so
// every else binds where it is written. Its fixer declines where automatic semicolon insertion would join lines, which
// Go's grammar never does across a closing brace. What is left is the shape itself, judged as the core port judges
// it: the else block's statement list is exactly one if statement.
package consistency_no_lonely_if

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-lonely-if"

// listener hears every if of the syntax dispatcher's walk.
var listener = rule.Listener{
	FileLocal: true, Nodes: []ast.Node{(*ast.IfStmt)(nil)}, Node: judgeIf}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_lonely_if", "Flags an if that is the only statement in "+
	"an else block, since the block is an else-if with an extra pair of braces that adds a level of indentation and "+
	"no branch. An else block holding anything beside the if, a labeled if, or an if in a then branch is left alone.",
	&listener)

// Rule is this rule's entry in the Go engine's registry. It reads only the syntax tree.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer, Listener: &listener}

var lonelyIfMessage = policy.MessageOf(Name, "lonelyIf")

// judgeIf reports the if that is the whole of node's else block.
func judgeIf(context *rule.Context, node ast.Node, _ []ast.Node) {
	statement, isIf := node.(*ast.IfStmt)
	if !isIf {
		return
	}
	// An else-if is already the shape the rule asks for, and the walk going on reaches it to judge its own else, so
	// only a block is read here.
	block, isBlock := statement.Else.(*ast.BlockStmt)
	// Every statement counts, an empty one an explicit semicolon leaves included, as the core port counts them: a
	// block holding anything beside the if is a block an else-if could not replace.
	if !isBlock || len(block.List) != 1 {
		return
	}
	// A labeled if is a LabeledStmt here and is not matched: a goto names the label, and an else-if has no place to
	// put one.
	lonely, isLonely := block.List[0].(*ast.IfStmt)
	if !isLonely {
		return
	}
	// The core port reports the inner if, the statement a reader moves up beside the else.
	context.Report(analysis.Diagnostic{
		Pos:      lonely.Pos(),
		End:      lonely.End(),
		Category: lonelyIfMessage.Id,
		Message:  lonelyIfMessage.Render(map[string]string{}),
	})
}
