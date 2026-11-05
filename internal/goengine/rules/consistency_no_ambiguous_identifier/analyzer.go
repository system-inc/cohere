// Package consistency_no_ambiguous_identifier is the Go engine's rule that a name is a word, not a letter.
//
// A name is read everywhere it is used and declared only once, so the keystrokes a single letter saves are saved at
// the declaration and paid for at every read. The repair is the word the letter stands for: `count`, `rowIndex`,
// `readError`. The Go form of `nexus/consistency-no-ambiguous-identifier`, with its judgment carried over where Go
// keeps it: only a lowercase letter is judged, the coordinates x, y and z keep theirs, and `e` gets its own message
// saying what the rule thinks it holds. Go has no events, so the guess is between an error and something unclear, and
// it is made from the type.
package consistency_no_ambiguous_identifier

import (
	"go/ast"
	"go/token"
	"go/types"
	"unicode"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-ambiguous-identifier"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_ambiguous_identifier",
	Doc: "Flags a single lowercase letter as a name where it is declared: a variable, constant, parameter, result, " +
		"receiver, type parameter, function, method, type or struct field, apart from the coordinates x, y and z, " +
		"because a letter makes every reader find the declaration to learn what it holds. An uppercase letter, a " +
		"type parameter by convention, and generated files are not judged.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var singleLetterMessage = policy.MessageOf(Name, "noSingleLetter")

var ambiguousEMessage = policy.MessageOf(Name, "noAmbiguousE")

// The two guesses an `e` finding can say it made. The TypeScript rule's third, an event, has no Go text: Go has no
// event type for an `e` to hold.
var (
	errorContext   = ambiguousEMessage.Option("context", "error")
	unclearContext = ambiguousEMessage.Option("context", "unclear")
)

// coordinateLetters are the names where the single letter is the conventional spelling and a longer one reads worse.
var coordinateLetters = map[string]bool{"x": true, "y": true, "z": true}

// unclearSuggestion is what an `e` finding offers when nothing around the declaration names what it holds.
const unclearSuggestion = "a descriptive name"

// errorInterface is the predeclared error, which a declared type implements when its value is an error.
var errorInterface = func() *types.Interface {
	errorType, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		// Invariant: the language specification declares error as an interface type.
		panic("the predeclared error is not an interface")
	}
	return errorType
}()

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// A generated file's names are its generator's, and a finding there would be fixed by hand and lost on the
		// next generation.
		if ast.IsGenerated(file) {
			continue
		}
		ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
			switch declaring := node.(type) {
			case *ast.Ident:
				// Defs holds an identifier only where it declares, so a use of a letter declared elsewhere is never
				// reported again, and a name another package declared is never reported here.
				if object := pass.TypesInfo.Defs[declaring]; isJudged(object) {
					judge(pass, declaring, []types.Type{object.Type()}, stack)
				}
			case *ast.TypeSwitchStmt:
				judgeTypeSwitchGuard(pass, declaring, stack)
			}
			return true
		})
	}
	return nil, nil
}

// isJudged is whether object is a declaration this rule reads: a variable, which in go/types is also a parameter,
// result, receiver or struct field, a constant, a type or type parameter, or a function or method. An embedded field
// is named by its type, which is judged where that type is declared. A label and an import's name are left alone.
func isJudged(object types.Object) bool {
	switch declared := object.(type) {
	case *types.Var:
		return !declared.Embedded()
	case *types.Const, *types.TypeName, *types.Func:
		return true
	}
	return false
}

// judgeTypeSwitchGuard judges the name a type switch binds, `switch value := subject.(type)`. go/types gives that
// identifier no object of its own: each case clause holds an implicit one, typed for its case, so the name is judged
// here once with every clause's type.
func judgeTypeSwitchGuard(pass *analysis.Pass, statement *ast.TypeSwitchStmt, stack []ast.Node) {
	assignment, isAssignment := statement.Assign.(*ast.AssignStmt)
	if !isAssignment || len(assignment.Lhs) != 1 {
		return
	}
	identifier, isIdentifier := assignment.Lhs[0].(*ast.Ident)
	if !isIdentifier {
		return
	}
	var clauseTypes []types.Type
	for _, clause := range statement.Body.List {
		if object := pass.TypesInfo.Implicits[clause]; object != nil {
			clauseTypes = append(clauseTypes, object.Type())
		}
	}
	judge(pass, identifier, clauseTypes, append(stack, statement, assignment))
}

// judge reports identifier when it is a single letter other than a coordinate. declaredTypes is every type the name
// holds, one except in a type switch, and stack is the nodes enclosing identifier, its parent last.
func judge(pass *analysis.Pass, identifier *ast.Ident, declaredTypes []types.Type, stack []ast.Node) {
	// One lowercase ASCII letter, the line both twins draw: an uppercase letter is a type parameter by convention, so
	// the TypeScript and Swift rules leave it alone and the three engines judge the same names. The blank identifier is
	// out too: it is Go's grammar for no name, not a name.
	if len(identifier.Name) != 1 || identifier.Name[0] < 'a' || identifier.Name[0] > 'z' {
		return
	}
	if coordinateLetters[identifier.Name] {
		return
	}
	if identifier.Name != "e" {
		pass.Report(analysis.Diagnostic{
			Pos:      identifier.Pos(),
			End:      identifier.End(),
			Category: singleLetterMessage.Id,
			Message:  singleLetterMessage.Render(map[string]string{"name": identifier.Name}),
		})
		return
	}
	suggestedName, context := suggestionFor(pass, identifier, declaredTypes, stack)
	pass.Report(analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: ambiguousEMessage.Id,
		Message:  ambiguousEMessage.Render(map[string]string{"suggestedName": suggestedName}, context),
	})
}

