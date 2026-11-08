// Package unmarshal is vet's unmarshal pass, a value passed to json, xml or asn1's Unmarshal, or to the Decode method
// of json, gob or xml's Decoder, that is neither a pointer nor an interface. Each decodes into what its argument
// points to, so a value that points nowhere is filled by nothing and the call only reports an error at run time. An
// interface, `any` among them, or a type parameter may hold a pointer, so neither is judged. It sits on the engine's
// syntax dispatcher, hearing each call on the walk the dispatcher makes once for every syntax rule.
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
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "unmarshal"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "report passing non-pointer or non-interface values to unmarshal\n\n" +
	"The unmarshal analysis reports calls to functions such as json.Unmarshal\n" +
	"in which the argument type is not a pointer or an interface."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/unmarshal"

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, as the original's
// Preorder visits them, of which it judges those decoding into a value. Every call is heard, in a package that imports
// no encoding package too, since a Decoder's Decode is a method a package can call on a value another package returns
// without importing the Decoder's own.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node:  checkCall,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	return analyzer
}

// packageState is what the port keeps over one package: whether it is one of the encoding packages it leaves alone,
// read once, on the first call heard, since the listener has no hook as the walk enters the package.
type packageState struct {
	// isRead is whether isLeftAlone has been read from the package's path.
	isRead bool
	// isLeftAlone is whether the package is one of the four encoding packages, whose calls are not judged.
	isLeftAlone bool
}

// checkCall reports node when it passes a value that is neither a pointer nor an interface to an Unmarshal or a
// Decoder's Decode, outside the encoding packages.
func checkCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	state := rule.State[packageState](context)
	if !state.isRead {
		state.isRead = true
		switch context.Package.Path() {
		case "encoding/gob", "encoding/json", "encoding/xml", "encoding/asn1":
			// These packages know how to use their own APIs, and their in-package tests, which share the path,
			// sometimes pass a non-pointer on purpose to test what an incorrect program gets. Their external tests,
			// `encoding/json_test` and the like, are judged.
			state.isLeftAlone = true
		}
	}
	if state.isLeftAlone {
		return
	}
	function := typeutil.StaticCallee(context.TypesInfo, call)
	if function == nil {
		return // Not a static call: a call of a function value, an interface's method, a conversion or a builtin.
	}
	decodedIndex := decodedArgumentIndex(function)
	if decodedIndex < 0 {
		return
	}
	// writtenIndex is where the call writes the argument decoded into: one later in a method expression, which writes
	// the receiver first. The original reads decodedIndex there too, which is the receiver.
	writtenIndex := decodedIndex
	if isMethodExpression(context.TypesInfo, call) {
		writtenIndex++
	}
	if len(call.Args) < writtenIndex+1 {
		return // Too few arguments written, as when the call spreads another call's results, `Unmarshal(read())`.
	}
	decoded, recorded := context.TypesInfo.Types[call.Args[writtenIndex]]
	if !recorded {
		// Types records every expression of a package that type-checks, and a pass runs only over such a package,
		// so this cannot fire. The original would panic here instead.
		return
	}
	// A type parameter's underlying type is its constraint's interface, so the interface case already holds it; the
	// original names it all the same, and so does the port.
	switch decoded.Type.Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.TypeParam:
		return
	}
	switch decodedIndex {
	case 0:
		context.Reportf(call.Lparen, "call of %s passes non-pointer", function.Name())
	case 1:
		context.Reportf(call.Lparen, "call of %s passes non-pointer as second argument", function.Name())
	}
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
