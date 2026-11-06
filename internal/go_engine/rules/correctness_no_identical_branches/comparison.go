package correctness_no_identical_branches

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
)

// comparison walks two branches in parallel and says whether they are the same code. It is used for one pair of
// branches and then dropped, since counterparts holds what that pair's sides declared.
type comparison struct {
	information *types.Info
	// counterparts maps an object the left side declares to the one the right side declares in the same place, so a
	// use on each side of what its own side declared counts as the same name.
	counterparts map[types.Object]types.Object
}

func (comparison *comparison) sameStatements(left []ast.Stmt, right []ast.Stmt) bool {
	return comparison.sameValue(reflect.ValueOf(left), reflect.ValueOf(right))
}

func (comparison *comparison) same(left ast.Node, right ast.Node) bool {
	return comparison.sameValue(reflect.ValueOf(left), reflect.ValueOf(right))
}

// sameValue compares two pieces of syntax tree field by field. Reflection reaches every field of every node kind,
// one go/ast adds later included, where a hand-written switch over the kinds would silently skip a field it never
// named and could call two different trees the same. A kind of value the tree does not hold today counts as a
// difference, so a newer go/ast can only make the rule miss.
func (comparison *comparison) sameValue(left reflect.Value, right reflect.Value) bool {
	if left.Type() != right.Type() {
		return false
	}
	switch left.Type() {
	case positionType:
		// Where a token sits is layout, but whether it is there at all is not: `f(values...)` differs from
		// `f(values)` only in its Ellipsis position being set.
		return (left.Int() == int64(token.NoPos)) == (right.Int() == int64(token.NoPos))
	case commentGroupType:
		return true
	case objectType, scopeType:
		// The parser's old name resolution, superseded by pass.TypesInfo, and its objects point back into the tree.
		return true
	}
	switch left.Kind() {
	case reflect.Pointer, reflect.Interface:
		if left.IsNil() || right.IsNil() {
			return left.IsNil() == right.IsNil()
		}
		if leftIdentifier, isIdentifier := left.Interface().(*ast.Ident); isIdentifier {
			rightIdentifier, isRightIdentifier := right.Interface().(*ast.Ident)
			return isRightIdentifier && comparison.sameIdentifier(leftIdentifier, rightIdentifier)
		}
		if leftClause, isClause := left.Interface().(*ast.CaseClause); isClause {
			if rightClause, isRightClause := right.Interface().(*ast.CaseClause); isRightClause {
				comparison.pairImplicits(leftClause, rightClause)
			}
		}
		return comparison.sameValue(left.Elem(), right.Elem())
	case reflect.Struct:
		for index := range left.NumField() {
			if !comparison.sameValue(left.Field(index), right.Field(index)) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if left.Len() != right.Len() {
			return false
		}
		for index := range left.Len() {
			if !comparison.sameValue(left.Index(index), right.Index(index)) {
				return false
			}
		}
		return true
	case reflect.String:
		return left.String() == right.String()
	case reflect.Bool:
		return left.Bool() == right.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return left.Int() == right.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return left.Uint() == right.Uint()
	default:
		return false
	}
}

// sameIdentifier is whether two names are spelled the same and denote the same thing: a declaration on each side,
// which then become counterparts, or a use of the same object, or of counterparts.
func (comparison *comparison) sameIdentifier(left *ast.Ident, right *ast.Ident) bool {
	if left.Name != right.Name {
		return false
	}
	leftDeclared, isLeftDeclaration := comparison.information.Defs[left]
	rightDeclared, isRightDeclaration := comparison.information.Defs[right]
	if isLeftDeclaration || isRightDeclaration {
		if !isLeftDeclaration || !isRightDeclaration {
			return false
		}
		// A declaring name with no object, the variable of a type switch guard, has its objects on the clauses.
		if leftDeclared == nil || rightDeclared == nil {
			return leftDeclared == rightDeclared
		}
		comparison.counterparts[leftDeclared] = rightDeclared
		return true
	}
	leftUsed := comparison.information.Uses[left]
	rightUsed := comparison.information.Uses[right]
	// A name the checker records nowhere is compared by its spelling alone, which the check above already did.
	if leftUsed == nil || rightUsed == nil {
		return leftUsed == rightUsed
	}
	if counterpart, isCounterpart := comparison.counterparts[leftUsed]; isCounterpart {
		return counterpart == rightUsed
	}
	return leftUsed == rightUsed
}

// pairImplicits makes the variables two matching type switch clauses declare implicitly into counterparts. Both
// clauses sit inside the branches being compared, in the same place, so a use of each clause's own variable is the
// same name. The clauses of the switch being judged are never paired, since its bodies are compared, not its clauses.
func (comparison *comparison) pairImplicits(left *ast.CaseClause, right *ast.CaseClause) {
	leftImplicit := comparison.information.Implicits[left]
	rightImplicit := comparison.information.Implicits[right]
	if leftImplicit != nil && rightImplicit != nil {
		comparison.counterparts[leftImplicit] = rightImplicit
	}
}
