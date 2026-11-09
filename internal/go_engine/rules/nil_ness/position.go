package nil_ness

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// originalPosition is where x/tools reports instruction. staticcheck's instruction holds the syntax it came from, and
// its position is that syntax's start, where x/tools' is mostly the token the syntax turns on, which
// designatedPosition finds. The rest differ by instruction.
func originalPosition(typesInfo *types.Info, instruction intermediate_representation.Instruction) token.Pos {
	switch instruction := instruction.(type) {
	case *intermediate_representation.FieldAddress:
		// staticcheck gives an explicit selection the selected name as its syntax and an implicit one, through an
		// embedded field, the whole selector, whose starts are where x/tools puts each.
		return instruction.Pos()
	case *intermediate_representation.IndexAddress:
		// Both forms index a slice or a pointer to an array ranged over by the loop's own statement, where x/tools
		// places the address at the value ranged over.
		if loop, isLoop := instruction.Source().(*ast.RangeStmt); isLoop {
			return designatedPosition(loop.X)
		}
	case *intermediate_representation.Store:
		return locationPosition(instruction, isDereference)
	case *intermediate_representation.Load:
		// An increment or an operator assignment through a pointer reads the location before it writes it, and
		// staticcheck gives the read the statement as its syntax too.
		switch instruction.Source().(type) {
		case *ast.AssignStmt, *ast.IncDecStmt:
			return locationPosition(instruction, isDereference)
		}
	case *intermediate_representation.MapUpdate:
		return locationPosition(instruction, func(expression ast.Expr) bool {
			index, isIndex := expression.(*ast.IndexExpr)
			return isIndex && isMap(typesInfo, index.X)
		})
	}
	return designatedPosition(instruction.Source())
}

// designatedPosition is the token x/tools positions an instruction built from node at: the left parenthesis of a
// call or a conversion, the selected name of a selection, the left bracket of an index or a slice, the operator of a
// binary expression, the arrow of a send, the left parenthesis of a type assertion, the colon of a keyed element, and
// otherwise node's start.
func designatedPosition(node ast.Node) token.Pos {
	switch node := node.(type) {
	case nil:
		return token.NoPos
	case *ast.ParenExpr:
		return designatedPosition(node.X)
	case *ast.CallExpr:
		return node.Lparen
	case *ast.SelectorExpr:
		return node.Sel.Pos()
	case *ast.IndexExpr:
		return node.Lbrack
	case *ast.IndexListExpr:
		return node.Lbrack
	case *ast.SliceExpr:
		return node.Lbrack
	case *ast.BinaryExpr:
		return node.OpPos
	case *ast.SendStmt:
		return node.Arrow
	case *ast.TypeAssertExpr:
		return node.Lparen
	case *ast.KeyValueExpr:
		return node.Colon
	}
	return node.Pos()
}

// locationPosition is where x/tools reports instruction, which reads or writes the location a left-hand side is, at
// that location's token, where staticcheck positions it at the statement's start, or at the literal's when it writes a
// composite literal in place. isLocation picks the left-hand sides that can be the location; when the statement has
// one, its token is the position, and otherwise the statement's start stands.
func locationPosition(
	instruction intermediate_representation.Instruction,
	isLocation func(expression ast.Expr) bool,
) token.Pos {
	var written []ast.Expr
	switch source := instruction.Source().(type) {
	case *ast.AssignStmt:
		written = source.Lhs
	case *ast.IncDecStmt:
		written = []ast.Expr{source.X}
	case *ast.CompositeLit:
		written = assignedFrom(instruction.Parent(), source)
	}
	var location ast.Expr
	for _, expression := range written {
		if isLocation(ast.Unparen(expression)) {
			if location != nil {
				return instruction.Pos()
			}
			location = ast.Unparen(expression)
		}
	}
	if location == nil {
		return instruction.Pos()
	}
	if dereference, isDereference := location.(*ast.StarExpr); isDereference {
		return dereference.Star
	}
	return designatedPosition(location)
}

// isDereference is whether expression is a pointer dereferenced, `*pointer`.
func isDereference(expression ast.Expr) bool {
	_, isStar := expression.(*ast.StarExpr)
	return isStar
}

// assignedFrom is the left-hand side an assignment in function writes literal to, as one of none or one. staticcheck
// writes a composite literal assigned through a pointer straight into the location, the store's syntax the literal,
// where x/tools builds it aside and copies it in at the left-hand side's position.
func assignedFrom(function *intermediate_representation.Function, literal *ast.CompositeLit) []ast.Expr {
	var written []ast.Expr
	if function.Syntax() == nil {
		return nil
	}
	ast.Inspect(function.Syntax(), func(node ast.Node) bool {
		assignment, isAssignment := node.(*ast.AssignStmt)
		if written != nil || !isAssignment || len(assignment.Lhs) != len(assignment.Rhs) {
			return written == nil
		}
		for index, right := range assignment.Rhs {
			if ast.Unparen(right) == ast.Expr(literal) {
				written = []ast.Expr{assignment.Lhs[index]}
			}
		}
		return written == nil
	})
	return written
}

// isMap is whether expression is of map type.
func isMap(typesInfo *types.Info, expression ast.Expr) bool {
	_, isMapType := coreType(typesInfo.TypeOf(expression)).(*types.Map)
	return isMapType
}
