// Package go_origin_names names a Go value for where it came from, the one naming the Go rule's judgment and the
// codemods share, so a finding's suggestion and a codemod's rename are the same name.
//
// An error is named for the call that produced it. A table of standard library calls comes first, matched through
// types, never by spelling (`os.ReadFile` gives readError). Then a New, For or Of call folds in its package or
// receiver (`release.For` gives releaseForError), then a declared function gives its own name in full words plus
// Error (`loadConfig` gives loadConfigurationError). A function value gives the value's name, and a call of a
// call's result the function that made it.
//
// A context.Context is named for where it came from: a parameter is the caller's, callerContext, and a local is
// named by the call that made it, from a table matched the same way (`context.WithTimeout` gives timeoutContext).
//
// A call that names nothing says why in a category, which a codemod counts and the rule reads as no name. The
// errors and the contexts share how a call is resolved, how a name splits into words and how a package name is
// spelled, so the two can't drift. It reads go/types and go/ast only, so a rule's pass and a codemod's loaded
// packages can both ask it.
package go_origin_names

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

// ProducingFunction resolves call to the function it calls, with the receiver types a table entry may match
// (the method's declared receiver, and the type of the expression it was called on). It returns a category
// instead when the call has no declared function behind it.
func ProducingFunction(info *types.Info, call *ast.CallExpr) (*types.Func, []TypeName, string) {
	if typeAndValue, found := info.Types[call.Fun]; found && typeAndValue.IsType() {
		return nil, nil, "right side is a conversion"
	}
	switch callee := typeutil.Callee(info, call).(type) {
	case *types.Func:
		function := callee.Origin()
		var receivers []TypeName
		if signature, isSignature := function.Type().(*types.Signature); isSignature && signature.Recv() != nil {
			receivers = append(receivers, NamedTypeOf(signature.Recv().Type()))
		}
		if selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSelector {
			if selection, found := info.Selections[selector]; found {
				receivers = append(receivers, NamedTypeOf(selection.Recv()))
			}
		}
		return function, receivers, ""
	case *types.Builtin:
		return nil, nil, "right side calls a builtin"
	case *types.Var:
		return nil, nil, "right side calls a function value"
	}
	return nil, nil, "right side calls a function expression"
}

// QualifiedName prints function as package.Name or (Receiver).Name, for a report.
func QualifiedName(function *types.Func) string {
	if signature, isSignature := function.Type().(*types.Signature); isSignature && signature.Recv() != nil {
		receiver := NamedTypeOf(signature.Recv().Type())
		if receiver.Name != "" {
			return "(" + receiver.PackagePath + "." + receiver.Name + ")." + function.Name()
		}
		return "(" + signature.Recv().Type().String() + ")." + function.Name()
	}
	if function.Pkg() == nil {
		return function.Name()
	}
	return function.Pkg().Path() + "." + function.Name()
}

// qualifierWords spells an all-lowercase package name in its camel words, so a qualifier reads as the words it
// is made of (`optionschema.For` gives optionSchemaForError, `optionschema.Context` optionSchemaContext). Every
// qualifier, an error's and a context's, passes through it, so a pass reproduces exactly the names the tree holds;
// a new multi-word package that needs splitting is added here, never respelled by hand after a pass.
var qualifierWords = map[string]string{
	"optionschema":  "optionSchema",
	"tooldirectory": "toolDirectory",
}

// QualifierSpelling is a package name's camel spelling, when qualifierWords knows one.
func QualifierSpelling(packageName string) (string, bool) {
	camel, known := qualifierWords[packageName]
	return camel, known
}
