// Package consistency_no_else_return is the Go engine's rule that an else does not follow an if branch that always
// returns.
//
// It is the Go form of ESLint core's no-else-return, and Go's own idiom agrees: the happy path runs down the left
// edge and an early return reads as the exception it is. Code after an if whose branch returns runs only when the
// branch did not, which is what the else says, so the else costs a level of indentation and tells the reader nothing.
//
// The judgment follows the core port's default, allowElseIf: an else-if chain is one judgment, every branch of it
// must return, and only then is its final else reported. Where the core port reads "returns" as "has a return
// somewhere in the block", this reads it as Go's own terminating statement narrowed to return: the branch's last
// statement is a return, a block ending in one, or an if and else that both do. Control cannot fall out of such a
// branch, so a finding is never wrong about the else being dead weight.
package consistency_no_else_return

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-else-return"

// listener hears every if of the syntax dispatcher's walk, with its parent, which says whether it heads a chain.
var listener = rule.Listener{
	FileLocal: true, Nodes: []ast.Node{(*ast.IfStmt)(nil)}, Node: judgeIf}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_else_return", "Flags the final else of an if or else-if "+
	"chain whose every branch always returns, since the code after the chain runs only when no branch returned and "+
	"the else adds a level of indentation to say nothing. An else that reads a variable declared in an if's init "+
	"statement is left alone, since dropping it would mean widening that variable's scope, and so is a labeled if.",
	&listener)

// Rule is this rule's entry in the Go engine's registry. It reads type information to tell whether an else uses a
// variable an if's init statement declared.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var elseAfterReturnMessage = policy.MessageOf(Name, "elseAfterReturn")

// judgeIf judges the chain node heads. An if that is another if's else, or that a label holds, is no chain head and
// is never judged on its own; its parent, the last of its ancestors, says which it is. An if nested inside a branch
// heads a chain of its own, and the walk going on reaches it.
func judgeIf(context *rule.Context, node ast.Node, stack []ast.Node) {
	statement, isIf := node.(*ast.IfStmt)
	if !isIf {
		return
	}
	if len(stack) > 0 {
		switch parent := stack[len(stack)-1].(type) {
		case *ast.IfStmt:
			if parent.Else == ast.Stmt(statement) {
				return
			}
		case *ast.LabeledStmt:
			// The core port judges only an if whose parent holds a statement list, and a label holds one statement.
			// Go would allow the unwrap here, but a labeled if is rare enough that matching upstream's silence costs
			// nothing and spares a goto target's reader a surprise.
			return
		}
	}
	judgeChain(context, statement)
}

// judgeChain reports the final else of the chain that head opens when every branch before it always returns. A chain
// with no final else, or with any branch that can fall through, is left alone, as the core port leaves it.
func judgeChain(context *rule.Context, head *ast.IfStmt) {
	// Every variable an init statement in the chain declares is in scope in the final else, and nowhere after the
	// chain, so those are the ones an unwrapped else would lose.
	declaredByInit := map[types.Object]bool{}
	for current := head; ; {
		recordInitDeclarations(context, current, declaredByInit)
		if current.Else == nil || !alwaysReturns(current.Body) {
			return
		}
		switch alternate := current.Else.(type) {
		case *ast.IfStmt:
			current = alternate
		case *ast.BlockStmt:
			if usesAny(context, alternate, declaredByInit) {
				return
			}
			context.Report(analysis.Diagnostic{
				Pos:      alternate.Lbrace,
				End:      alternate.Rbrace + 1,
				Category: elseAfterReturnMessage.Id,
				Message:  elseAfterReturnMessage.Render(map[string]string{}),
			})
			return
		default:
			// The parser puts only an if or a block after else, so anything else is skipped rather than guessed at.
			return
		}
	}
}

// recordInitDeclarations adds to declared every variable that statement's init statement declares. Only a short
// variable declaration declares in an init; an assignment, a send or a call there names nothing new.
//
// Mutates: declared gains each variable the init declares, so the caller's set covers every init of the chain
func recordInitDeclarations(context *rule.Context, statement *ast.IfStmt, declared map[types.Object]bool) {
	assignment, isAssignment := statement.Init.(*ast.AssignStmt)
	if !isAssignment {
		return
	}
	for _, expression := range assignment.Lhs {
		identifier, isIdentifier := expression.(*ast.Ident)
		if !isIdentifier {
			continue
		}
		// Defs holds only what this statement declares, and a blank identifier declares nothing, so neither an
		// assignment's targets nor `_` is recorded.
		if object := context.TypesInfo.Defs[identifier]; object != nil {
			declared[object] = true
		}
	}
}

// usesAny is whether any identifier inside block resolves to one of objects. A use inside a function literal counts,
// since the literal still needs the variable in scope where it is written.
func usesAny(context *rule.Context, block *ast.BlockStmt, objects map[types.Object]bool) bool {
	if len(objects) == 0 {
		return false
	}
	found := false
	ast.Inspect(block, func(node ast.Node) bool {
		if found {
			return false
		}
		if identifier, isIdentifier := node.(*ast.Ident); isIdentifier && objects[context.TypesInfo.Uses[identifier]] {
			found = true
		}
		return !found
	})
	return found
}

// alwaysReturns is whether control can leave statement only by returning: Go's terminating statement, narrowed to
// return. A return, a block whose last non-empty statement always returns, a labeled statement that does, and an if
// with an else whose branches both do. A panic, a call to os.Exit, a goto, a loop or a switch is not read, since the
// rule is about return, and a loop or switch would need its every break traced to be sure; leaving them out can only
// miss an else, never flag a needed one.
func alwaysReturns(statement ast.Stmt) bool {
	switch statement := statement.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		// The last statement, not any: a statement after a return is unreachable, and the spec reads a block by its
		// end. An explicit semicolon leaves an empty statement behind, which says nothing about where control goes.
		for index := len(statement.List) - 1; index >= 0; index-- {
			if _, isEmpty := statement.List[index].(*ast.EmptyStmt); isEmpty {
				continue
			}
			return alwaysReturns(statement.List[index])
		}
		return false
	case *ast.LabeledStmt:
		return alwaysReturns(statement.Stmt)
	case *ast.IfStmt:
		return statement.Else != nil && alwaysReturns(statement.Body) && alwaysReturns(statement.Else)
	}
	return false
}
