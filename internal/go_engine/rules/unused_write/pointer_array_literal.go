package unused_write

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/core_type"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// reportLiftedArrayLiterals reports the element of a pointer to an array literal of one element, `values :=
// &[1]int{x}`, that nothing reads, where staticcheck's form no longer holds the write.
//
// x/tools writes the literal's element through an index address, and the original reports it when nothing else uses
// the array. staticcheck stores a literal that gives every element whole, and when every use of the pointer is a load
// or a store through it, or none at all, as with len and cap, which read no element and build no instruction, it
// lifts the allocation into registers and leaves no store to judge. Where the allocation stays, deadWrites judges the
// literal's element itself (arrayLiteralElementWrite), so this reports only a literal staticcheck built no
// allocation for, and nothing twice.
//
// With the allocation lifted, the only use the original counts that can remain is a load of the whole array, `*values`
// read, so the element is reported unless some use of the variable may read: every use but an argument of len or cap,
// the index alone of a loop ranging over it, a store of a whole array through it, `*values = x`, an assignment of
// another pointer to the variable, and a copy of it into another variable, whose uses count as its own. A use the
// syntax cannot place among those counts as a read, so the check never reports what the original does not.
func reportLiftedArrayLiterals(context *rule.Context, function *intermediate_representation.Function) {
	syntax := function.Source()
	body := functionBody(syntax)
	if body == nil {
		return
	}
	bound := map[*types.Var]*ast.CompositeLit{}
	inspectOwnSyntax(context, body, func(node ast.Node) {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			if statement.Tok != token.DEFINE || len(statement.Lhs) != len(statement.Rhs) {
				return
			}
			for index, left := range statement.Lhs {
				if variable, literal, isBound := boundArrayLiteral(context, left, statement.Rhs[index]); isBound {
					bound[variable] = literal
				}
			}
		case *ast.ValueSpec:
			if len(statement.Names) != len(statement.Values) {
				return
			}
			for index, name := range statement.Names {
				if variable, literal, isBound := boundArrayLiteral(context, name, statement.Values[index]); isBound {
					bound[variable] = literal
				}
			}
		}
	})
	if len(bound) == 0 {
		return
	}
	// A literal staticcheck kept an allocation for is deadWrites' to judge.
	allocated := map[ast.Node]bool{}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			if allocation, isAllocation := instruction.(*intermediate_representation.Allocation); isAllocation {
				allocated[allocation.Source()] = true
			}
		}
	}
	readers := readersOf(context, syntax, bound)
	for variable, literal := range bound {
		if allocated[literal] || readers[variable] {
			continue
		}
		position, index, isReported := arrayLiteralElement(context, literal)
		if isReported {
			context.Report(analysis.Diagnostic{Pos: position, Message: "unused write to array index " + index})
		}
	}
}

// boundArrayLiteral is the variable left declares and the array literal of one element value takes the address of,
// when it does.
func boundArrayLiteral(context *rule.Context, left ast.Expr, value ast.Expr) (*types.Var, *ast.CompositeLit, bool) {
	identifier, isIdentifier := left.(*ast.Ident)
	if !isIdentifier {
		return nil, nil, false
	}
	variable, isVariable := context.TypesInfo.Defs[identifier].(*types.Var)
	if !isVariable {
		return nil, nil, false
	}
	address, isAddress := ast.Unparen(value).(*ast.UnaryExpr)
	if !isAddress || address.Op != token.AND {
		return nil, nil, false
	}
	literal, isLiteral := ast.Unparen(address.X).(*ast.CompositeLit)
	if !isLiteral {
		return nil, nil, false
	}
	array, isArray := core_type.Of(context.TypesInfo.TypeOf(literal)).(*types.Array)
	if !isArray || array.Len() != 1 || len(literal.Elts) != 1 {
		return nil, nil, false
	}
	return variable, literal, true
}

// functionBody is the statements a function's syntax holds: a declared function's body, a function literal's, or for
// the function staticcheck makes of a loop over an iterator function, the loop's body.
func functionBody(syntax ast.Node) *ast.BlockStmt {
	switch function := syntax.(type) {
	case *ast.FuncDecl:
		return function.Body
	case *ast.FuncLit:
		return function.Body
	case *ast.RangeStmt:
		return function.Body
	}
	return nil
}

// inspectOwnSyntax calls visit on every node of body that belongs to the function itself, leaving out the function
// literals and the bodies of loops over iterator functions inside it, each a function of its own that is judged on its
// own.
func inspectOwnSyntax(context *rule.Context, body *ast.BlockStmt, visit func(node ast.Node)) {
	ast.Inspect(body, func(node ast.Node) bool {
		switch syntax := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.RangeStmt:
			if _, isIterator := core_type.Of(context.TypesInfo.TypeOf(syntax.X)).(*types.Signature); isIterator {
				visit(syntax)
				for _, part := range []ast.Expr{syntax.Key, syntax.Value, syntax.X} {
					if part != nil {
						ast.Inspect(part, func(node ast.Node) bool {
							if node != nil {
								visit(node)
							}
							return true
						})
					}
				}
				return false
			}
		}
		visit(node)
		return true
	})
}

