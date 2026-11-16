package unread_assigned_value

import (
	"go/ast"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// functionWalker walks one function's syntax twice: first gathering its switches' tags, then, once hasSwitchTags is
// set, judging each assignment and each increment or decrement against them.
type functionWalker struct {
	context  *rule.Context
	function *intermediate_representation.Function
	// switchTags are the values the function's switches compare their cases against. A switch's tag is read by its
	// cases, which the representation compares against it without a referrer hasUse would see. A switch with no tag
	// has the nil value, which no value asked about is, so it adds nothing, and a function without a tagged switch
	// leaves the slice nil. A function has few switches, so a scan of the slice costs less than a map would.
	switchTags    []intermediate_representation.Value
	hasSwitchTags bool
}

// Visit gathers a switch's tag in the first walk and judges an assignment, an increment or a decrement in the second,
// and walks on into every node's children, as the original's ast.Inspect does.
func (walker *functionWalker) Visit(node ast.Node) ast.Visitor {
	if !walker.hasSwitchTags {
		if statement, isSwitch := node.(*ast.SwitchStmt); isSwitch {
			if tag, _ := walker.function.ValueForExpression(statement.Tag); tag != nil {
				walker.switchTags = append(walker.switchTags, tag)
			}
		}
		return walker
	}
	switch statement := node.(type) {
	case *ast.IncDecStmt:
		checkIncrement(walker.context, walker.function, statement, walker.switchTags)
	case *ast.AssignStmt:
		checkAssignment(walker.context, walker.function, statement, walker.switchTags)
	}
	return walker
}
