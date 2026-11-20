package standing_presence

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// The noany row is a bound, never the rule's flow. consistency-no-any reports an any or an empty interface written in
// a declaration's type unless something in the package fixes that type, and what fixes it is a value a body hands the
// declaration, read by a whole-package flow of six node kinds (the report on it, #xeafx79, sections 2 and 8). A record
// of that flow per body is no small thing, and no set of used objects can say it. What a set of used objects can say
// is which bodies could touch a key at all: a body marks or feeds a key only if it is the key's own function, names or
// calls that function, names the key, builds the key's struct type in a literal, or selects a method of an interface
// method key's name. So every such body is kept present: a superset of the bodies the rule's flow reads, which is
// sound by construction, whatever the flow makes of them, and no flow is reimplemented here.
//
// Only the declaration-level candidates are read: one written in a body is that body's own, the census's sixth reason,
// which wave 3's record carries. A candidate that keeps no declaration, `type T any` or `type F func(any)`, is reported
// whatever any body does, so it needs no body present.

// noAnyKeys are the declarations the package's declaration-level candidates keep, as the rule keys each, by its
// generic origin: the functions a parameter or result key belongs to, the variables kept, fields, parameters, results
// and package-level variables among them, and the names of the interface methods kept.
type noAnyKeys struct {
	functions   map[*types.Func]bool
	variables   map[*types.Var]bool
	methodNames map[string]bool
}

// noAnyBound is what noany's bound takes out of the standing set, each with its reason: each function a key belongs
// to is named, so its users leave, and its own body leaves; each field or variable key is named, and so is each struct
// type that holds a field key, since a body building it in a literal can mark a field it never names; and each used
// method of an interface method key's name is named, on any receiver, as emptyfunction's are.
func noAnyBound(
	pass *analysis.Pass,
	host fact_reach.Host,
	used []fact_reach.UsedObject,
	find func(identity build_reads.Identity) (types.Object, bool),
) ([]fact_reach.Named, []Leaving) {
	keys := noAnyKeysOf(pass)
	if len(keys.functions) == 0 && len(keys.variables) == 0 && len(keys.methodNames) == 0 {
		return nil, nil
	}
	var named []fact_reach.Named
	var leaving []Leaving
	if len(keys.functions) > 0 {
		// The place of each function the host declares with a body, by its key, as buildOrderNamed maps one to the
		// other.
		places := map[fact_reach.Object]fact_reach.Place{}
		for _, place := range host.Places() {
			for _, key := range host.Declared(place) {
				places[key] = place
			}
		}
		for _, function := range host.Functions() {
			found, isFound := find(function.Identity)
			declared, isFunction := found.(*types.Func)
			if !isFound || !isFunction || !keys.functions[declared.Origin()] {
				continue
			}
			named = append(named, fact_reach.Named{Object: function.Object, Reason: NoAny.Reason()})
			if place, isPlaced := places[function.Object]; isPlaced {
				leaving = append(leaving, Leaving{Place: place, Reason: NoAny.Reason()})
			}
		}
	}
	for _, object := range used {
		found, isFound := find(object.Identity)
		if !isFound {
			continue
		}
		isKey := false
		switch declared := found.(type) {
		case *types.Var:
			isKey = keys.variables[declared.Origin()]
		case *types.TypeName:
			isKey = holdsKeyField(declared.Type(), keys.variables)
		}
		if isKey {
			named = append(named, fact_reach.Named{Object: object.Object, Reason: NoAny.Reason()})
		}
	}
	if len(keys.methodNames) > 0 {
		named = append(named, methodsNamed(used, keys.methodNames, find, NoAny.Reason())...)
	}
	return named, leaving
}

// holdsKeyField is whether the struct a named type declares holds a field that is a key.
func holdsKeyField(declared types.Type, variables map[*types.Var]bool) bool {
	structure, isStruct := declared.Underlying().(*types.Struct)
	if !isStruct {
		return false
	}
	for index := range structure.NumFields() {
		if variables[structure.Field(index).Origin()] {
			return true
		}
	}
	return false
}

