package standing_presence

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_any"
)

// The noany row is a bound, never the rule's flow. consistency-no-any reports an any or an empty interface written in
// a declaration's type unless something in the package fixes that type, and what fixes it is a value a body hands a
// declaration, which the rule's settle then spreads: a fixing place on a variable back along what it was defined from,
// and a value from elsewhere forward along every declaration it feeds (rules/consistency_no_any/flow_analysis.go,
// settle). So a body touches a key whenever it names, calls or builds in a literal a declaration settle routes to the
// key, through any number of declarations of no candidate's own. The row closes the keys the candidates keep under the
// rule's own two tables (consistency_no_any.RecordFlows), recorded over the build's present syntax: every declaration
// and initializer, and every body that does not stand. A declaration defined from a key, or one that feeds a key, is a
// key too, and so on, and a parameter or result that becomes one makes its function one. The bodies that then name a
// key leave, and their flows are read in turn, until the keys stop growing. That is a superset of the bodies the
// rule's flow can route to a key, sound by construction whatever the flow makes of them, and no flow is reimplemented
// here: the edges are the rule's.
//
// Only the declaration-level candidates are read: one written in a body is that body's own, the census's sixth reason,
// which the host's presence record carries. A candidate that keeps no declaration, `type T any` or
// `type F func(any)`, is reported whatever any body does, so it needs no body present.

// noAnyBound is what noany's bound takes out of standing, each with its reason, once the keys are closed under the
// rule's flow: each function a key belongs to is named, so its users leave, and its own body leaves; each field or
// variable key is named, and so is each type a literal of which can mark a field key it never names (holdsKeyField);
// and each used method of an interface method key's name is named, on any receiver, as emptyfunction's are. named are
// the objects the other rows name, whose users leave and so are present, their flows read with the rest.
func noAnyBound(
	pass *analysis.Pass,
	host fact_reach.Host,
	used []fact_reach.UsedObject,
	find func(identity build_reads.Identity) (types.Object, bool),
	standing map[fact_reach.Place]bool,
	named []fact_reach.Named,
) ([]fact_reach.Named, []Leaving) {
	keys := noAnyKeysOf(pass)
	if len(keys.functions)+len(keys.variables)+len(keys.methodNames)+len(keys.literals) == 0 {
		return nil, nil
	}
	syntax := syntaxOf(pass)
	owners := flowOwners(pass)
	// The bodies that leave, the other rows' users among them, which are present and whose flows are read.
	left := map[fact_reach.Place]bool{}
	for _, object := range named {
		for _, user := range host.Users(object.Object) {
			left[user] = true
		}
	}
	var keyNamed []fact_reach.Named
	var leaving []Leaving
	// Each round reads the flows of what is present, closes the keys under them, and takes out what names a key, which
	// is then present. The keys only grow, among the package's finitely many declarations, so the rounds end.
	for {
		edges := consistency_no_any.RecordFlows(pass.TypesInfo, pass.Pkg, pass.Files,
			func(declaration *ast.FuncDecl) bool {
				place := placeOf(syntax, declaration)
				return !standing[place] || left[place]
			})
		grew := keys.close(edges, owners)
		keyNamed, leaving = keys.named(host, used, find)
		for _, body := range leaving {
			left[body.Place] = true
		}
		for _, object := range keyNamed {
			for _, user := range host.Users(object.Object) {
				left[user] = true
			}
		}
		if !grew {
			return keyNamed, leaving
		}
	}
}

// flowOwners are the function declarations, function literals and interface methods of the pass's files by each of
// their parameters and results, so a parameter or result that becomes a key makes what it belongs to one.
func flowOwners(pass *analysis.Pass) map[*types.Var]consistency_no_any.FlowNode {
	owners := map[*types.Var]consistency_no_any.FlowNode{}
	own := func(signature *types.Signature, owner consistency_no_any.FlowNode) {
		for _, tuple := range []*types.Tuple{signature.Params(), signature.Results()} {
			for index := range tuple.Len() {
				owners[tuple.At(index)] = owner
			}
		}
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch declared := node.(type) {
			case *ast.FuncDecl:
				if function, isFunction := pass.TypesInfo.Defs[declared.Name].(*types.Func); isFunction {
					own(function.Signature(), consistency_no_any.FlowNode{Object: function})
				}
			case *ast.FuncLit:
				if signature, isSignature := pass.TypesInfo.TypeOf(declared).(*types.Signature); isSignature {
					own(signature, consistency_no_any.FlowNode{Literal: declared})
				}
			case *ast.InterfaceType:
				if written, isInterface := pass.TypesInfo.TypeOf(declared).(*types.Interface); isInterface {
					for index := range written.NumExplicitMethods() {
						method := written.ExplicitMethod(index)
						own(method.Signature(), consistency_no_any.FlowNode{Object: method})
					}
				}
			}
			return true
		})
	}
	return owners
}

