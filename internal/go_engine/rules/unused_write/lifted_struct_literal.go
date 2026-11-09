package unused_write

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"

	"golang.org/x/tools/go/cfg"

	"github.com/system-inc/cohere/internal/go_engine/core_type"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// liftedStructLiteralWrites is a write for each field given in a pointer to a struct literal, `&T{field: x}`, whose
// allocation staticcheck lifted away, where the original reports each one, with no store or object.
//
// x/tools writes each given field through its own field address, and the original reports them when nothing uses the
// allocation but a store of a whole struct through it, `*pointer = x`. staticcheck stores the literal whole, and when
// every use of the pointer is a load or a store through it, as when the variable holding it is given another value
// before anything reads it, it lifts the allocation into registers: each load becomes the value it reads, and the
// literal's allocation and store are gone, leaving only the literal's value. A lifted literal is judged here, from
// that value and the function's syntax, and a kept one by deadWrites, which judges the store, so nothing is judged
// twice.
//
// The original counts any load as a use, so the fields are reported only where no load can have been. A load that
// read the literal's value became a use of that value, so the value must have none. A load that read a struct stored
// through the pointer later became a use of that struct instead, and a load nothing reads became nothing, so the
// syntax is read too (mayLoadThrough).
//
// Only a literal whose pointer an assignment or a declaration gives straight to a variable, or that a whole struct is
// stored straight through, `*&T{...} = x`, is judged. Any other use of `&T{...}` that staticcheck lifts is a load
// straight through it, `*&T{...}` or a value method called on it, which the original counts as a use.
func liftedStructLiteralWrites(context *rule.Context, function *intermediate_representation.Function) []write {
	body := functionBody(function.Source())
	if body == nil {
		return nil
	}
	// judged is the literals given straight to a variable or stored straight through.
	judged := map[*ast.CompositeLit]bool{}
	given := func(left ast.Expr, right ast.Expr) {
		identifier, isIdentifier := ast.Unparen(left).(*ast.Ident)
		literal, isLiteral := addressedLiteral(context, right)
		if !isIdentifier || !isLiteral {
			return
		}
		if _, isVariable := context.TypesInfo.ObjectOf(identifier).(*types.Var); isVariable {
			judged[literal] = true
		}
	}
	inspectOwnSyntax(context, body, func(node ast.Node) {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			if statement.Tok == token.ASSIGN {
				for _, left := range statement.Lhs {
					// The pointer goes nowhere but the store, so no variable holds it.
					if star, isStar := ast.Unparen(left).(*ast.StarExpr); isStar {
						if literal, isLiteral := addressedLiteral(context, star.X); isLiteral {
							judged[literal] = true
						}
					}
				}
			}
			if (statement.Tok == token.ASSIGN || statement.Tok == token.DEFINE) &&
				len(statement.Lhs) == len(statement.Rhs) {
				for index, left := range statement.Lhs {
					given(left, statement.Rhs[index])
				}
			}
		case *ast.ValueSpec:
			if len(statement.Names) == len(statement.Values) {
				for index, name := range statement.Names {
					given(name, statement.Values[index])
				}
			}
		}
	})
	if len(judged) == 0 {
		return nil
	}
	writes := []write{}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			// Where staticcheck kept the allocation, the literal's value has the store into it as a use.
			value, isComposite := instruction.(*intermediate_representation.CompositeValue)
			if !isComposite || len(*value.Referrers()) > 0 {
				continue
			}
			literal, isLiteral := value.Source().(*ast.CompositeLit)
			if !isLiteral {
				continue
			}
			if !judged[literal] {
				continue
			}
			structure, isStruct := core_type.Of(value.Type()).(*types.Struct)
			if !isStruct || mayLoadThrough(context, function.Source(), literal) {
				continue
			}
			writes = append(writes, givenFieldWrites(literal, value, structure)...)
		}
	}
	return writes
}

