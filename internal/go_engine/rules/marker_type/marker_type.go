// Package marker_type decides whether a type's methods only mark it, the way a sum type's variants are spelled in Go:
// an interface with an unexported method, `instructionValue()`, and each variant declaring it with an empty body,
// `func (*Await) instructionValue() {}`. Such a method carries no behavior to find; it is Go's spelling of union
// membership, the way a member of a discriminated union is a type in TypeScript, which max-classes-per-file never
// counted. So a variant stays beside its interface, and a file of them serves no type with methods.
//
// The answer lives once because two tools must give it alike: consistency-require-one-type-per-file, which decides
// which types a file serves, and the codemod that splits files by type, which must move exactly the types the rule
// counts and leave the variants where the rule leaves them. The codemod reads syntax alone, with no type information,
// so this works from go/ast and the standard library only. A package under internal/ is importable only from inside
// cohere's module, so the codemod is built from inside cohere's tree to call it.
package marker_type

import "go/ast"

// IsMarkerOnly is whether methods, every method a type declares, are all marker methods, so the type counts as no type
// with methods. A marker method has a receiver, no parameters, an empty body and no results:
// `func (A) kind() int { return 0 }` answers something and is a real method, and so is one with no body at all, which
// is written in assembly. So is `func (noopTracer) Trace(event Event) {}`, empty as it is: a method that takes
// something is an operation its callers ask for, and a null object that performs it as nothing is behavior a reader
// looks for in its own file, where a marker is only a name the type answers to. An empty body is one with no
// statements, so a comment inside it, which an empty function is asked to carry to say why it is empty, keeps it
// empty. A type with no methods at all is no marker type, so IsMarkerOnly of none is false: whether a method-less type
// counts is its caller's question, and a caller that already skips those never asks this.
func IsMarkerOnly(methods []*ast.FuncDecl) bool {
	if len(methods) == 0 {
		return false
	}
	for _, method := range methods {
		if !IsMarkerMethod(method) {
			return false
		}
	}
	return true
}

// IsMarkerMethod is whether method has the shape of one that declares membership and nothing else: a receiver, no
// parameters, no results and an empty body. It lives here, beside IsMarkerOnly, because consistency-no-empty-function
// asks it too, before it reads the type information that tells a sealed union's marker from any other empty method, and
// the two rules must agree on what a marker looks like. NumFields is zero for a nil field list, so it reads a missing
// parameter or result list as an empty one, and an empty result list, `func (A) kind() ()`, declares no result either.
// A parameter, even a blank `_ int`, is a field, so a method taking one is an operation and no marker. In code that
// compiles a method with a result always ends in a return, so its body is never empty, but the results are still
// read: a caller working from syntax alone can be handed `func (A) kind() int {}`, which parses, and it is no marker.
func IsMarkerMethod(method *ast.FuncDecl) bool {
	return method.Recv != nil && method.Body != nil && len(method.Body.List) == 0 &&
		method.Type.Params.NumFields() == 0 && method.Type.Results.NumFields() == 0
}
