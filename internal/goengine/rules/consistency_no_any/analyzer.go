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
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-any"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_any",
	Doc: "Flags any or interface{} written as the type of a parameter, result, struct field, interface method, " +
		"variable or named type this package declares, a type argument inside one included, since the house states " +
		"a value's type. Left alone: a method matching a method of another package in the import graph, a value " +
		"handed to a place whose type is no interface or taken from another package's declaration of the same " +
		"type, a constraint, a type written inside an expression, and a generated file.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads the types to tell whose a type is.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var anyTypeMessage = policy.MessageOf(Name, "anyType")

func run(pass *analysis.Pass) (any, error) {
	flows := &flowAnalysis{
		pass:        pass,
		sinkFixed:   map[flowKey]bool{},
		definedFrom: map[flowKey][]flowKey{},
		sourceFixed: map[flowKey]bool{},
		feeds:       map[flowKey][]flowKey{},
	}
	candidates := []candidate{}
	for _, file := range pass.Files {
		// A generated file's declarations are its generator's, and an edit by hand is lost on the next generation.
		// This is the one place the rule skips a file, so a driver filter can take it over by deleting these lines.
		// Its flows are still read, since a value a generated file hands on fixes a type as surely as any other.
		judged := !ast.IsGenerated(file)
		stack := []ast.Node{}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			flows.record(node, stack)
			if judged {
				if found, isCandidate := flows.candidateAt(node, stack); isCandidate {
					candidates = append(candidates, found)
				}
			}
			stack = append(stack, node)
			return true
		})
	}
	flows.settle(candidates)
	for _, found := range candidates {
		if !flows.anyFixed(found.declarations) {
			pass.Report(analysis.Diagnostic{
				Pos:      found.node.Pos(),
				End:      found.node.End(),
				Category: anyTypeMessage.Id,
				Message:  anyTypeMessage.Render(map[string]string{"spelling": found.spelling}),
			})
		}
	}
	return nil, nil
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