// addressedLiteral is the literal expression takes the address of, `&T{...}`, when it does, looked through as
// pointerOperand looks.
func addressedLiteral(context *rule.Context, expression ast.Expr) (*ast.CompositeLit, bool) {
	address, isAddress := pointerOperand(context, expression).(*ast.UnaryExpr)
	if !isAddress || address.Op != token.AND {
		return nil, false
	}
	literal, isLiteral := ast.Unparen(address.X).(*ast.CompositeLit)
	return literal, isLiteral
}

// pointerOperand is expression with each parenthesis, conversion and `&*` it is wrapped in looked through. A conversion
// to the pointer's own type and `&*pointer` may build no instruction, so the pointer itself goes through them, and any
// other conversion of a pointer builds one that keeps the allocation.
func pointerOperand(context *rule.Context, expression ast.Expr) ast.Expr {
	operand := ast.Unparen(expression)
	for {
		switch wrapper := operand.(type) {
		case *ast.CallExpr:
			if len(wrapper.Args) != 1 || !context.TypesInfo.Types[wrapper.Fun].IsType() {
				return operand
			}
			operand = ast.Unparen(wrapper.Args[0])
		case *ast.UnaryExpr:
			star, isStar := ast.Unparen(wrapper.X).(*ast.StarExpr)
			if wrapper.Op != token.AND || !isStar {
				return operand
			}
			operand = ast.Unparen(star.X)
		default:
			return operand
		}
	}
}

// panicBuiltin is the builtin panic, the one call x/tools' form builds nothing after.
var panicBuiltin = types.Universe.Lookup("panic")

// mayLoadThrough is whether the function whose syntax is syntax may load through the pointer to literal in a way
// staticcheck's lifted form no longer shows: a dereference that is not the target of an assignment, `*pointer` read,
// or, where a whole struct is stored through the pointer, a method called through it, which may load what was stored.
//
// It follows the pointer through the function's control flow graph, built by x/tools' go/cfg as lostcancel builds
// one, as a reaching definitions pass: at each point it knows the variables that may hold the pointer there. An
// assignment of `&literal` to a variable puts it there, an assignment from a variable that may hold it copies it, and
// any other assignment to a variable takes it out. A use counts where the expression it goes through is a variable
// that may hold the pointer at that point. Anything else that could hold it, a field, an element, a captured variable,
// an interface, or a call it is handed to, keeps the allocation, which staticcheck then never lifts, so no lifted
// literal's pointer goes there. A function literal's body, which go/cfg leaves out of the graph of the function around
// it and which has a graph of its own, is passed over for the same reason.
//
// The original counts every load its form builds that some path from the entry reaches, and x/tools' form ends a
// block at a call of panic and at no other call, so the graph is cut after panic alone: a load after os.Exit is still
// a use to the original.
func mayLoadThrough(context *rule.Context, syntax ast.Node, literal *ast.CompositeLit) bool {
	graph := cfg.New(functionBody(syntax), func(call *ast.CallExpr) bool {
		callee, isIdentifier := ast.Unparen(call.Fun).(*ast.Ident)
		return !isIdentifier || context.TypesInfo.Uses[callee] != panicBuiltin
	})
	// holding is, at the start of each block, the variables that may hold the pointer there, and nil for a block no
	// path from the entry reaches.
	holding := make([]map[*types.Var]bool, len(graph.Blocks))
	holding[0] = map[*types.Var]bool{}
	pending := []*cfg.Block{graph.Blocks[0]}
	for len(pending) > 0 {
		block := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		held := holding[block.Index]
		for _, node := range block.Nodes {
			held = heldAfter(context, node, literal, held)
		}
		for _, successor := range block.Succs {
			changed := holding[successor.Index] == nil
			if changed {
				holding[successor.Index] = map[*types.Var]bool{}
			}
			for variable := range held {
				if !holding[successor.Index][variable] {
					holding[successor.Index][variable] = true
					changed = true
				}
			}
			if changed {
				pending = append(pending, successor)
			}
		}
	}
	loads, storesThrough, methodCalls := false, false, false
	for _, block := range graph.Blocks {
		held := holding[block.Index]
		if held == nil {
			continue
		}
		for _, node := range block.Nodes {
			// A statement reads what it reads before it assigns anything.
			uses := usesThrough(context, node, held)
			loads = loads || uses.load
			storesThrough = storesThrough || uses.storeThrough
			methodCalls = methodCalls || uses.methodCall
			held = heldAfter(context, node, literal, held)
		}
	}
	return loads || storesThrough && methodCalls
}

