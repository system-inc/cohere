// Package consistency_no_any is the Go engine's rule that a declaration states the type of what it holds.
//
// `any`, and the empty interface it names, says a value could be anything, so the compiler checks nothing about it
// and the next reader has to find every place it is set to learn what it really is. The house states a value's type:
// a concrete type, a small interface of the methods the code needs, or a type parameter where it truly takes any.
//
// A type is ours to state only when this package chose it. A method matching a method of another package, which an
// interface may ask for, has the signature that other method fixed. A value handed to a place whose type is not an
// interface, a function literal passed as a callback or a []any forwarded with `...`, has the type that place
// fixed, and that place is judged where it is declared. A declaration that takes its value from another package's
// declaration of the same type, `var value any = cache.Load()`, holds what that package returns. A constraint,
// `[T any]`, is no value's type, and a type written inside an expression, a composite literal's, a conversion's,
// make's, a type assertion's or a case's, is not a declaration. All of those are left alone.
package consistency_no_any

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-any"

// listener follows values and gathers candidates over the syntax dispatcher's walk of every file, hearing each node
// that hands a value on and each that could be any or an empty interface, and judges the candidates once the whole
// package has been walked, since a place anywhere in it may fix a declaration's type.
var listener = rule.Listener{
	FileEnter: enterFile,
	Nodes: []ast.Node{
		(*ast.CallExpr)(nil),
		(*ast.AssignStmt)(nil),
		(*ast.ValueSpec)(nil),
		(*ast.ReturnStmt)(nil),
		(*ast.CompositeLit)(nil),
		(*ast.SendStmt)(nil),
		(*ast.Ident)(nil),
		(*ast.InterfaceType)(nil),
	},
	Node:        judgeNode,
	PackageExit: exitPackage,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_any", "Flags any or interface{} written as the type of a "+
	"parameter, result, struct field, interface method, variable or named type this package declares, a type "+
	"argument inside one included, since the house states a value's type. Left alone: a method matching a method of "+
	"another package in the import graph, a value handed to a place whose type is no interface or taken from another "+
	"package's declaration of the same type, a constraint, and a type written inside an expression.", &listener)

// Rule is this rule's entry in the Go engine's registry. It reads the types to tell whose a type is.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var anyTypeMessage = policy.MessageOf(Name, "anyType")

// packageState is what the rule gathers over its package, held in its context's state.
type packageState struct {
	// flows follows the package's values, made as the walk enters its first file.
	flows *flowAnalysis
	// candidates are the any and interface{} words written in a declaration's type, in the order the walk met them.
	candidates []candidate
	// handings are the nodes that hand a value on, in the order the walk met them, recorded into flows only once the
	// package is walked and only when it holds a candidate: the flows decide nothing else, and most packages write
	// no any at all.
	handings []handing
}

// handing is a node that hands a value on, with the signature of the function it is in when it is a return
// statement, read from the walk's stack where the node was met, since the stack is gone by the time it is recorded.
type handing struct {
	node      ast.Node
	signature *types.Signature
}

// enterFile makes the package's flow analysis, the first time the walk enters one of its files.
func enterFile(context *rule.Context, _ *ast.File) {
	state := rule.State[packageState](context)
	if state.flows != nil {
		return
	}
	state.flows = &flowAnalysis{
		context:     context,
		sinkFixed:   map[flowKey]bool{},
		definedFrom: map[flowKey][]flowKey{},
		sourceFixed: map[flowKey]bool{},
		feeds:       map[flowKey][]flowKey{},
	}
	state.candidates = []candidate{}
}

// judgeNode keeps every node that hands a value on, and keeps node as a candidate when it is any or an empty
// interface written in a declaration's type, its ancestors in stack.
func judgeNode(context *rule.Context, node ast.Node, stack []ast.Node) {
	state := rule.State[packageState](context)
	if state.flows == nil {
		return
	}
	switch node.(type) {
	case *ast.CallExpr, *ast.AssignStmt, *ast.ValueSpec, *ast.CompositeLit, *ast.SendStmt:
		state.handings = append(state.handings, handing{node: node})
		return
	case *ast.ReturnStmt:
		state.handings = append(state.handings, handing{node: node, signature: state.flows.enclosingSignature(stack)})
		return
	}
	if found, isCandidate := state.flows.candidateAt(node, stack); isCandidate {
		state.candidates = append(state.candidates, found)
	}
}

