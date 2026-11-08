// Package std_methods is vet's stdmethods pass, a method named for one of the standard library's well-known interface
// methods, `Format`, `MarshalJSON`, `ReadByte` and the rest, whose signature does not match that interface's. Such a
// method is usually meant to satisfy the interface, and the standard library looks for it with a dynamic check, so a
// wrong signature is no compile error: the check just fails, sometimes mysteriously. A method whose first parameter
// is not the interface's signal type, a `Scan` taking no `fmt.ScanState`, is taken to mean something else and left
// alone, as are `WriteTo` with more than one parameter, and `Is`, `As` and `Unwrap` on a type that is not an error.
// It sits on the engine's syntax dispatcher, hearing each function declaration and interface type on the walk the
// dispatcher makes once for every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/stdmethods/stdmethods.go, whose
// table of canonical signatures it keeps entry for entry, whose logic it keeps, and whose messages it reports word for
// word, at the same position. The registry runs the port in the original's place, and the original stays the oracle:
// the parity tests beside this file run both over the same packages. The two agree everywhere, so there is no
// differences fixture.
package std_methods

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "stdmethods"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check signature of methods of well-known interfaces\n\n" +
	"Sometimes a type may be intended to satisfy an interface but may fail to\n" +
	"do so because of a mistake in its method signature.\n" +
	"For example, the result of this WriteTo method should be (int64, error),\n" +
	"not error, to satisfy io.WriterTo:\n\n" +
	"\ttype myWriterTo struct{...}\n" +
	"\tfunc (myWriterTo) WriteTo(w io.Writer) error { ... }\n\n" +
	"This check ensures that each method whose name matches one of several\n" +
	"well-known interface methods from the standard library has the correct\n" +
	"signature for that interface.\n\n" +
	"Checked method names include:\n\n" +
	"\tFormat GobEncode GobDecode MarshalJSON MarshalXML\n" +
	"\tPeek ReadByte ReadFrom ReadRune Scan Seek\n" +
	"\tUnmarshalJSON UnreadByte UnreadRune WriteByte\n" +
	"\tWriteTo"

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/stdmethods"

// Listener is what the port hears of each package from the dispatcher: every function declaration and interface type
// together, in the one preorder the original's Preorder visits them, so the findings come in the original's order,
// where two walks would visit one kind, then the other.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.FuncDecl)(nil), (*ast.InterfaceType)(nil)},
	Node:  checkDeclaration,
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

// signalPrefix opens a parameter type in canonicalMethods that signals the canonical meaning is intended.
const signalPrefix = "="

// canonicalMethods is, by method name, the parameter and result types of each method the standard library finds with
// a dynamic interface check, written as typeString writes a type, so a method with a listed name and other types would
// fail that check at run time rather than fail to compile. x/tools' canonicalMethods, entry for entry.
//
// A few of these names are common, and a `Scan` method may have nothing to do with fmt.Scanner. A parameter type
// opening with signalPrefix is the signal that the canonical meaning is intended: a method whose types at those places
// differ is left alone, and one whose types there match must match everywhere.
var canonicalMethods = map[string]struct{ parameters, results []string }{
	"As": {[]string{"any"}, []string{"bool"}}, // errors.As
	// "Flush": {{}, {"error"}}, // http.Flusher and jpeg.writer conflict, so x/tools leaves Flush out.
	"Format":        {[]string{"=fmt.State", "rune"}, []string{}},                      // fmt.Formatter
	"GobDecode":     {[]string{"[]byte"}, []string{"error"}},                           // gob.GobDecoder
	"GobEncode":     {[]string{}, []string{"[]byte", "error"}},                         // gob.GobEncoder
	"Is":            {[]string{"error"}, []string{"bool"}},                             // errors.Is
	"MarshalJSON":   {[]string{}, []string{"[]byte", "error"}},                         // json.Marshaler
	"MarshalXML":    {[]string{"*xml.Encoder", "xml.StartElement"}, []string{"error"}}, // xml.Marshaler
	"ReadByte":      {[]string{}, []string{"byte", "error"}},                           // io.ByteReader
	"ReadFrom":      {[]string{"=io.Reader"}, []string{"int64", "error"}},              // io.ReaderFrom
	"ReadRune":      {[]string{}, []string{"rune", "int", "error"}},                    // io.RuneReader
	"Scan":          {[]string{"=fmt.ScanState", "rune"}, []string{"error"}},           // fmt.Scanner
	"Seek":          {[]string{"=int64", "int"}, []string{"int64", "error"}},           // io.Seeker
	"UnmarshalJSON": {[]string{"[]byte"}, []string{"error"}},                           // json.Unmarshaler
	"UnmarshalXML":  {[]string{"*xml.Decoder", "xml.StartElement"}, []string{"error"}}, // xml.Unmarshaler
	"UnreadByte":    {[]string{}, []string{"error"}},
	"UnreadRune":    {[]string{}, []string{"error"}},
	"Unwrap":        {[]string{}, []string{"error"}},                      // errors.Unwrap
	"WriteByte":     {[]string{"byte"}, []string{"error"}},                // jpeg.writer (matching bufio.Writer)
	"WriteTo":       {[]string{"=io.Writer"}, []string{"int64", "error"}}, // io.WriterTo
}

// errorInterface is the predeclared error's interface, which a type implements to have `Is`, `As` and `Unwrap` judged.
var errorInterface = predeclaredErrorInterface()

