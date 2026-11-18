package unread_assigned_value

import (
	"go/ast"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// functionWalker walks one function's syntax once, gathering its switches' tags and judging each assignment and each
// increment or decrement against the tags gathered so far, keeping as unread each value none of them reads.
type functionWalker struct {
	function *intermediate_representation.Function
	// switchTags are the values the function's switches compare their cases against. A switch's tag is read by its
	// cases, which the representation compares against it without a referrer hasUse would see. A switch with no tag
	// has the nil value, which no value asked about is, so it adds nothing, and a function without a tagged switch
	// leaves the slice nil. A function has few switches, so a scan of the slice costs less than a map would.
	switchTags []intermediate_representation.Value
	// unread are the values the walk found nothing reading, by the tags it had gathered when it judged each, in the
	// order it judged them, which Check judges again against every tag.
	unread []unreadValue
}

// unreadValue is a value the walk found nothing reading: the statement that gives it, the expression it is the value
// of, which the message names, and the value.
type unreadValue struct {
	statement ast.Node
	assigned  ast.Expr
	value     intermediate_representation.Value
}

// judge keeps value as unread when nothing reads it, by the tags gathered so far.
func (walker *functionWalker) judge(statement ast.Node, assigned ast.Expr, value intermediate_representation.Value) {
	if !hasUse(value, walker.switchTags) {
		walker.unread = append(walker.unread, unreadValue{statement: statement, assigned: assigned, value: value})
	}
}

// Visit gathers a switch's tag and judges an assignment, an increment or a decrement, and walks on into every node's
// children but a nested function literal's.
//
// The original's ast.Inspect walks into nested literals too, and finds nothing there: the builder lowers a literal's
// body into the literal's own function, so every expression inside it is recorded in that function's expression map
// and none in the enclosing one's, a captured variable included, which the literal reads as a free variable of its
// own. Every value the enclosing function asks for in there is nil, which adds no tag and judges nothing, and the
// literal is heard as a source function of its own, whose walk judges its assignments. So no finding can change. A
// function whose syntax is itself a literal is walked from that literal, its root.
func (walker *functionWalker) Visit(node ast.Node) ast.Visitor {
	if _, isLiteral := node.(*ast.FuncLit); isLiteral && node != walker.function.Source() {
		return nil
	}
	switch statement := node.(type) {
	case *ast.SwitchStmt:
		if tag, _ := walker.function.ValueForExpression(statement.Tag); tag != nil {
			walker.switchTags = append(walker.switchTags, tag)
		}
	case *ast.IncDecStmt:
		checkIncrement(walker, statement)
	case *ast.AssignStmt:
		checkAssignment(walker, statement)
	}
	return walker
}