// exitPackage records the package's flows and settles them when it holds a candidate, and reports each candidate none
// of whose declarations has its type fixed elsewhere.
func exitPackage(context *rule.Context) {
	state := rule.State[packageState](context)
	if state.flows == nil || len(state.candidates) == 0 {
		return
	}
	for _, each := range state.handings {
		state.flows.record(each.node, each.signature)
	}
	state.flows.settle(state.candidates)
	for _, found := range state.candidates {
		if !state.flows.anyFixed(found.declarations) {
			context.Report(analysis.Diagnostic{
				Pos:      found.node.Pos(),
				End:      found.node.End(),
				Category: anyTypeMessage.Id,
				Message:  anyTypeMessage.Render(map[string]string{"spelling": found.spelling}),
			})
		}
	}
}

// flowKey names a declaration whose type the rule may find fixed elsewhere: a variable, field, parameter, result or
// function by its object, or a function literal, which has no object, by its node.
type flowKey struct {
	object  types.Object
	literal *ast.FuncLit
}

// candidate is an any or interface{} written in a declaration's type, with the declarations it is part of, from the
// innermost out. Any one of them having its type fixed elsewhere fixes the words written in it.
type candidate struct {
	node         ast.Node
	spelling     string
	declarations []flowKey
}

// flowingValue is one value handed somewhere: an expression, or one value of a multi-value expression.
type flowingValue struct {
	expression ast.Expr
	// resultIndex is which value of a multi-value expression this is, or -1 for an expression's only value.
	resultIndex int
	valueType   types.Type
}

// reference is where a value comes from: one of our declarations, or another package's.
type reference struct {
	key flowKey
	// foreign is whether the value comes from another package's declaration, whose type is not ours to restate.
	foreign bool
}

// parameterType is the type an argument at index must have, and the parameter it lands in. Past the last parameter
// of a variadic function each argument is one element, unless the call spreads a slice with `...`, which is then the
// whole parameter.
func parameterType(signature *types.Signature, index int, spread bool) (types.Type, int) {
	count := signature.Params().Len()
	if signature.Variadic() && index >= count-1 {
		last := signature.Params().At(count - 1).Type()
		if spread {
			return last, count - 1
		}
		if slice, isSlice := last.Underlying().(*types.Slice); isSlice {
			return slice.Elem(), count - 1
		}
		return nil, count
	}
	if index < count {
		return signature.Params().At(index).Type(), index
	}
	return nil, count
}

// variablesOf are the variables field declares, found by position in the variables of the whole list: a field of
// several names declares one each, and an unnamed or embedded field one.
func variablesOf(list *ast.FieldList, field *ast.Field, count int, at func(int) *types.Var) []*types.Var {
	position := 0
	for _, each := range list.List {
		declared := max(1, len(each.Names))
		if each == field {
			variables := []*types.Var{}
			for index := position; index < position+declared && index < count; index++ {
				variables = append(variables, at(index))
			}
			return variables
		}
		position += declared
	}
	return nil
}

// sameShape is whether two signatures take as many parameters, return as many results, and agree on a variadic end.
func sameShape(left *types.Signature, right *types.Signature) bool {
	return left.Params().Len() == right.Params().Len() &&
		left.Results().Len() == right.Results().Len() &&
		left.Variadic() == right.Variadic()
}

// buildForeignMethods indexes the methods of every package reachable through current's imports, current's own
// excepted: a method declared here is ours, and is judged with the rest of this package.
func buildForeignMethods(current *types.Package) *foreignMethods {
	foreign := &foreignMethods{byName: map[string][]*types.Func{}, genericByName: map[string][]*types.Func{}}
	visited := map[*types.Package]bool{current: true}
	pending := []*types.Package{current}
	for len(pending) > 0 {
		next := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, imported := range next.Imports() {
			if visited[imported] {
				continue
			}
			visited[imported] = true
			pending = append(pending, imported)
			for _, name := range imported.Scope().Names() {
				typeName, isTypeName := imported.Scope().Lookup(name).(*types.TypeName)
				if !isTypeName || typeName.IsAlias() {
					continue
				}
				if named, isNamed := typeName.Type().(*types.Named); isNamed {
					foreign.add(named)
				}
			}
		}
	}
	return foreign
}