// holdsKeyField is whether a composite literal of declared can hand a value to a field key: one of the struct it
// declares, or of a struct a literal elides inside it, an element, a map's key or a pointer's target, through named
// types too, so `Rows{{value}}` marks a field of Rows's elements, whose type it never names. No other literal reaches a
// key's field. The rule takes a literal's field from the literal's own type, unkeyed by its index
// (recordCompositeLiteral, rules/consistency_no_any/flow_analysis.go:201-235), and keys a field by its object
// (referenceToObject, flow_analysis.go:442-459); go/types makes a struct's fields anew for each struct type written,
// so a literal whose type is written in place, `struct{ rows []any }{value}`, marks fields of its own, never an
// anonymous struct's key. A keyed element and a selection use the field itself, which the host's record holds.
func holdsKeyField(declared types.Type, variables map[*types.Var]bool) bool {
	// Each type is read once, since a named type's elements can name the type again.
	seen := map[types.Type]bool{}
	var holds func(current types.Type) bool
	holds = func(current types.Type) bool {
		if seen[current] {
			return false
		}
		seen[current] = true
		switch underlying := current.Underlying().(type) {
		case *types.Struct:
			for index := range underlying.NumFields() {
				if variables[underlying.Field(index).Origin()] {
					return true
				}
			}
		case *types.Slice:
			return holds(underlying.Elem())
		case *types.Array:
			return holds(underlying.Elem())
		case *types.Map:
			return holds(underlying.Key()) || holds(underlying.Elem())
		case *types.Pointer:
			return holds(underlying.Elem())
		}
		return false
	}
	return holds(declared)
}

// noAnyKeysOf is what every declaration-level candidate of the pass's files keeps. The rule's dispatcher hears a
// candidate anywhere in a file (rules/consistency_no_any/analyzer.go:92-100); the walk here skips every function's
// body, a literal's too, so a candidate is read only where it is written in a declaration's type.
func noAnyKeysOf(pass *analysis.Pass) noAnyKeys {
	keys := noAnyKeys{functions: map[*types.Func]bool{}, variables: map[*types.Var]bool{},
		methodNames: map[string]bool{}, literals: map[*ast.FuncLit]bool{}}
	for _, file := range pass.Files {
		// The ancestors of the node being heard, the file first, as the rule's dispatcher hands them over.
		stack := []ast.Node{}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if _, isBlock := node.(*ast.BlockStmt); isBlock && len(stack) > 0 {
				switch stack[len(stack)-1].(type) {
				case *ast.FuncDecl, *ast.FuncLit:
					// Inspect calls no nil for a node whose children it skips, so it is never pushed.
					return false
				}
			}
			if spellsAny(pass.TypesInfo, node) {
				if declarations, literal, judged := noAnyDeclarations(pass.TypesInfo, node, stack); judged {
					addNoAnyKeys(keys, declarations)
					if literal != nil {
						keys.literals[literal] = true
					}
				}
			}
			stack = append(stack, node)
			return true
		})
	}
	return keys
}

// addNoAnyKeys adds each declaration one candidate keeps to keys, by its kind: an interface's method by its name, any
// other function by its origin, and a variable by its origin.
func addNoAnyKeys(keys noAnyKeys, declarations []types.Object) {
	for _, declaration := range declarations {
		switch declared := declaration.(type) {
		case *types.Func:
			if receiver := declared.Signature().Recv(); receiver != nil && types.IsInterface(receiver.Type()) {
				keys.methodNames[declared.Name()] = true
				continue
			}
			keys.functions[declared.Origin()] = true
		case *types.Var:
			keys.variables[declared.Origin()] = true
		}
	}
}

// spellsAny is whether node is the predeclared any, by its object, or an interface type with an empty method list
// (candidateAt, rules/consistency_no_any/flow_analysis.go:462-485).
func spellsAny(information *types.Info, node ast.Node) bool {
	switch written := node.(type) {
	case *ast.Ident:
		return written.Name == "any" && information.Uses[written] == types.Universe.Lookup("any")
	case *ast.InterfaceType:
		return written.Methods == nil || len(written.Methods.List) == 0
	}
	return false
}