// noAnyKeysOf is what every declaration-level candidate of the pass's files keeps. The rule's dispatcher hears a
// candidate anywhere in a file (rules/consistency_no_any/analyzer.go:92-100); the walk here skips every function's
// body, a literal's too, so a candidate is read only where it is written in a declaration's type.
func noAnyKeysOf(pass *analysis.Pass) noAnyKeys {
	keys := noAnyKeys{functions: map[*types.Func]bool{}, variables: map[*types.Var]bool{},
		methodNames: map[string]bool{}}
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
			if spellsAny(pass, node) {
				if declarations, judged := noAnyDeclarations(pass, node, stack); judged {
					addNoAnyKeys(keys, declarations)
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
func spellsAny(pass *analysis.Pass, node ast.Node) bool {
	switch written := node.(type) {
	case *ast.Ident:
		return written.Name == "any" && pass.TypesInfo.Uses[written] == types.Universe.Lookup("any")
	case *ast.InterfaceType:
		return written.Methods == nil || len(written.Methods.List) == 0
	}
	return false
}

// noAnyDeclarations climbs from a candidate through the type it is written in to the declaration that states it, and
// is each declaration on the way, arm for arm as the rule's declarationsAround does
// (rules/consistency_no_any/flow_analysis.go:491-537). judged is false where the rule's is: the climb left type syntax
// for an expression, or passed through a constraint, a receiver or an interface's embedded element.
func noAnyDeclarations(pass *analysis.Pass, node ast.Node, stack []ast.Node) ([]types.Object, bool) {
	declarations := []types.Object{}
	child := node
	for index := len(stack) - 1; index >= 0; index-- {
		switch parent := stack[index].(type) {
		case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.StarExpr, *ast.Ellipsis, *ast.ParenExpr,
			*ast.IndexExpr, *ast.IndexListExpr, *ast.FuncType, *ast.FieldList, *ast.StructType, *ast.InterfaceType:
			// Type syntax, which the climb passes through.
		case *ast.Field:
			fieldDeclarations, judged := noAnyFieldDeclarations(pass, parent, stack[:index])
			if !judged {
				return nil, false
			}
			declarations = append(declarations, fieldDeclarations...)
		case *ast.FuncDecl:
			if child != parent.Type {
				return nil, false
			}
			if object := pass.TypesInfo.Defs[parent.Name]; object != nil {
				declarations = append(declarations, object)
			}
			return declarations, true
		case *ast.FuncLit:
			// A literal is a key of its own that no body names, so it keeps nothing here.
			return declarations, child == parent.Type
		case *ast.TypeSpec:
			return declarations, child == parent.Type
		case *ast.ValueSpec:
			if child != parent.Type {
				return nil, false
			}
			for _, name := range parent.Names {
				if object := pass.TypesInfo.Defs[name]; object != nil {
					declarations = append(declarations, object)
				}
			}
			return declarations, true
		default:
			return nil, false
		}
		child = stack[index]
	}
	return nil, false
}

// noAnyFieldDeclarations are the declarations field states, its ancestors in above, as the rule's fieldDeclarations
// reads them (rules/consistency_no_any/flow_analysis.go:542-590): the parameters or results of a function whose
// signature is known, a struct's fields, or an interface's method. judged is false for a type parameter, a receiver
// and an interface's embedded element.
func noAnyFieldDeclarations(pass *analysis.Pass, field *ast.Field, above []ast.Node) ([]types.Object, bool) {
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
		signature := noAnySignature(pass, owner, above[:len(above)-2])
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
		structure, isStruct := pass.TypesInfo.TypeOf(owner).(*types.Struct)
		if !isStruct {
			return nil, true
		}
		return fieldVariables(list, field, structure.NumFields(), structure.Field), true
	case *ast.InterfaceType:
		if len(field.Names) == 0 {
			return nil, false
		}
		if method := pass.TypesInfo.Defs[field.Names[0]]; method != nil {
			return []types.Object{method}, true
		}
		return nil, true
	}
	return nil, false
}

// noAnySignature is the signature of the function, function literal or interface method functionType belongs to, its
// ancestors in above, or nil for a function type written as a type (signatureOwning,
// rules/consistency_no_any/flow_analysis.go:594-620).
func noAnySignature(pass *analysis.Pass, functionType *ast.FuncType, above []ast.Node) *types.Signature {
	if len(above) == 0 {
		return nil
	}
	switch owner := above[len(above)-1].(type) {
	case *ast.FuncDecl:
		if declared, isFunction := pass.TypesInfo.Defs[owner.Name].(*types.Func); isFunction &&
			owner.Type == functionType {
			return declared.Signature()
		}
	case *ast.FuncLit:
		signature, _ := pass.TypesInfo.TypeOf(owner).(*types.Signature)
		return signature
	case *ast.Field:
		if len(owner.Names) == 0 || owner.Type != functionType || len(above) < 3 {
			return nil
		}
		if _, isInterface := above[len(above)-3].(*ast.InterfaceType); !isInterface {
			return nil
		}
		if method, isFunction := pass.TypesInfo.Defs[owner.Names[0]].(*types.Func); isFunction {
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