// suggestionFor is the name an `e` finding offers and the guess it says it made. An `e` whose every type implements
// error is an error, named for what failed: the call that produced it (`closeError`), the type a receiver is
// (`parseError`), or the function it is declared in (`loadError`). Anything else is unclear, and the rule says so
// rather than guess a word for it.
func suggestionFor(
	pass *analysis.Pass,
	identifier *ast.Ident,
	declaredTypes []types.Type,
	stack []ast.Node,
) (string, policy.MessageOption) {
	if len(declaredTypes) == 0 {
		return unclearSuggestion, unclearContext
	}
	for _, declaredType := range declaredTypes {
		if !types.Implements(declaredType, errorInterface) {
			return unclearSuggestion, unclearContext
		}
	}
	if call := producingCall(identifier, stack[len(stack)-1]); call != nil {
		// The callee's object, so a method, a qualified function and a function held in a variable all give their own
		// name, and a call of a literal, which has none, falls through. So does an error constructor, whose name says
		// only that an error was made, never what failed.
		if callee := typeutil.Callee(pass.TypesInfo, call); callee != nil && !isErrorConstructor(callee) {
			return asCode(lowerCamel(callee.Name()) + "Error"), errorContext
		}
	}
	for index := len(stack) - 1; index >= 0; index-- {
		function, isFunction := stack[index].(*ast.FuncDecl)
		if !isFunction {
			continue
		}
		// A receiver is named for its type in house style, `analyzer *Analyzer`, and an error type's method is
		// usually Error, which would make the function's name a poor suggestion.
		if function.Recv != nil && function.Recv.Pos() <= identifier.Pos() && identifier.End() <= function.Recv.End() {
			if typeName := receiverTypeName(declaredTypes[0]); typeName != "" {
				return asCode(lowerCamel(typeName)), errorContext
			}
		}
		return asCode(lowerCamel(function.Name.Name) + "Error"), errorContext
	}
	return unclearSuggestion, errorContext
}

// producingCall is the call whose result identifier is declared to hold, `e := call()`, `_, e := call()` or
// `var e = call()`, or nil when the declaration is any other shape.
func producingCall(identifier *ast.Ident, parent ast.Node) *ast.CallExpr {
	var declared []ast.Expr
	var values []ast.Expr
	switch declaration := parent.(type) {
	case *ast.AssignStmt:
		if declaration.Tok != token.DEFINE {
			return nil
		}
		declared, values = declaration.Lhs, declaration.Rhs
	case *ast.ValueSpec:
		for _, name := range declaration.Names {
			declared = append(declared, name)
		}
		values = declaration.Values
	default:
		return nil
	}
	// One call on the right is the shape where every name on the left is one of its results.
	if len(values) != 1 {
		return nil
	}
	for _, expression := range declared {
		if expression == identifier {
			call, isCall := ast.Unparen(values[0]).(*ast.CallExpr)
			if !isCall {
				return nil
			}
			return call
		}
	}
	return nil
}

// errorConstructors are the standard library functions that build an error from their arguments, by package path and
// name. Named for the call, `e := fmt.Errorf(...)` would suggest `errorfError`.
var errorConstructors = map[string]map[string]bool{
	"errors": {"New": true, "Join": true},
	"fmt":    {"Errorf": true},
}

// isErrorConstructor is whether callee is one of errorConstructors, a package-level function of that package, so a
// method or a function of ours spelled the same is never mistaken for it.
func isErrorConstructor(callee types.Object) bool {
	function, isFunction := callee.(*types.Func)
	if !isFunction || function.Pkg() == nil || function.Signature().Recv() != nil {
		return false
	}
	return errorConstructors[function.Pkg().Path()][function.Name()]
}

// receiverTypeName is the name of the type a receiver of declaredType is a value or pointer of, or empty when it is
// not a named type.
func receiverTypeName(declaredType types.Type) string {
	if pointer, isPointer := types.Unalias(declaredType).(*types.Pointer); isPointer {
		declaredType = pointer.Elem()
	}
	if named, isNamed := types.Unalias(declaredType).(*types.Named); isNamed {
		return named.Obj().Name()
	}
	return ""
}

// lowerCamel is name spelled as a local is, its opening capitals lowered: ReadFile becomes readFile, and an initialism
// that opens a longer name lowers up to the capital that opens the next word, so HTTPGet becomes httpGet.
func lowerCamel(name string) string {
	runes := []rune(name)
	opening := 0
	for opening < len(runes) && unicode.IsUpper(runes[opening]) {
		opening++
	}
	if opening > 1 && opening < len(runes) {
		opening--
	}
	for index := range opening {
		runes[index] = unicode.ToLower(runes[index])
	}
	return string(runes)
}

// asCode is a suggested name as the Go message spells code, between backticks, which the unclear suggestion, a phrase
// rather than a name, goes without.
func asCode(name string) string {
	return "`" + name + "`"
}