// heldAfter is the variables that may hold the pointer to literal once node has made its assignments, where held is
// those that may hold it before.
func heldAfter(
	context *rule.Context,
	node ast.Node,
	literal *ast.CompositeLit,
	held map[*types.Var]bool,
) map[*types.Var]bool {
	var lefts []ast.Expr
	var rights []ast.Expr
	switch statement := node.(type) {
	case *ast.AssignStmt:
		if statement.Tok != token.ASSIGN && statement.Tok != token.DEFINE {
			return held
		}
		lefts, rights = statement.Lhs, statement.Rhs
	case *ast.ValueSpec:
		for _, name := range statement.Names {
			lefts = append(lefts, name)
		}
		rights = statement.Values
	default:
		return held
	}
	// Every value is evaluated before any variable is assigned, and a call's results never hold a lifted pointer.
	carries := make([]bool, len(lefts))
	if len(rights) == len(lefts) {
		for index, right := range rights {
			addressed, isAddressed := addressedLiteral(context, right)
			carries[index] = isAddressed && addressed == literal || mayHold(context, right, held)
		}
	}
	after := maps.Clone(held)
	for index, left := range lefts {
		identifier, isIdentifier := ast.Unparen(left).(*ast.Ident)
		if !isIdentifier {
			continue
		}
		variable, isVariable := context.TypesInfo.ObjectOf(identifier).(*types.Var)
		if !isVariable {
			continue
		}
		if carries[index] {
			after[variable] = true
			continue
		}
		delete(after, variable)
	}
	return after
}

// mayHold is whether expression goes through a variable held says may hold the pointer.
func mayHold(context *rule.Context, expression ast.Expr, held map[*types.Var]bool) bool {
	identifier, isIdentifier := pointerOperand(context, expression).(*ast.Ident)
	if !isIdentifier {
		return false
	}
	variable, isVariable := context.TypesInfo.Uses[identifier].(*types.Var)
	return isVariable && held[variable]
}

// uses is what a node does through the pointer.
type uses struct {
	// load is whether it dereferences the pointer other than as an assignment's target.
	load bool
	// storeThrough is whether it stores a whole struct through the pointer, `*pointer = x`.
	storeThrough bool
	// methodCall is whether it calls a method through the pointer, or takes one as a value.
	methodCall bool
}

// usesThrough is what node, one node of a block, does through a variable held says may hold the pointer, passing
// over the bodies of the function literals in it.
func usesThrough(context *rule.Context, node ast.Node, held map[*types.Var]bool) uses {
	found := uses{}
	// targets is the dereferences an assignment stores through, `*pointer = x`.
	targets := map[*ast.StarExpr]bool{}
	// ast.Inspect visits an assignment before its targets, so they are marked by the time they are visited.
	ast.Inspect(node, func(child ast.Node) bool {
		switch expression := child.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if expression.Tok != token.ASSIGN {
				return true
			}
			for _, left := range expression.Lhs {
				if star, isStar := ast.Unparen(left).(*ast.StarExpr); isStar {
					targets[star] = true
				}
			}
		case *ast.StarExpr:
			if !mayHold(context, expression.X, held) {
				return true
			}
			if targets[expression] {
				found.storeThrough = true
				return true
			}
			found.load = true
		case *ast.SelectorExpr:
			selection, isSelection := context.TypesInfo.Selections[expression]
			if isSelection && selection.Kind() != types.FieldVal && mayHold(context, expression.X, held) {
				found.methodCall = true
			}
		}
		return true
	})
	return found
}
