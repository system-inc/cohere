// Package correctness_no_caller_data_mutation is the Go engine's rule that a function does not write into data its
// caller passed in.
//
// A slice, a map and a pointer share what they reach with the caller, so a write at one lands in the caller's data.
// The call reads like it hands data over, and the caller's data comes back different with nothing at the call site to
// say so. Go does fill a caller's buffer on purpose, as Read(p []byte) and json.Unmarshal do. A method that implements
// io.Reader's Read has that contract from the interface, so its buffer is exempt with nothing written, and any other
// function whose contract is to fill a parameter says so once on its own doc comment, with
// `// Mutates: <parameter> <why>`, which exempts that parameter's writes and no other's.
//
// This is the twin of nexus/correctness-no-caller-data-mutation, read for Go: a method writing its own receiver is the
// type's API and is never judged, and only a direct write at the parameter is, since that is the write a reader can
// see is the caller's without following the data further.
package correctness_no_caller_data_mutation

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-no-caller-data-mutation"

// listener hears every function declaration of the syntax dispatcher's walk, each one at a file's top level.
var listener = rule.Listener{Nodes: []ast.Node{(*ast.FuncDecl)(nil)}, Node: judgeFunction}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "correctness_no_caller_data_mutation", "Flags a direct write at a "+
	"declared function's slice, map or pointer parameter (parameter[index] =, parameter[key] =, parameter.Field = "+
	"through a pointer, *parameter =, ++, --, op=, delete and clear), which lands in the caller's data with nothing "+
	"at the call site to say so, unless the function's doc comment says // Mutates: <parameter> <why>. Receivers, "+
	"the buffer of a Read that implements io.Reader, function literals' parameters and a parameter the function "+
	"reassigns or takes the address of are not judged.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var callerDataMutationMessage = policy.MessageOf(Name, "callerDataMutation")

var mutatesWithoutReasonMessage = policy.MessageOf(Name, "mutatesWithoutReason")

var mutatesUnknownParameterMessage = policy.MessageOf(Name, "mutatesUnknownParameter")

// judgeFunction judges the writes of a declared function at its parameters, once its Mutates tags are read.
func judgeFunction(context *rule.Context, node ast.Node, _ []ast.Node) {
	function, isFunction := node.(*ast.FuncDecl)
	if !isFunction {
		return
	}
	// The tags are read, and a malformed one reported, even on a function with no body to judge.
	outParameters := readMutatesTags(context, function)
	if function.Body == nil {
		return
	}
	// io.Reader's contract is that Read fills its buffer, so a Read that implements it fills the caller's buffer by
	// contract, as a Mutates tag would say.
	if buffer := rule.State[ioReader](context).bufferOf(context, function); buffer != nil {
		outParameters[buffer] = true
	}
	parameters := judgedParameters(context, function, outParameters)
	if len(parameters) == 0 {
		return
	}
	judgeWrites(context, function.Body, parameters)
}

// findIoReader is io.Reader's interface, found among the imports of analyzed and each package they reach, or nil when
// none of them is io. What it found, or that it found nothing, is recorded in reads, since it is an answer of the
// import closure no type read of analyzed holds, and the search is the one the restore asks again in another build
// (build_reads.ClosureInterface, #b2t9xmf).
func findIoReader(analyzed *types.Package, reads *build_reads.Reads) *types.Interface {
	reader, declared := build_reads.ClosureInterface(analyzed.Imports(), "io", "Reader")
	reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryClosure, Path: "io", Name: "Reader",
		Found: declared != nil, Answer: declared})
	return reader
}