// readersOf is the variables of bound some use may read through, read from the whole of the function's syntax, its
// function literals included, since a literal that only takes the length captures nothing in either form. A variable
// that copies the pointer, `other := values`, is the same value in either form, so its uses are the bound variable's
// uses too.
func readersOf(context *rule.Context, syntax ast.Node, bound map[*types.Var]*ast.CompositeLit) map[*types.Var]bool {
	harmless := map[*ast.Ident]bool{}
	harmlessUse := func(expression ast.Expr) {
		if identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident); isIdentifier {
			harmless[identifier] = true
		}
	}
	// A copy of the pointer into another variable reads nothing, and the copy's uses count as the bound variable's.
	copies, sources := copiesOf(context, syntax)
	for source := range sources {
		harmless[source] = true
	}
	ast.Inspect(syntax, func(node ast.Node) bool {
		switch construct := node.(type) {
		case *ast.CallExpr:
			callee, isCallee := ast.Unparen(construct.Fun).(*ast.Ident)
			if !isCallee || len(construct.Args) != 1 {
				return true
			}
			builtin, isBuiltin := context.TypesInfo.Uses[callee].(*types.Builtin)
			if isBuiltin && (builtin.Name() == "len" || builtin.Name() == "cap") {
				harmlessUse(construct.Args[0])
			}
		case *ast.RangeStmt:
			if construct.Value == nil || isBlank(construct.Value) {
				harmlessUse(construct.X)
			}
		case *ast.AssignStmt:
			if construct.Tok != token.ASSIGN {
				return true
			}
			for _, left := range construct.Lhs {
				if star, isStar := ast.Unparen(left).(*ast.StarExpr); isStar {
					harmlessUse(star.X)
					continue
				}
				harmlessUse(left)
			}
		}
		return true
	})
	// sharing maps each variable that holds a bound variable's pointer to the bound variable.
	sharing := map[*types.Var]*types.Var{}
	for variable := range bound {
		pending := []*types.Var{variable}
		for len(pending) > 0 {
			holder := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if _, isSeen := sharing[holder]; isSeen {
				continue
			}
			sharing[holder] = variable
			pending = append(pending, copies[holder]...)
		}
	}
	readers := map[*types.Var]bool{}
	ast.Inspect(syntax, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier || harmless[identifier] {
			return true
		}
		variable, isVariable := context.TypesInfo.Uses[identifier].(*types.Var)
		if isVariable && sharing[variable] != nil {
			readers[sharing[variable]] = true
		}
		return true
	})
	return readers
}

// copiesOf maps each variable syntax assigns to another, `other := values`, to the variables assigned its value, and
// gives the identifiers each such copy reads.
func copiesOf(context *rule.Context, syntax ast.Node) (map[*types.Var][]*types.Var, map[*ast.Ident]bool) {
	copies := map[*types.Var][]*types.Var{}
	sources := map[*ast.Ident]bool{}
	copied := func(left ast.Expr, right ast.Expr) {
		leftIdentifier, isLeftIdentifier := ast.Unparen(left).(*ast.Ident)
		rightIdentifier, isRightIdentifier := ast.Unparen(right).(*ast.Ident)
		if !isLeftIdentifier || !isRightIdentifier || leftIdentifier.Name == "_" {
			return
		}
		target, isTarget := context.TypesInfo.ObjectOf(leftIdentifier).(*types.Var)
		source, isSource := context.TypesInfo.Uses[rightIdentifier].(*types.Var)
		if isTarget && isSource {
			copies[source] = append(copies[source], target)
			sources[rightIdentifier] = true
		}
	}
	ast.Inspect(syntax, func(node ast.Node) bool {
		switch construct := node.(type) {
		case *ast.ValueSpec:
			if len(construct.Names) == len(construct.Values) {
				for index, name := range construct.Names {
					copied(name, construct.Values[index])
				}
			}
		case *ast.AssignStmt:
			if (construct.Tok == token.ASSIGN || construct.Tok == token.DEFINE) &&
				len(construct.Lhs) == len(construct.Rhs) {
				for index, left := range construct.Lhs {
					copied(left, construct.Rhs[index])
				}
			}
		}
		return true
	})
	return copies, sources
}

// isBlank is whether expression is the blank identifier.
func isBlank(expression ast.Expr) bool {
	identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	return isIdentifier && identifier.Name == "_"
}

// arrayLiteralElement is where the original reports the one element of literal, at the colon of a keyed element and
// the start of a positional one, and how the port names its index, as indexText names one: a keyed element's key as
// written, and a positional one's by its value, 0.
func arrayLiteralElement(context *rule.Context, literal *ast.CompositeLit) (token.Pos, string, bool) {
	if len(literal.Elts) != 1 {
		return token.NoPos, "", false
	}
	element := literal.Elts[0]
	keyed, isKeyed := element.(*ast.KeyValueExpr)
	if !isKeyed {
		// A positional element has no index in its source, so it is named by its value (see indexText).
		return element.Pos(), "0", true
	}
	key := context.TypesInfo.Types[keyed.Key]
	if key.Value == nil || key.Type == nil {
		return token.NoPos, "", false
	}
	return keyed.Colon, sourceText(context, keyed.Key), true
}

// arrayLiteralElementWrite is the write of the one element of an array literal of one element store writes whole into
// the allocation the literal makes, where staticcheck kept that allocation: x/tools writes the element through its
// index address. A literal of more elements is judged by none of them, since x/tools' address for each element is a
// use of the array to every other element's write.
func arrayLiteralElementWrite(
	store *intermediate_representation.Store,
	allocation *intermediate_representation.Allocation,
) (write, bool) {
	literal, value, isLiteral := literalStoredWhole(store)
	if !isLiteral {
		return write{}, false
	}
	array, isArray := core_type.Of(value.Type()).(*types.Array)
	if !isArray || array.Len() != 1 || len(literal.Elts) != 1 {
		return write{}, false
	}
	return write{store: store, object: allocation, field: notAField, arrayLiteral: literal}, true
}
