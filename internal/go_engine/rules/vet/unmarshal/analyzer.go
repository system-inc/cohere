// Package unmarshal is vet's unmarshal pass, a value passed to json, xml or asn1's Unmarshal, or to the Decode method
// of json, gob or xml's Decoder, that is neither a pointer nor an interface. Each decodes into what its argument
// points to, so a value that points nowhere is filled by nothing and the call only reports an error at run time. An
// interface, `any` among them, or a type parameter may hold a pointer, so neither is judged.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/unmarshal/unmarshal.go, whose
// logic it keeps, the four encoding packages it leaves alone among them, and whose messages it reports word for
// word, at the same position. The registry runs the port in the original's place, and the original stays the oracle:
// the parity tests beside this file run both over the same packages.
//
// The two differ in one place, a Decode called as a method expression, `(*json.Decoder).Decode(decoder, value)`,
// each case in testdata/src/differences and held by TestPortDiffersFromOriginalOnlyWhereItIsRight. A method
// expression writes the receiver first, so the value decoded into is the second argument. The original reads the
// first, the receiver: it never finds a value there, which the port finds, and it finds a receiver that is a struct
// value promoting Decode, or one argument spreading a call's results, as a non-pointer, wrongly, which the port does
// not.
package unmarshal

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "unmarshal",
	Doc: "report passing non-pointer or non-interface values to unmarshal\n\n" +
		"The unmarshal analysis reports calls to functions such as json.Unmarshal\n" +
		"in which the argument type is not a pointer or an interface.",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/unmarshal",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	switch pass.Pkg.Path() {
	case "encoding/gob", "encoding/json", "encoding/xml", "encoding/asn1":
		// These packages know how to use their own APIs, and their in-package tests, which share the path,
		// sometimes pass a non-pointer on purpose to test what an incorrect program gets. Their external tests,
		// `encoding/json_test` and the like, are judged.
		return nil, nil
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("unmarshal: inspect's result is missing")
	}
	// All visits each call in preorder, as the original's Preorder does, and hands it over typed. Every call is
	// read, in a package that imports no encoding package too, since a Decoder's Decode is a method a package can
	// call on a value another package returns without importing the Decoder's own.
	for call := range inspector.All[*ast.CallExpr](inspected) {
		function := typeutil.StaticCallee(pass.TypesInfo, call)
		if function == nil {
			continue // Not a static call: a call of a function value, an interface's method, a conversion or a builtin.
		}
		decodedIndex := decodedArgumentIndex(function)
		if decodedIndex < 0 {
			continue
		}
		// writtenIndex is where the call writes the argument decoded into: one later in a method expression, which
		// writes the receiver first. The original reads decodedIndex there too, which is the receiver.
		writtenIndex := decodedIndex
		if isMethodExpression(pass.TypesInfo, call) {
			writtenIndex++
		}
		if len(call.Args) < writtenIndex+1 {
			continue // Too few arguments written, as when the call spreads another call's results, `Unmarshal(read())`.
		}
		decoded, recorded := pass.TypesInfo.Types[call.Args[writtenIndex]]
		if !recorded {
			// Types records every expression of a package that type-checks, and a pass runs only over such a
			// package, so this cannot fire. The original would panic here instead.
			continue
		}
		// A type parameter's underlying type is its constraint's interface, so the interface case already holds it;
		// the original names it all the same, and so does the port.
		switch decoded.Type.Underlying().(type) {
		case *types.Pointer, *types.Interface, *types.TypeParam:
			continue
		}
		switch decodedIndex {
		case 0:
			pass.Reportf(call.Lparen, "call of %s passes non-pointer", function.Name())
		case 1:
			pass.Reportf(call.Lparen, "call of %s passes non-pointer as second argument", function.Name())
		}
	}
	return nil, nil
}

// decodedArgumentIndex is the index of the argument function decodes into, 1 for json, xml and asn1's Unmarshal,
// `func([]byte, any) error`, and 0 for the Decode method of json, gob and xml's Decoder, `func(any) error`, and -1
// for every other function. It reads only names and paths, so it allocates nothing, as the original's classification
// does not.
func decodedArgumentIndex(function *types.Func) int {
	receiver := function.Signature().Recv()
	switch {
	case function.Name() == "Unmarshal" && receiver == nil:
		switch function.Pkg().Path() {
		case "encoding/json", "encoding/xml", "encoding/asn1":
			return 1
		}
	case function.Name() == "Decode" && receiver != nil:
		named := receiverNamed(receiver)
		if named == nil {
			// StaticCallee never returns an interface's method, and every other method's receiver is a named type or
			// a pointer to one, so this cannot fire. The original would panic here instead.
			return -1
		}
		if named.Obj().Name() == "Decoder" {
			switch named.Obj().Pkg().Path() {
			case "encoding/json", "encoding/xml", "encoding/gob":
				return 0
			}
		}
	}
	return -1
}

// receiverNamed is the named type of receiver, through one pointer and any alias of either, and nil when it names
// none. x/tools' typesinternal.ReceiverNamed, less whether the receiver is a pointer, which nothing here reads.
func receiverNamed(receiver *types.Var) *types.Named {
	unpointed := receiver.Type()
	if pointer, isPointer := types.Unalias(unpointed).(*types.Pointer); isPointer {
		unpointed = pointer.Elem()
	}
	named, _ := types.Unalias(unpointed).(*types.Named) // A receiver that names no type is nil, which the caller reads.
	return named
}

// isMethodExpression reports whether call calls a method expression, `(*json.Decoder).Decode`, parenthesized or not,
// whose receiver is the first argument written.
func isMethodExpression(information *types.Info, call *ast.CallExpr) bool {
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	selection, selected := information.Selections[selector]
	return selected && selection.Kind() == types.MethodExpr
}