// judgedParameters is every named parameter of function the rule judges: not the receiver, which is the type's API,
// not one a Mutates tag exempts, and not one the function reassigns or exposes to reassignment, since a write after
// that may land in a copy the function made rather than in the caller's data, and telling which needs the flow
// analysis this rule does not do.
func judgedParameters(
	context *rule.Context,
	function *ast.FuncDecl,
	outParameters map[*types.Var]bool,
) map[*types.Var]bool {
	parameters := map[*types.Var]bool{}
	for _, field := range function.Type.Params.List {
		for _, name := range field.Names {
			parameter, isVariable := context.TypesInfo.Defs[name].(*types.Var)
			if isVariable && !outParameters[parameter] {
				parameters[parameter] = true
			}
		}
	}
	for parameter := range reassignedParameters(context, function.Body, parameters) {
		delete(parameters, parameter)
	}
	return parameters
}

// reassignedParameters is each of parameters that body gives a new value or lets something else give one: assigned
// with any operator or a := that reuses it, assigned by a range, incremented, has its address taken, or calls a
// pointer method on itself, which takes its address without an &. Function literals in body count, since they reach
// the same variable.
func reassignedParameters(
	context *rule.Context,
	body *ast.BlockStmt,
	parameters map[*types.Var]bool,
) map[*types.Var]bool {
	reassigned := map[*types.Var]bool{}
	mark := func(expression ast.Expr) {
		if parameter := parameterNamed(context, expression, parameters); parameter != nil {
			reassigned[parameter] = true
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			// go/types records a name a := reuses in Uses and a new one in Defs, so only the reuse resolves here.
			for _, left := range statement.Lhs {
				mark(left)
			}
		case *ast.RangeStmt:
			if statement.Tok == token.ASSIGN {
				mark(statement.Key)
				mark(statement.Value)
			}
		case *ast.IncDecStmt:
			mark(statement.X)
		case *ast.UnaryExpr:
			if statement.Op == token.AND {
				mark(statement.X)
			}
		case *ast.SelectorExpr:
			if callsPointerMethodOnValue(context, statement) {
				mark(statement.X)
			}
		}
		return true
	})
	return reassigned
}

// callsPointerMethodOnValue is whether selector is a method of pointer receiver selected on a value that is no
// pointer, which Go turns into a call on the value's address.
func callsPointerMethodOnValue(context *rule.Context, selector *ast.SelectorExpr) bool {
	selection, isSelection := context.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal {
		return false
	}
	method, isFunction := selection.Obj().(*types.Func)
	if !isFunction || method.Signature().Recv() == nil {
		return false
	}
	if _, receiverIsPointer := method.Signature().Recv().Type().(*types.Pointer); !receiverIsPointer {
		return false
	}
	_, valueIsPointer := selection.Recv().Underlying().(*types.Pointer)
	return !valueIsPointer
}

// judgeWrites reports every direct write in body at one of parameters that reaches the caller's data.
func judgeWrites(context *rule.Context, body *ast.BlockStmt, parameters map[*types.Var]bool) {
	ast.Inspect(body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			// A := declares names and writes through none, so its left side never holds a write at a parameter.
			if statement.Tok == token.DEFINE {
				return true
			}
			for _, left := range statement.Lhs {
				judgeTarget(context, left, parameters)
			}
		case *ast.IncDecStmt:
			judgeTarget(context, statement.X, parameters)
		case *ast.RangeStmt:
			// `for parameter[index] = range values` writes the element on every turn.
			if statement.Tok == token.ASSIGN {
				judgeTarget(context, statement.Key, parameters)
				judgeTarget(context, statement.Value, parameters)
			}
		case *ast.CallExpr:
			judgeBuiltinCall(context, statement, parameters)
		}
		return true
	})
}

