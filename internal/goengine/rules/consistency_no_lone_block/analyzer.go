// Package consistency_no_lone_block is the Go engine's rule that a bare block earns its braces by scoping something.
//
// It is the Go form of ESLint core's no-lone-blocks. A block standing on its own among statements opens a scope, and
// in Go that is sometimes the point: braces around a `:=` keep a short-lived variable from leaking into the rest of
// the function. A bare block that declares nothing directly inside it scopes nothing, so its braces only indent the
// code and suggest a grouping the language does not keep. A `defer` inside one still runs when the function returns,
// not when the block ends, which is the misreading the braces invite.
//
// The judgment is narrower than the core port's on purpose. The core port also reports a block that holds a binding
// when it is the only statement of its enclosing block; this rule reports only a block that declares nothing, since a
// block that declares something is doing the one thing Go uses bare blocks for, and a reader's choice of where to put
// that scope is not a defect. A labeled block is a goto target and is left alone too.
package consistency_no_lone_block

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-lone-block"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_lone_block",
	Doc: "Flags a bare { } block standing among statements that declares nothing directly inside it, no :=, var, " +
		"const or type, and carries no label, since such braces scope nothing and only indent the code. A bare " +
		"block that declares something is Go's way of scoping a variable and is left alone.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. A bare block and what it declares are syntax, so the rule
// reads no types.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var redundantBlockMessage = policy.MessageOf(Name, "redundantBlock")

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// The walk reaches every statement list, the ones inside function literals included. A block reached any
		// other way is one the syntax requires, a function, if, else, for, switch or select body, and is never in a
		// list, so it is never judged. A labeled block is the LabeledStmt's statement rather than a list's member, so
		// it is never judged either.
		ast.Inspect(file, func(node ast.Node) bool {
			switch holder := node.(type) {
			case *ast.BlockStmt:
				judgeList(pass, holder.List)
			case *ast.CaseClause:
				// A case clause is already a scope of its own in Go, so braces wrapping a whole case add a scope only
				// when they declare something, and are judged the same as anywhere else.
				judgeList(pass, holder.Body)
			case *ast.CommClause:
				judgeList(pass, holder.Body)
			}
			return true
		})
	}
	return nil, nil
}

// judgeList reports every bare block among statements that declares nothing directly inside it.
func judgeList(pass *analysis.Pass, statements []ast.Stmt) {
	for _, statement := range statements {
		block, isBlock := statement.(*ast.BlockStmt)
		if !isBlock || declaresSomething(block) {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:      block.Pos(),
			End:      block.End(),
			Category: redundantBlockMessage.Id,
			Message:  redundantBlockMessage.Render(nil),
		})
	}
}

// declaresSomething reports whether a statement directly inside the block binds a name to the block's scope.
//
// Directly, not recursively: a `:=` in an if's init, a for's clause or a nested block belongs to that statement's own
// scope, and a `:=` inside a function literal belongs to the literal, so none of them makes these braces load-bearing.
// A label inside the block binds to the whole function, as Go's labels do, so it does not count either.
func declaresSomething(block *ast.BlockStmt) bool {
	for _, statement := range block.List {
		switch declaring := statement.(type) {
		case *ast.AssignStmt:
			// A short variable declaration always declares at least one new name, and inside the block every name
			// on its left is new to the block's scope.
			if declaring.Tok == token.DEFINE {
				return true
			}
		case *ast.DeclStmt:
			// Inside a body a DeclStmt holds only a var, const or type declaration, each of which binds to the
			// block. A function declaration cannot stand inside a body, so there is no fourth kind to consider.
			return true
		}
	}
	return false
}
