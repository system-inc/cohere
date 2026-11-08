package consistency_no_abbreviated_identifier

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/naming/abbreviation"
	"github.com/system-inc/cohere/internal/naming/go_origin_names"
)

// errorNameWords are the vocabulary's words as an error's name is built from them, read once from the same file the
// findings are judged by.
var errorNameWords = go_origin_names.VocabularyOf(abbreviation.Policy)

// variableWrites is how a package's code writes one variable: how many times, the right side of the statement that
// defines it, and whether its address is taken, since a write through a pointer is invisible to the count.
type variableWrites struct {
	// parameter marks a function's parameter, which its caller defines, so a write in the body is counted beside no
	// definition.
	parameter bool
	count     int
	// defined marks a variable an assignment or a var declaration defines, and values is that statement's right side.
	defined      bool
	values       []ast.Expr
	addressTaken bool
}

// writesOf counts every write of every variable in the package's files: each assignment's left side, a `:=` that
// reuses a name as well as one that declares it, since go/types resolves both, a var declaration, an increment or
// decrement, and a range clause's key and value. A write inside a closure is a write like any other. It marks every
// parameter too.
func writesOf(context *rule.Context) map[*types.Var]*variableWrites {
	writes := map[*types.Var]*variableWrites{}
	variableOf := func(expression ast.Expr) (*types.Var, *ast.Ident) {
		identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
		if !isIdentifier {
			return nil, nil
		}
		variable, isVariable := context.TypesInfo.ObjectOf(identifier).(*types.Var)
		if !isVariable {
			return nil, nil
		}
		if writes[variable] == nil {
			writes[variable] = &variableWrites{}
		}
		return variable, identifier
	}
	for _, file := range context.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncType:
				// A function's, a literal's, or a function type's on its own, as a method of an interface.
				if typed.Params == nil {
					return true
				}
				for _, field := range typed.Params.List {
					for _, name := range field.Names {
						if variable, _ := variableOf(name); variable != nil {
							writes[variable].parameter = true
						}
					}
				}
			case *ast.AssignStmt:
				for _, left := range typed.Lhs {
					variable, identifier := variableOf(left)
					if variable == nil {
						continue
					}
					writes[variable].count++
					if context.TypesInfo.Defs[identifier] == variable {
						writes[variable].defined = true
						writes[variable].values = typed.Rhs
					}
				}
			case *ast.ValueSpec:
				for _, name := range typed.Names {
					if variable, _ := variableOf(name); variable != nil {
						writes[variable].count++
						writes[variable].defined = true
						writes[variable].values = typed.Values
					}
				}
			case *ast.IncDecStmt:
				if variable, _ := variableOf(typed.X); variable != nil {
					writes[variable].count++
				}
			case *ast.RangeStmt:
				for _, expression := range []ast.Expr{typed.Key, typed.Value} {
					if expression == nil {
						continue
					}
					if variable, _ := variableOf(expression); variable != nil {
						// A range clause writes once per iteration and defines nothing a call names.
						writes[variable].count += 2
					}
				}
			case *ast.UnaryExpr:
				if typed.Op != token.AND {
					return true
				}
				if variable, _ := variableOf(typed.X); variable != nil {
					writes[variable].addressTaken = true
				}
			}
			return true
		})
	}
	return writes
}

// originName is the name where a local error or context came from gives it, with the id of the message that names
// it, when that name is free where the local is declared. An error is named for the call that produced it, and a
// context.Context, matched by its type and never by its name's spelling, for its caller when it is a parameter and
// for the call that made it otherwise. A value named for a call must be written exactly once: one definition whose
// right side is a single call, and no other assignment, increment or address taken anywhere. One written more than
// once is a web of definitions that a single rename can't split, so it is left to the advice, as is one whose call
// names nothing (a constructed error, a context from a call outside the table) or whose name is taken.
func originName(
	context *rule.Context,
	reachable reach,
	writes func() map[*types.Var]*variableWrites,
	identifier *ast.Ident,
) (string, string, bool) {
	variable, isVariable := context.TypesInfo.Defs[identifier].(*types.Var)
	if !isVariable || variable.IsField() || variable.Parent() == nil || variable.Parent() == context.Package.Scope() {
		return "", "", false
	}
	written := writes()[variable]
	newName, messageId := "", ""
	switch {
	case types.Identical(variable.Type(), types.Universe.Lookup("error").Type()):
		if call, isOne := singleCall(written); isOne {
			newName, _, _ = go_origin_names.CallErrorName(context.TypesInfo, call, errorNameWords)
			messageId = namedForItsCall
		}
	case go_origin_names.IsContext(variable.Type()) && written != nil && written.parameter:
		// A parameter the body writes holds the caller's context only until the write, so the caller's name would
		// be wrong after it.
		if written.count == 0 && !written.addressTaken {
			newName, messageId = go_origin_names.CallerContextName, contextNamedForItsOrigin
		}
	case go_origin_names.IsContext(variable.Type()):
		if call, isOne := singleCall(written); isOne {
			newName, _, _ = go_origin_names.ContextCallName(context.TypesInfo, call)
			messageId = contextNamedForItsOrigin
		}
	}
	if newName == "" || newName == identifier.Name || isTakenInGo(reachable, newName) ||
		isDeclaredNear(variable.Parent(), newName) {
		return "", "", false
	}
	return newName, messageId, true
}

// singleCall is the call a variable is written from, when it is written exactly once, by its own definition, whose
// right side is that one call.
func singleCall(written *variableWrites) (*ast.CallExpr, bool) {
	if written == nil || !written.defined || written.count != 1 || written.addressTaken || len(written.values) != 1 {
		return nil, false
	}
	call, isCall := ast.Unparen(written.values[0]).(*ast.CallExpr)
	return call, isCall
}

// isDeclaredNear is whether a name is declared in the scope a local is declared in, in any scope enclosing it, or in
// any scope nested in it. The first would make the rename a redeclaration, the second would shadow a name its uses
// may read after the rename, and the third would capture its uses inside that scope. A nested scope that holds none
// of the local's uses is counted too, which declines a rename the engine's own check would allow, the safe side.
func isDeclaredNear(scope *types.Scope, name string) bool {
	if _, object := scope.LookupParent(name, token.NoPos); object != nil {
		return true
	}
	pending := []*types.Scope{scope}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current.Lookup(name) != nil {
			return true
		}
		for index := range current.NumChildren() {
			pending = append(pending, current.Child(index))
		}
	}
	return false
}