// judgeTarget reports target when it is a direct write at a parameter that lands in the caller's data: an element of
// a slice, a map's entry, an element of an array behind a pointer, a field behind a pointer, or the pointer's whole
// value. A deeper chain, parameter.Inner.Field or parameter[index].Field, is not judged.
func judgeTarget(context *rule.Context, target ast.Expr, parameters map[*types.Var]bool) {
	if target == nil {
		return
	}
	reaches := false
	switch written := ast.Unparen(target).(type) {
	case *ast.IndexExpr:
		if parameter := parameterNamed(context, written.X, parameters); parameter != nil {
			reaches = everyTerm(parameter.Type(), isSharedByIndex)
		} else if parameter := dereferencedParameter(context, written.X, parameters); parameter != nil {
			// (*parameter)[index] is parameter[index] spelled out, and reaches the caller only for an array behind
			// the pointer. Behind a pointer to a slice or map it is a second reach, which is not judged.
			reaches = everyTerm(parameter.Type(), isPointerToArray)
		}
	case *ast.SelectorExpr:
		// A field only: a method value writes nothing, and a qualified identifier has no parameter at its root.
		selection, isSelection := context.TypesInfo.Selections[written]
		if !isSelection || selection.Kind() != types.FieldVal {
			return
		}
		parameter := parameterNamed(context, written.X, parameters)
		if parameter == nil {
			// (*parameter).Field is parameter.Field spelled out.
			parameter = dereferencedParameter(context, written.X, parameters)
		}
		// A struct passed by value is the function's own copy, even when one of its embedded fields is a pointer
		// that a promoted field is reached through, so only a parameter that is itself a pointer is judged.
		reaches = parameter != nil && everyTerm(parameter.Type(), isPointer)
	case *ast.StarExpr:
		if parameter := parameterNamed(context, written.X, parameters); parameter != nil {
			reaches = everyTerm(parameter.Type(), isPointer)
		}
	}
	if reaches {
		report(context, target, callerDataMutationMessage)
	}
}

// judgeBuiltinCall reports a call of the builtin delete or clear on a parameter, each of which empties the caller's
// map or zeroes the caller's slice. A function of the package's own that is named delete or clear is not the builtin,
// so it resolves to something else and is never judged.
func judgeBuiltinCall(context *rule.Context, call *ast.CallExpr, parameters map[*types.Var]bool) {
	name, isIdentifier := ast.Unparen(call.Fun).(*ast.Ident)
	if !isIdentifier || len(call.Args) == 0 {
		return
	}
	builtin, isBuiltin := context.TypesInfo.Uses[name].(*types.Builtin)
	if !isBuiltin || (builtin.Name() != "delete" && builtin.Name() != "clear") {
		return
	}
	parameter := parameterNamed(context, call.Args[0], parameters)
	if parameter != nil && everyTerm(parameter.Type(), isSharedByIndex) {
		report(context, call, callerDataMutationMessage)
	}
}

// parameterNamed is the parameter expression names, when it is one of parameters spelled as its bare name, or nil.
func parameterNamed(context *rule.Context, expression ast.Expr, parameters map[*types.Var]bool) *types.Var {
	if expression == nil {
		return nil
	}
	name, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	if !isIdentifier {
		return nil
	}
	parameter, isVariable := context.TypesInfo.Uses[name].(*types.Var)
	if !isVariable || !parameters[parameter] {
		return nil
	}
	return parameter
}

// dereferencedParameter is the parameter expression dereferences, *parameter, or nil.
func dereferencedParameter(context *rule.Context, expression ast.Expr, parameters map[*types.Var]bool) *types.Var {
	star, isStar := ast.Unparen(expression).(*ast.StarExpr)
	if !isStar {
		return nil
	}
	return parameterNamed(context, star.X, parameters)
}

// everyTerm is whether holds is true of candidate's underlying type, or, for a type parameter, of every type its
// constraint allows. A constraint with no terms, any or a method set, could be anything, so a type parameter it
// constrains is never judged: its write may land in a copy.
func everyTerm(candidate types.Type, holds func(types.Type) bool) bool {
	typeParameter, isTypeParameter := types.Unalias(candidate).(*types.TypeParam)
	if !isTypeParameter {
		return holds(candidate.Underlying())
	}
	constraint, isInterface := typeParameter.Constraint().Underlying().(*types.Interface)
	return isInterface && everyTermOf(constraint, holds)
}