// predeclaredErrorInterface is the interface underlying the universe's error. x/tools' errorType.
func predeclaredErrorInterface() *types.Interface {
	underlying, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		// Invariant: the language declares error as an interface type.
		panic("std_methods: the predeclared error is not an interface")
	}
	return underlying
}

// checkDeclaration judges the method a function declaration with a receiver declares, and each method an interface
// type declares.
func checkDeclaration(context *rule.Context, node ast.Node, _ []ast.Node) {
	switch node := node.(type) {
	case *ast.FuncDecl:
		// A function declaration without a receiver is a plain function, whose name implements no interface.
		if node.Recv != nil {
			judgeMethod(context, node.Name)
		}
	case *ast.InterfaceType:
		// An embedded interface or a type union in a constraint is a field without names, and declares no method.
		for _, field := range node.Methods.List {
			for _, name := range field.Names {
				judgeMethod(context, name)
			}
		}
	}
}

// judgeMethod reports the method name declares when canonicalMethods lists its name and it signals the canonical
// meaning but its signature does not match. x/tools' canonicalMethod.
func judgeMethod(context *rule.Context, name *ast.Ident) {
	expected, canonical := canonicalMethods[name.Name]
	if !canonical {
		return
	}
	method, isFunction := context.TypesInfo.Defs[name].(*types.Func)
	if !isFunction {
		// Invariant: the checker records the *types.Func every method declaration and interface method defines.
		panic("std_methods: a method's name defines no function")
	}
	signature, isSignature := method.Type().(*types.Signature)
	if !isSignature {
		// Invariant: a *types.Func's type is always its *types.Signature.
		panic("std_methods: a method's type is not a signature")
	}

	// A WriteTo with more than one parameter is not trying to implement io.WriterTo at all, and comes up often enough
	// to leave alone.
	if name.Name == "WriteTo" && signature.Params().Len() > 1 {
		return
	}

	// Is, As and Unwrap mean what errors' functions look for only on a type that implements error. Every method,
	// declared or in an interface, has a receiver, so the missing one is the original's guard, kept for fidelity, and
	// no input reaches it.
	if name.Name == "Is" || name.Name == "As" || name.Name == "Unwrap" {
		if receiver := signature.Recv(); receiver == nil || !types.Implements(receiver.Type(), errorInterface) {
			return
		}
	}

	// Unwrap has two canonical signatures, `Unwrap() error` and `Unwrap() []error`, which the table cannot hold, so it
	// is judged here alone.
	if name.Name == "Unwrap" {
		if signature.Params().Len() == 0 && signature.Results().Len() == 1 {
			result := typeString(signature.Results().At(0).Type())
			if result == "error" || result == "[]error" {
				return
			}
		}
		context.ReportRangef(name, "method Unwrap() should have signature Unwrap() error or Unwrap() []error")
		return
	}

	// Only a method whose signal types all match means the canonical method, and is judged further.
	if !matchesTypes(expected.parameters, signature.Params(), signalPrefix) ||
		!matchesTypes(expected.results, signature.Results(), signalPrefix) {
		return
	}

	if !matchesTypes(expected.parameters, signature.Params(), "") ||
		!matchesTypes(expected.results, signature.Results(), "") {
		expectedSignature := name.Name + "(" + joinedTypes(expected.parameters) + ")"
		// One result is written bare and several in parentheses, as Go writes them; none adds nothing.
		if len(expected.results) == 1 {
			expectedSignature += " " + joinedTypes(expected.results)
		} else if len(expected.results) > 1 {
			expectedSignature += " (" + joinedTypes(expected.results) + ")"
		}
		// The signature as declared, parameter names and all, after the method's name in place of `func`.
		actualSignature := name.Name + strings.TrimPrefix(typeString(signature), "func")
		context.ReportRangef(name, "method %s should have signature %s", actualSignature, expectedSignature)
	}
}

// typeString is a type as the table writes one, each package named by its name rather than its path, so `fmt.State`
// matches whatever the importing file calls fmt. x/tools' typeString.
func typeString(written types.Type) string {
	return types.TypeString(written, (*types.Package).Name)
}

// joinedTypes is a list of the table's types as a signature writes them, without their signal prefixes. x/tools'
// argjoin.
func joinedTypes(expected []string) string {
	unprefixed := make([]string, len(expected))
	for index, expectedType := range expected {
		unprefixed[index] = strings.TrimPrefix(expectedType, signalPrefix)
	}
	return strings.Join(unprefixed, ", ")
}

// matchesTypes is whether each type in expected that opens with prefix matches the type at its place in actual. With
// the empty prefix every type is compared, and actual may hold no more types than expected. x/tools' matchParams.
func matchesTypes(expected []string, actual *types.Tuple, prefix string) bool {
	for index, expectedType := range expected {
		if !strings.HasPrefix(expectedType, prefix) {
			continue
		}
		if index >= actual.Len() || !matchesType(expectedType, actual.At(index).Type()) {
			return false
		}
	}
	return prefix != "" || actual.Len() <= len(expected)
}

// matchesType is whether actual is written as expected, taking `any` and `interface{}` as one type. x/tools'
// matchParamType.
func matchesType(expected string, actual types.Type) bool {
	unprefixed := strings.TrimPrefix(expected, signalPrefix)
	// Comparing written types is more than a type identity needs, and as easy.
	written := typeString(actual)
	return written == unprefixed ||
		(written == "any" || written == "interface{}") && (unprefixed == "any" || unprefixed == "interface{}")
}