// noAnyDeclarations climbs from a candidate through the type it is written in to the declaration that states it, and
// is each declaration on the way, arm for arm as the rule's declarationsAround does
// (rules/consistency_no_any/flow_analysis.go:491-537). judged is false where the rule's is: the climb left type syntax
// for an expression, or passed through a constraint, a receiver or an interface's embedded element. literal is the
// function literal the climb ended at, a key with no object, and nil for any other end.
func noAnyDeclarations(
	information *types.Info,
	node ast.Node,
	stack []ast.Node,
) ([]types.Object, *ast.FuncLit, bool) {
	declarations := []types.Object{}
	child := node
	for index := len(stack) - 1; index >= 0; index-- {
		switch parent := stack[index].(type) {
		case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.StarExpr, *ast.Ellipsis, *ast.ParenExpr,
			*ast.IndexExpr, *ast.IndexListExpr, *ast.FuncType, *ast.FieldList, *ast.StructType, *ast.InterfaceType:
			// Type syntax, which the climb passes through.
		case *ast.Field:
			fieldDeclarations, judged := noAnyFieldDeclarations(information, parent, stack[:index])
			if !judged {
				return nil, nil, false
			}
			declarations = append(declarations, fieldDeclarations...)
		case *ast.FuncDecl:
			if child != parent.Type {
				return nil, nil, false
			}
			if object := information.Defs[parent.Name]; object != nil {
				declarations = append(declarations, object)
			}
			return declarations, nil, true
		case *ast.FuncLit:
			// A literal is a key of its own, with no object, which a variable defined from it routes to.
			return declarations, parent, child == parent.Type
		case *ast.TypeSpec:
			return declarations, nil, child == parent.Type
		case *ast.ValueSpec:
			if child != parent.Type {
				return nil, nil, false
			}
			for _, name := range parent.Names {
				if object := information.Defs[name]; object != nil {
					declarations = append(declarations, object)
				}
			}
			return declarations, nil, true
		default:
			return nil, nil, false
		}
		child = stack[index]
	}
	return nil, nil, false
}

// noAnyFieldDeclarations are the declarations field states, its ancestors in above, as the rule's fieldDeclarations
// reads them (rules/consistency_no_any/flow_analysis.go:542-590): the parameters or results of a function whose
// signature is known, a struct's fields, or an interface's method. judged is false for a type parameter, a receiver
// and an interface's embedded element.
func noAnyFieldDeclarations(
	information *types.Info,
	field *ast.Field,
	above []ast.Node,
) ([]types.Object, bool) {
	if len(above) < 2 {
		return nil, false
	}
	list, isList := above[len(above)-1].(*ast.FieldList)
	if !isList {
		return nil, false
	}
	switch owner := above[len(above)-2].(type) {
	case *ast.FuncType:
		if list == owner.TypeParams {
			return nil, false
		}
		signature := noAnySignature(information, owner, above[:len(above)-2])
		// A function type written as a type declares no variables.
		if signature == nil {
			return nil, true
		}
		variables := signature.Params()
		if list == owner.Results {
			variables = signature.Results()
		}
		return fieldVariables(list, field, variables.Len(), variables.At), true
	case *ast.StructType:
		structure, isStruct := information.TypeOf(owner).(*types.Struct)
		if !isStruct {
			return nil, true
		}
		return fieldVariables(list, field, structure.NumFields(), structure.Field), true
	case *ast.InterfaceType:
		if len(field.Names) == 0 {
			return nil, false
		}
		if method := information.Defs[field.Names[0]]; method != nil {
			return []types.Object{method}, true
		}
		return nil, true
	}
	return nil, false
}

// noAnySignature is the signature of the function, function literal or interface method functionType belongs to, its
// ancestors in above, or nil for a function type written as a type (signatureOwning,
// rules/consistency_no_any/flow_analysis.go:594-620).
func noAnySignature(information *types.Info, functionType *ast.FuncType, above []ast.Node) *types.Signature {
	if len(above) == 0 {
		return nil
	}
	switch owner := above[len(above)-1].(type) {
	case *ast.FuncDecl:
		if declared, isFunction := information.Defs[owner.Name].(*types.Func); isFunction &&
			owner.Type == functionType {
			return declared.Signature()
		}
	case *ast.FuncLit:
		signature, _ := information.TypeOf(owner).(*types.Signature)
		return signature
	case *ast.Field:
		if len(owner.Names) == 0 || owner.Type != functionType || len(above) < 3 {
			return nil
		}
		if _, isInterface := above[len(above)-3].(*ast.InterfaceType); !isInterface {
			return nil
		}
		if method, isFunction := information.Defs[owner.Names[0]].(*types.Func); isFunction {
			return method.Signature()
		}
	}
	return nil
}

// fieldVariables are the variables field declares, found by position among the count variables of the whole list
// (variablesOf, rules/consistency_no_any/analyzer.go:198-212): a field of several names declares one each, and an
// unnamed or embedded field one.
func fieldVariables(list *ast.FieldList, field *ast.Field, count int, at func(int) *types.Var) []types.Object {
	position := 0
	for _, each := range list.List {
		declared := max(1, len(each.Names))
		if each == field {
			variables := []types.Object{}
			for index := position; index < position+declared && index < count; index++ {
				variables = append(variables, at(index))
			}
			return variables
		}
		position += declared
	}
	return nil
}