// everyTermOf is whether holds is true of every type constraint allows. A constraint's type set is the intersection of
// what each of its embedded elements allows, so one element whose every term holds is enough. An element is a union of
// terms, a single type, or an interface, a named constraint among them, read through to its own elements.
func everyTermOf(constraint *types.Interface, holds func(types.Type) bool) bool {
	for index := range constraint.NumEmbeddeds() {
		switch element := constraint.EmbeddedType(index).(type) {
		case *types.Union:
			if everyUnionTerm(element, holds) {
				return true
			}
		default:
			if nested, isInterface := element.Underlying().(*types.Interface); isInterface {
				if everyTermOf(nested, holds) {
					return true
				}
			} else if holds(element.Underlying()) {
				return true
			}
		}
	}
	return false
}

func everyUnionTerm(union *types.Union, holds func(types.Type) bool) bool {
	for index := range union.Len() {
		if !holds(union.Term(index).Type().Underlying()) {
			return false
		}
	}
	return true
}

// isSharedByIndex is whether an element written by index lands in memory the caller shares: a slice's array, a map,
// or an array behind a pointer. An array passed by value is the function's own copy.
func isSharedByIndex(underlying types.Type) bool {
	switch underlying.(type) {
	case *types.Slice, *types.Map:
		return true
	}
	return isPointerToArray(underlying)
}

func isPointerToArray(underlying types.Type) bool {
	pointer, isPointer := underlying.(*types.Pointer)
	if !isPointer {
		return false
	}
	_, isArray := pointer.Elem().Underlying().(*types.Array)
	return isArray
}

func isPointer(underlying types.Type) bool {
	_, isPointer := underlying.(*types.Pointer)
	return isPointer
}

// readMutatesTags is the parameters function's doc comment makes out-parameters by contract, one per Mutates line
// that names a parameter of function and gives a reason. A line that names no parameter, or gives no reason, is
// reported where it is written and exempts nothing, so a tag can never hide a write without saying why.
func readMutatesTags(context *rule.Context, function *ast.FuncDecl) map[*types.Var]bool {
	outParameters := map[*types.Var]bool{}
	if function.Doc == nil {
		return outParameters
	}
	for _, comment := range function.Doc.List {
		for _, line := range strings.Split(comment.Text, "\n") {
			// The marker counts where it opens a line, after the comment's slashes, stars and spaces, read as the
			// marker package reads a reasoned marker, so a sentence that mentions it exempts nothing.
			opening := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "/*"))
			tag, hasMarker := strings.CutPrefix(opening, marker.Mutates)
			if !hasMarker {
				continue
			}
			tag = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(tag), "*/"))
			// The parameter's name is the first word, ended by a space or a tab, and the reason is the rest.
			parameterName, reason := tag, ""
			if space := strings.IndexFunc(tag, unicode.IsSpace); space >= 0 {
				parameterName, reason = tag[:space], tag[space:]
			}
			parameter := parameterOf(context, function, parameterName)
			switch {
			case parameter == nil:
				report(context, comment, mutatesUnknownParameterMessage)
			case strings.TrimSpace(reason) == "":
				report(context, comment, mutatesWithoutReasonMessage)
			default:
				outParameters[parameter] = true
			}
		}
	}
	return outParameters
}

// parameterOf is function's parameter called name, or nil. The receiver is no parameter here: its writes are never
// judged, so a tag naming it would exempt nothing.
func parameterOf(context *rule.Context, function *ast.FuncDecl, name string) *types.Var {
	if name == "" || name == "_" {
		return nil
	}
	for _, field := range function.Type.Params.List {
		for _, parameterName := range field.Names {
			if parameterName.Name != name {
				continue
			}
			if parameter, isVariable := context.TypesInfo.Defs[parameterName].(*types.Var); isVariable {
				return parameter
			}
		}
	}
	return nil
}

func report(context *rule.Context, node ast.Node, message policy.MessageHandle) {
	context.Report(analysis.Diagnostic{
		Pos:      node.Pos(),
		End:      node.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{}),
	})
}