// flowAnalysis follows values through the package to learn which declarations have a type fixed elsewhere.
//
// A value handed to a place whose type is no interface must have that type, so the place fixes the declaration the
// value comes from: sinkFixed. A declaration given a value of exactly its own type from another package holds what
// that package hands it: sourceFixed. The two are kept apart, because a place fixing a value's type says nothing
// about where the value came from, and joining them would let two of our own declarations excuse each other.
type flowAnalysis struct {
	pass      *analysis.Pass
	sinkFixed map[flowKey]bool
	// definedFrom maps a variable a short declaration, or a var with no type, defines to the declaration its value
	// came from. Its type is that declaration's, so a place that fixes it fixes that declaration.
	definedFrom map[flowKey][]flowKey
	sourceFixed map[flowKey]bool
	// feeds maps a declaration to those given its value at its own type, which take their type from it.
	feeds map[flowKey][]flowKey
	// methodFixed caches whether a method's signature matches a method of another package.
	methodFixed map[*types.Func]bool
	foreign     *foreignMethods
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

// record notes every place node hands a value on.
func (flows *flowAnalysis) record(node ast.Node, stack []ast.Node) {
	switch handing := node.(type) {
	case *ast.CallExpr:
		flows.recordCall(handing)
	case *ast.AssignStmt:
		flows.recordAssignment(handing)
	case *ast.ValueSpec:
		flows.recordValueSpec(handing)
	case *ast.ReturnStmt:
		flows.recordReturn(handing, flows.enclosingSignature(stack))
	case *ast.CompositeLit:
		flows.recordCompositeLiteral(handing)
	case *ast.SendStmt:
		flows.recordSend(handing)
	}
}

// recordCall notes each argument handed to its parameter, and a conversion's operand handed to the type it names.
func (flows *flowAnalysis) recordCall(call *ast.CallExpr) {
	info := flows.pass.TypesInfo
	if info.Types[call.Fun].IsType() {
		if len(call.Args) == 1 {
			flows.sink(info.TypeOf(call), flows.valueOf(call.Args[0]))
		}
		return
	}
	// A builtin's parameters are typed from its own arguments, append's from the slice it is given, so they fix
	// nothing that was not already fixed.
	if info.Types[call.Fun].IsBuiltin() {
		return
	}
	calleeType := info.TypeOf(call.Fun)
	if calleeType == nil {
		return
	}
	signature, isSignature := calleeType.Underlying().(*types.Signature)
	if !isSignature {
		return
	}
	// One multi-value call spread over the parameters, f(g()), hands values no single expression holds.
	if len(call.Args) == 1 {
		if _, isTuple := info.TypeOf(call.Args[0]).(*types.Tuple); isTuple {
			return
		}
	}
	// A function of this package receives what its callers hand it, so its parameter takes the type of a value
	// another package hands it. The declared parameters are the generic origin's, whose objects the declaration
	// defines, while the expected types are the instantiation's, which the arguments are checked against.
	var declaredParameters *types.Tuple
	if function, isFunc := typeutil.Callee(info, call).(*types.Func); isFunc && function.Pkg() == flows.pass.Pkg {
		declaredParameters = function.Origin().Signature().Params()
	}
	for index, argument := range call.Args {
		expected, parameterIndex := parameterType(signature, index, call.Ellipsis.IsValid())
		value := flows.valueOf(argument)
		flows.sink(expected, value)
		if declaredParameters != nil && parameterIndex < declaredParameters.Len() {
			parameter := declaredParameters.At(parameterIndex)
			flows.source(reference{key: flowKey{object: parameter}}, expected, value)
		}
	}
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

// recordAssignment notes each value handed to what is left of `=`, or defined by `:=`.
func (flows *flowAnalysis) recordAssignment(assignment *ast.AssignStmt) {
	// An operator assignment, `+=`, computes a value of the left side's own type, handing nothing new to it.
	if assignment.Tok != token.ASSIGN && assignment.Tok != token.DEFINE {
		return
	}
	values := flows.valuesOf(assignment.Rhs, len(assignment.Lhs))
	for index, left := range assignment.Lhs {
		if index >= len(values) {
			return
		}
		if identifier, isIdentifier := left.(*ast.Ident); isIdentifier && assignment.Tok == token.DEFINE {
			// A name := declares anew is defined by its value; one it redeclares is assigned like any other.
			if defined := flows.pass.TypesInfo.Defs[identifier]; defined != nil {
				flows.define(defined, values[index])
				continue
			}
		}
		leftType := flows.pass.TypesInfo.TypeOf(left)
		flows.sink(leftType, values[index])
		flows.source(flows.referenceOf(left, -1), leftType, values[index])
	}
}

// recordValueSpec notes each value a var declaration gives its names.
func (flows *flowAnalysis) recordValueSpec(spec *ast.ValueSpec) {
	values := flows.valuesOf(spec.Values, len(spec.Names))
	for index, name := range spec.Names {
		variable, isVar := flows.pass.TypesInfo.Defs[name].(*types.Var)
		if !isVar || index >= len(values) {
			continue
		}
		if spec.Type == nil {
			flows.define(variable, values[index])
			continue
		}
		flows.sink(variable.Type(), values[index])
		flows.source(reference{key: flowKey{object: variable}}, variable.Type(), values[index])
	}
}

// recordReturn notes each value a return statement hands to its function's results.
func (flows *flowAnalysis) recordReturn(statement *ast.ReturnStmt, signature *types.Signature) {
	if signature == nil {
		return
	}
	results := signature.Results()
	for index, value := range flows.valuesOf(statement.Results, results.Len()) {
		result := results.At(index)
		flows.sink(result.Type(), value)
		flows.source(reference{key: flowKey{object: result}}, result.Type(), value)
	}
}

// recordCompositeLiteral notes each element a composite literal hands to its field, element or key type.
func (flows *flowAnalysis) recordCompositeLiteral(literal *ast.CompositeLit) {
	literalType := flows.pass.TypesInfo.TypeOf(literal)
	if literalType == nil {
		return
	}
	underlying := literalType.Underlying()
	// An element of a []*T whose type is elided, {...} for &T{...}, is recorded as the pointer.
	if pointer, isPointer := underlying.(*types.Pointer); isPointer {
		underlying = pointer.Elem().Underlying()
	}
	switch composite := underlying.(type) {
	case *types.Struct:
		for index, element := range literal.Elts {
			if keyValue, isKeyValue := element.(*ast.KeyValueExpr); isKeyValue {
				if identifier, isIdentifier := keyValue.Key.(*ast.Ident); isIdentifier {
					if field, isField := flows.pass.TypesInfo.Uses[identifier].(*types.Var); isField {
						flows.recordField(field, keyValue.Value)
					}
				}
				continue
			}
			if index < composite.NumFields() {
				flows.recordField(composite.Field(index), element)
			}
		}
	case *types.Slice:
		flows.recordElements(literal, nil, composite.Elem())
	case *types.Array:
		flows.recordElements(literal, nil, composite.Elem())
	case *types.Map:
		flows.recordElements(literal, composite.Key(), composite.Elem())
	}
}

// recordField notes value handed to field in a struct literal.
func (flows *flowAnalysis) recordField(field *types.Var, value ast.Expr) {
	handed := flows.valueOf(value)
	flows.sink(field.Type(), handed)
	flows.source(flows.referenceToObject(field), field.Type(), handed)
}

// recordElements notes each element of a slice, array or map literal handed to the element type, and each map key
// to the key type. An index in a slice literal, {2: value}, is a constant and hands nothing.
func (flows *flowAnalysis) recordElements(literal *ast.CompositeLit, keyType types.Type, elementType types.Type) {
	for _, element := range literal.Elts {
		if keyValue, isKeyValue := element.(*ast.KeyValueExpr); isKeyValue {
			if keyType != nil {
				flows.sink(keyType, flows.valueOf(keyValue.Key))
			}
			flows.sink(elementType, flows.valueOf(keyValue.Value))
			continue
		}
		flows.sink(elementType, flows.valueOf(element))
	}
}

// recordSend notes the value a send statement hands to its channel's element type.
func (flows *flowAnalysis) recordSend(statement *ast.SendStmt) {
	channelType := flows.pass.TypesInfo.TypeOf(statement.Chan)
	if channelType == nil {
		return
	}
	channel, isChannel := channelType.Underlying().(*types.Chan)
	if !isChannel {
		return
	}
	value := flows.valueOf(statement.Value)
	flows.sink(channel.Elem(), value)
	flows.source(flows.referenceOf(statement.Chan, -1), channel.Elem(), value)
}

// enclosingSignature is the signature of the innermost function the stack is inside, whose results a return
// statement hands its values to, or nil outside any function.
func (flows *flowAnalysis) enclosingSignature(stack []ast.Node) *types.Signature {
	for index := len(stack) - 1; index >= 0; index-- {
		switch function := stack[index].(type) {
		case *ast.FuncLit:
			signature, _ := flows.pass.TypesInfo.TypeOf(function).(*types.Signature)
			return signature
		case *ast.FuncDecl:
			if object, isFunc := flows.pass.TypesInfo.Defs[function.Name].(*types.Func); isFunc {
				return object.Signature()
			}
			return nil
		}
	}
	return nil
}

func (flows *flowAnalysis) valueOf(expression ast.Expr) flowingValue {
	return flowingValue{expression: expression, resultIndex: -1, valueType: flows.pass.TypesInfo.TypeOf(expression)}
}

// valuesOf pairs count receivers with the values right of an assignment or return: one expression each, or one
// multi-value expression spread over them, a call or a comma-ok form. Nil when the two cannot be paired.
func (flows *flowAnalysis) valuesOf(right []ast.Expr, count int) []flowingValue {
	if len(right) == count {
		values := make([]flowingValue, count)
		for index, expression := range right {
			values[index] = flows.valueOf(expression)
		}
		return values
	}
	if len(right) != 1 {
		return nil
	}
	tuple, isTuple := flows.pass.TypesInfo.TypeOf(right[0]).(*types.Tuple)
	if !isTuple || tuple.Len() != count {
		return nil
	}
	values := make([]flowingValue, count)
	for index := range count {
		values[index] = flowingValue{expression: right[0], resultIndex: index, valueType: tuple.At(index).Type()}
	}
	return values
}

// sink notes value handed to a place of type expected. A place whose type is an interface accepts a value of any
// type that has its methods, so it fixes nothing; any other place accepts only its own type, or one with the same
// underlying type, so the declaration the value comes from takes its type from that place.
func (flows *flowAnalysis) sink(expected types.Type, value flowingValue) {
	if expected == nil || value.valueType == nil || types.IsInterface(expected) {
		return
	}
	if _, isTuple := value.valueType.(*types.Tuple); isTuple {
		return
	}
	if !types.AssignableTo(value.valueType, expected) {
		return
	}
	if from := flows.referenceOf(value.expression, value.resultIndex); from.key != (flowKey{}) {
		flows.sinkFixed[from.key] = true
	}
}

// source notes value given to receiver, a declaration of type receiverType. Only a value of exactly that type fixes
// it, since a declaration that could have held a narrower type than its value's chose its own.
func (flows *flowAnalysis) source(receiver reference, receiverType types.Type, value flowingValue) {
	if receiver.key == (flowKey{}) || receiverType == nil || value.valueType == nil {
		return
	}
	if !types.Identical(receiverType, value.valueType) {
		return
	}
	from := flows.referenceOf(value.expression, value.resultIndex)
	switch {
	case from.foreign:
		flows.sourceFixed[receiver.key] = true
	case from.key != (flowKey{}):
		flows.feeds[from.key] = append(flows.feeds[from.key], receiver.key)
	}
}

// define notes variable defined by value with no type of its own, which then stands for the value's declaration:
// a place fixing the variable fixes that declaration, and the variable takes its type from wherever that came from.
func (flows *flowAnalysis) define(variable types.Object, value flowingValue) {
	key := flowKey{object: variable}
	from := flows.referenceOf(value.expression, value.resultIndex)
	if from.foreign {
		flows.sourceFixed[key] = true
	}
	if from.key != (flowKey{}) {
		flows.definedFrom[key] = append(flows.definedFrom[key], from.key)
		flows.feeds[from.key] = append(flows.feeds[from.key], key)
	}
}

// referenceOf is the declaration value comes from: the variable, field or function it names, the function literal
// it is, the container it is read from, or the result of the function it calls. A value built where it stands, a
// literal of another kind or a conversion, comes from no declaration.
func (flows *flowAnalysis) referenceOf(expression ast.Expr, resultIndex int) reference {
	expression = ast.Unparen(expression)
	if call, isCall := expression.(*ast.CallExpr); isCall {
		return flows.referenceToResult(call, max(resultIndex, 0))
	}
	// The second value of a comma-ok form is a bool the form makes, not anything its operand holds.
	if resultIndex > 0 {
		return reference{}
	}
	switch operand := expression.(type) {
	case *ast.Ident:
		return flows.referenceToObject(flows.pass.TypesInfo.Uses[operand])
	case *ast.SelectorExpr:
		return flows.referenceToObject(flows.pass.TypesInfo.Uses[operand.Sel])
	case *ast.FuncLit:
		return reference{key: flowKey{literal: operand}}
	case *ast.IndexExpr:
		// An element read from a container, or a generic function instantiated, comes from what is indexed.
		return flows.referenceOf(operand.X, -1)
	case *ast.IndexListExpr:
		return flows.referenceOf(operand.X, -1)
	case *ast.StarExpr:
		return flows.referenceOf(operand.X, -1)
	case *ast.UnaryExpr:
		if operand.Op == token.AND || operand.Op == token.ARROW {
			return flows.referenceOf(operand.X, -1)
		}
	}
	return reference{}
}

// referenceToResult is where the value at index of call's results comes from: the result of a function of this
// package, which is judged where it is declared, another package's function, or the function value called, whose
// own type gives the result its type.
func (flows *flowAnalysis) referenceToResult(call *ast.CallExpr, index int) reference {
	info := flows.pass.TypesInfo
	if info.Types[call.Fun].IsType() {
		return reference{}
	}
	if info.Types[call.Fun].IsBuiltin() {
		// recover returns any because the language says so, while every other builtin's result is typed from its
		// own arguments, append's from the slice it is given, and so comes from no declaration of its own.
		if typeutil.Callee(info, call) == types.Universe.Lookup("recover") {
			return reference{foreign: true}
		}
		return reference{}
	}
	if function, isFunc := typeutil.Callee(info, call).(*types.Func); isFunc {
		// A method of the predeclared error belongs to no package and returns no empty interface.
		if function.Pkg() == nil {
			return reference{}
		}
		if function.Pkg() != flows.pass.Pkg {
			return reference{foreign: true}
		}
		results := function.Origin().Signature().Results()
		if index >= results.Len() {
			return reference{}
		}
		return reference{key: flowKey{object: results.At(index)}}
	}
	return flows.referenceOf(call.Fun, -1)
}

// referenceToObject is the declaration object is: one of this package's variables, fields or functions, by its
// generic origin when it is an instantiation's, or another package's.
func (flows *flowAnalysis) referenceToObject(object types.Object) reference {
	switch declared := object.(type) {
	case *types.Var:
		object = declared.Origin()
	case *types.Func:
		object = declared.Origin()
	default:
		return reference{}
	}
	if object.Pkg() == nil {
		return reference{}
	}
	if object.Pkg() != flows.pass.Pkg {
		return reference{foreign: true}
	}
	return reference{key: flowKey{object: object}}
}

// candidateAt is node as a candidate when it is any or an empty interface written in a declaration's type.
func (flows *flowAnalysis) candidateAt(node ast.Node, stack []ast.Node) (candidate, bool) {
	spelling := ""
	switch written := node.(type) {
	case *ast.Ident:
		// The predeclared any by its object, so a local type named any, or a parameter named any, is not it.
		if flows.pass.TypesInfo.Uses[written] != types.Universe.Lookup("any") {
			return candidate{}, false
		}
		spelling = "any"
	case *ast.InterfaceType:
		if written.Methods != nil && len(written.Methods.List) > 0 {
			return candidate{}, false
		}
		spelling = "interface{}"
	default:
		return candidate{}, false
	}
	declarations, judged := flows.declarationsAround(node, stack)
	if !judged {
		return candidate{}, false
	}
	return candidate{node: node, spelling: spelling, declarations: declarations}, true
}

// declarationsAround climbs from a candidate through the type it is written in to the declaration that states it,
// collecting each declaration on the way whose type could be fixed elsewhere. judged is false when the climb leaves
// type syntax for an expression, or passes through a constraint or an interface's embedded element, where the
// words are no value's type.
func (flows *flowAnalysis) declarationsAround(node ast.Node, stack []ast.Node) ([]flowKey, bool) {
	declarations := []flowKey{}
	child := node
	for index := len(stack) - 1; index >= 0; index-- {
		switch parent := stack[index].(type) {
		case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.StarExpr, *ast.Ellipsis, *ast.ParenExpr,
			*ast.IndexExpr, *ast.IndexListExpr, *ast.FuncType, *ast.FieldList, *ast.StructType, *ast.InterfaceType:
			// Type syntax: the candidate is part of a larger type, an element, a pointer's target, a type argument.
		case *ast.Field:
			fieldDeclarations, judged := flows.fieldDeclarations(parent, stack[:index])
			if !judged {
				return nil, false
			}
			declarations = append(declarations, fieldDeclarations...)
		case *ast.FuncDecl:
			// A receiver's type is never an empty interface, so a climb from it is no signature's.
			if child != parent.Type {
				return nil, false
			}
			if object := flows.pass.TypesInfo.Defs[parent.Name]; object != nil {
				declarations = append(declarations, flowKey{object: object})
			}
			return declarations, true
		case *ast.FuncLit:
			if child != parent.Type {
				return nil, false
			}
			return append(declarations, flowKey{literal: parent}), true
		case *ast.TypeSpec:
			return declarations, child == parent.Type
		case *ast.ValueSpec:
			if child != parent.Type {
				return nil, false
			}
			for _, name := range parent.Names {
				if object := flows.pass.TypesInfo.Defs[name]; object != nil {
					declarations = append(declarations, flowKey{object: object})
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

// fieldDeclarations are the declarations field states: parameters or results of a function whose signature is
// known, struct fields, or an interface's method. judged is false for a type parameter, a receiver, and an
// interface's embedded element, whose types are constraints or are fixed by the type they name.
func (flows *flowAnalysis) fieldDeclarations(field *ast.Field, above []ast.Node) ([]flowKey, bool) {
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
		signature := flows.signatureOwning(owner, above[:len(above)-2])
		// A function type written as a type, func(any) in a field or a named type, declares no variables.
		if signature == nil {
			return nil, true
		}
		variables := signature.Params()
		if list == owner.Results {
			variables = signature.Results()
		}
		declarations := []flowKey{}
		for _, variable := range variablesOf(list, field, variables.Len(), variables.At) {
			declarations = append(declarations, flowKey{object: variable})
		}
		return declarations, true
	case *ast.StructType:
		structure, isStruct := flows.pass.TypesInfo.TypeOf(owner).(*types.Struct)
		if !isStruct {
			return nil, true
		}
		declarations := []flowKey{}
		for _, variable := range variablesOf(list, field, structure.NumFields(), structure.Field) {
			declarations = append(declarations, flowKey{object: variable})
		}
		return declarations, true
	case *ast.InterfaceType:
		// An embedded element, an interface or a type set's term, is a constraint or names its own methods.
		if len(field.Names) == 0 {
			return nil, false
		}
		if method := flows.pass.TypesInfo.Defs[field.Names[0]]; method != nil {
			return []flowKey{{object: method}}, true
		}
		return nil, true
	}
	return nil, false
}

// signatureOwning is the signature of the function, function literal or interface method a function type belongs to,
// whose parameters and results are variables, or nil for a function type written as a type.
func (flows *flowAnalysis) signatureOwning(functionType *ast.FuncType, above []ast.Node) *types.Signature {
	if len(above) == 0 {
		return nil
	}
	switch owner := above[len(above)-1].(type) {
	case *ast.FuncDecl:
		if object, isFunc := flows.pass.TypesInfo.Defs[owner.Name].(*types.Func); isFunc && owner.Type == functionType {
			return object.Signature()
		}
	case *ast.FuncLit:
		signature, _ := flows.pass.TypesInfo.TypeOf(owner).(*types.Signature)
		return signature
	case *ast.Field:
		// A method of an interface: a named field whose type is this function type, inside an interface.
		if len(owner.Names) == 0 || owner.Type != functionType || len(above) < 3 {
			return nil
		}
		if _, isInterface := above[len(above)-3].(*ast.InterfaceType); !isInterface {
			return nil
		}
		if method, isFunc := flows.pass.TypesInfo.Defs[owner.Names[0]].(*types.Func); isFunc {
			return method.Signature()
		}
	}
	return nil
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

// settle finishes the analysis once every flow is recorded: a place fixing a defined variable fixes what it was
// defined from, a method matching another package's is fixed, a fixed function's parameters and results hold what
// that fixing hands them, and a declaration fed by a fixed one takes its type.
func (flows *flowAnalysis) settle(candidates []candidate) {
	// Each walk starts from the keys marked fixed, by their value rather than their presence in the map, so an entry
	// ever written false could not spread.
	pending := []flowKey{}
	for key, fixed := range flows.sinkFixed {
		if fixed {
			pending = append(pending, key)
		}
	}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, from := range flows.definedFrom[key] {
			if !flows.sinkFixed[from] {
				flows.sinkFixed[from] = true
				pending = append(pending, from)
			}
		}
	}
	flows.methodFixed = map[*types.Func]bool{}
	for _, found := range candidates {
		for _, key := range found.declarations {
			if method, isMethod := key.object.(*types.Func); isMethod && method.Signature().Recv() != nil {
				if _, decided := flows.methodFixed[method]; !decided {
					flows.methodFixed[method] = flows.matchesForeignMethod(method)
				}
			}
		}
	}
	// A function whose signature is fixed receives values of the types that fixing chose, so its parameters and
	// results count as given from elsewhere. The function itself is not marked, since a place of this package may be
	// what fixed it, and the function's value given to a variable must not then excuse that variable.
	for key, fixed := range flows.sinkFixed {
		if fixed {
			flows.seedSignature(key)
		}
	}
	for method, fixed := range flows.methodFixed {
		if fixed {
			flows.seedSignature(flowKey{object: method})
		}
	}
	for key, fixed := range flows.sourceFixed {
		if fixed {
			pending = append(pending, key)
		}
	}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, fed := range flows.feeds[key] {
			if !flows.sourceFixed[fed] {
				flows.sourceFixed[fed] = true
				pending = append(pending, fed)
			}
		}
	}
}

// seedSignature marks the parameters and results of the function key names as given from elsewhere.
func (flows *flowAnalysis) seedSignature(key flowKey) {
	var signature *types.Signature
	switch {
	case key.literal != nil:
		signature, _ = flows.pass.TypesInfo.TypeOf(key.literal).(*types.Signature)
	default:
		if function, isFunc := key.object.(*types.Func); isFunc {
			signature = function.Signature()
		}
	}
	if signature == nil {
		return
	}
	for index := range signature.Params().Len() {
		flows.sourceFixed[flowKey{object: signature.Params().At(index)}] = true
	}
	for index := range signature.Results().Len() {
		flows.sourceFixed[flowKey{object: signature.Results().At(index)}] = true
	}
}

// anyFixed is whether any of declarations has its type fixed elsewhere.
func (flows *flowAnalysis) anyFixed(declarations []flowKey) bool {
	for _, key := range declarations {
		if flows.sinkFixed[key] || flows.sourceFixed[key] {
			return true
		}
		if method, isMethod := key.object.(*types.Func); isMethod && flows.methodFixed[method] {
			return true
		}
	}
	return false
}

// matchesForeignMethod is whether a method of another package in the import graph has method's name and signature.
// Implementing an interface takes exactly its method's signature, and an interface written here to be satisfied by
// another package's type copies that type's method, so in both cases the signature is that package's. A near miss,
// a generic type's method with as many parameters and results, is left alone too, since some instantiation may be
// what it matches.
func (flows *flowAnalysis) matchesForeignMethod(method *types.Func) bool {
	if flows.foreign == nil {
		flows.foreign = buildForeignMethods(flows.pass.Pkg)
	}
	for _, foreignSignature := range flows.foreign.byName[method.Name()] {
		if types.Identical(method.Signature(), foreignSignature) {
			return true
		}
	}
	for _, foreignSignature := range flows.foreign.genericByName[method.Name()] {
		if sameShape(method.Signature(), foreignSignature) {
			return true
		}
	}
	return false
}

// sameShape is whether two signatures take as many parameters, return as many results, and agree on a variadic end.
func sameShape(left *types.Signature, right *types.Signature) bool {
	return left.Params().Len() == right.Params().Len() &&
		left.Results().Len() == right.Results().Len() &&
		left.Variadic() == right.Variadic()
}

// foreignMethods are the exported methods of every named type in a package's import graph, interfaces and concrete
// types alike, by name. Signature identity ignores receivers, so each is held by its signature alone.
type foreignMethods struct {
	byName map[string][]*types.Signature
	// genericByName holds a generic type's methods apart, since their signatures speak of type parameters that only
	// an instantiation the rule cannot know replaces, so they are matched by shape.
	genericByName map[string][]*types.Signature
}

// buildForeignMethods indexes the methods of every package reachable through current's imports, current's own
// excepted: a method declared here is ours, and is judged with the rest of this package.
func buildForeignMethods(current *types.Package) *foreignMethods {
	foreign := &foreignMethods{byName: map[string][]*types.Signature{}, genericByName: map[string][]*types.Signature{}}
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

// add indexes named's exported methods: an interface's whole method set, embedded methods included, and a concrete
// type's own declared methods. Unexported ones belong to their package, where no other package can match them.
func (foreign *foreignMethods) add(named *types.Named) {
	index := foreign.byName
	if named.TypeParams().Len() > 0 {
		index = foreign.genericByName
	}
	if interfaceType, isInterface := named.Underlying().(*types.Interface); isInterface {
		for methodIndex := range interfaceType.NumMethods() {
			method := interfaceType.Method(methodIndex)
			if method.Exported() {
				index[method.Name()] = append(index[method.Name()], method.Signature())
			}
		}
		return
	}
	for methodIndex := range named.NumMethods() {
		method := named.Method(methodIndex)
		if method.Exported() {
			index[method.Name()] = append(index[method.Name()], method.Signature())
		}
	}
}
