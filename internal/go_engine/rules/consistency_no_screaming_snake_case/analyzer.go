// Package consistency_no_screaming_snake_case is the Go engine's rule that a name we declare is spelled in MixedCaps,
// with no underscore inside it, and that a value local to a function opens lower-case.
//
// Go already says whether a name is exported by the case of its first letter, and a capital already marks where one
// word ends and the next begins, so `MAX_RETRIES` shouts a fact the casing carries and `max_retries` marks its word
// boundary twice. The Go form of `nexus/consistency-no-screaming-snake-case`, widened to every underscore because Go
// has no file-local casing to argue for one: Effective Go spells every name in MixedCaps. Where the underscore is
// someone else's grammar it is left alone: the blank identifier, the parts of a test function's name go test and go
// doc read, a name cgo or the linker sees, a constant named exactly as one of syscall's or golang.org/x/sys's it
// mirrors, and a name read from an external key spelled exactly as the name is, `os.Getenv("DATABASE_URL")`. A local
// value's capital first letter would say exported, which a local never is, so it is reported too.
//
// Both exemptions hold only at the other system's exact spelling (P4 of the text rules' decisions): the shouting form
// earns its place by being greppable across the boundary, and a name spelled otherwise matches nothing on the other
// side. So `O_RDONLY = syscall.O_RDONLY` is exempt, and `O_RDONLY_MODE = syscall.O_RDONLY` is reported (us08), as is
// `O_RDONLY = syscall.O_WRONLY`, which spells a constant its value does not name.
package consistency_no_screaming_snake_case

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-screaming-snake-case"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_screaming_snake_case",
	Doc: "Flags a name we declare that is in SCREAMING_SNAKE_CASE or has an underscore in it, suggesting its " +
		"MixedCaps form with exported names kept exported, and a value local to a function that opens with a " +
		"capital, suggesting its lowerCamel form, because Go marks word boundaries and export with capitals. Not " +
		"judged: the blank identifier, an example's name, the test, benchmark or fuzz prefix of a test's name, a " +
		"name an //export or //go:linkname directive gives out, a constant named exactly as the constant of " +
		"syscall or golang.org/x/sys it is assigned from or compared against, a name declared from a call whose " +
		"constant string argument is exactly that name (os.Getenv(\"DATABASE_URL\")), and a name whose " +
		"suggested form would not be an identifier (type_ would be the keyword type).",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var screamingSnakeCaseMessage = policy.MessageOf(Name, "screamingSnakeCase")

var underscoreInNameMessage = policy.MessageOf(Name, "underscoreInName")

var capitalizedLocalValueMessage = policy.MessageOf(Name, "capitalizedLocalValue")

// testPrefixes open the names go test runs as tests. The prefix is go test's grammar and the rest of the name is ours,
// so TestParse_line is judged as Parse_line and suggests TestParseLine.
var testPrefixes = []string{"Test", "Benchmark", "Fuzz"}

// examplePrefix opens an example's name, which go doc reads whole: ExampleType_Method attaches the example to a method,
// and Example_suffix names one of several, so every underscore in it is go doc's grammar.
const examplePrefix = "Example"

func run(pass *analysis.Pass) (any, error) {
	mirrored := mirroredConstants(pass)
	keyed := keyMirrors(pass)
	directed := directedNames(pass)
	for _, file := range pass.Files {
		// The file's own name, not one a //line directive gives it, since only a _test.go file holds tests.
		isTestFile := strings.HasSuffix(pass.Fset.PositionFor(file.Package, false).Filename, "_test.go")
		ast.Inspect(file, func(node ast.Node) bool {
			switch declaring := node.(type) {
			case *ast.Ident:
				// Defs holds an identifier only where it declares, so a use is never judged, and a name another
				// package declared, `syscall.O_RDONLY` or `unix.Stat_t`, is never reported here.
				object := pass.TypesInfo.Defs[declaring]
				if !isJudged(object) || mirrored[object] || keyed[object] {
					return true
				}
				if object.Parent() == pass.Pkg.Scope() && directed[object.Name()] {
					return true
				}
				judge(pass, declaring, isExported(object), isTestFile && isTopLevelFunction(object),
					isLocalValue(pass, object))
			case *ast.TypeSwitchStmt:
				// go/types gives the name a type switch binds, `switch value := subject.(type)`, no object of its own,
				// only one per case clause, so it is judged here. It is a value local to the switch, so never exported.
				if identifier := typeSwitchGuard(declaring); identifier != nil {
					judge(pass, identifier, false, false, true)
				}
			}
			return true
		})
	}
	return nil, nil
}

// isJudged is whether object is a declaration this rule reads: a variable, which in go/types is also a parameter,
// result, receiver or struct field, a constant, a type or type parameter, a function or method, or a label. An
// embedded field is named by its type, which is judged where that type is declared, and often is another package's.
// An import's name is left alone: it usually repeats the package's own name, which is the package's to choose.
func isJudged(object types.Object) bool {
	switch declared := object.(type) {
	case *types.Var:
		return !declared.Embedded()
	case *types.Const, *types.TypeName, *types.Func, *types.Label:
		return true
	}
	return false
}

// isExported is whether object is exported in the sense Go's casing marks: a package-level name, a field or a method,
// spelled with a capital. A type parameter keeps the case its author gave it, a capital by convention, since its case
// marks nothing. Any other name is local to a function, so its MixedCaps form opens lowercase whatever case it had.
func isExported(object types.Object) bool {
	switch declared := object.(type) {
	case *types.Var:
		if declared.IsField() {
			return declared.Exported()
		}
	case *types.Func:
		if declared.Signature().Recv() != nil {
			return declared.Exported()
		}
	case *types.TypeName:
		if _, isTypeParameter := declared.Type().(*types.TypeParam); isTypeParameter {
			return declared.Exported()
		}
	}
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope() && object.Exported()
}

// isLocalValue is whether object is a value declared inside a function or a signature: a constant or a variable,
// which in go/types is also a parameter, a result, a receiver and a range binding. Such a name is never exported
// whatever its case, so a capital first letter on it marks nothing Go can read. A field is a struct's, and Go's
// casing exports it, and a type is UpperCamel wherever it is declared, so neither is a local value.
func isLocalValue(pass *analysis.Pass, object types.Object) bool {
	switch declared := object.(type) {
	case *types.Var:
		if declared.IsField() {
			return false
		}
	case *types.Const:
	default:
		return false
	}
	return object.Parent() != nil && object.Parent() != pass.Pkg.Scope()
}

// isTopLevelFunction is whether object is a package-level function, the only kind go test runs or go doc reads as an
// example.
func isTopLevelFunction(object types.Object) bool {
	function, isFunction := object.(*types.Func)
	return isFunction && function.Signature().Recv() == nil && function.Pkg() != nil &&
		function.Parent() == function.Pkg().Scope()
}

// typeSwitchGuard is the identifier a type switch binds, or nil when it binds none.
func typeSwitchGuard(statement *ast.TypeSwitchStmt) *ast.Ident {
	assignment, isAssignment := statement.Assign.(*ast.AssignStmt)
	if !isAssignment || len(assignment.Lhs) != 1 {
		return nil
	}
	identifier, isIdentifier := assignment.Lhs[0].(*ast.Ident)
	if !isIdentifier {
		return nil
	}
	return identifier
}

// judge reports identifier when its name has an underscore in it, suggesting the MixedCaps form, or when it names a
// local value and opens with a capital, suggesting the lowerCamel form. exported decides whether the MixedCaps form
// opens with a capital, testFunction is whether identifier names a top-level function in a _test.go file, whose test
// prefix or example name is go's grammar rather than ours, and localValue is whether it names a value declared inside
// a function.
func judge(pass *analysis.Pass, identifier *ast.Ident, exported bool, testFunction bool, localValue bool) {
	// The blank identifier is Go's grammar for no name, not a name.
	if identifier.Name == "_" {
		return
	}
	if !strings.Contains(identifier.Name, "_") {
		if localValue {
			judgeLocalOpening(pass, identifier)
		}
		return
	}
	prefix := ""
	if testFunction {
		if isGoTestName(identifier.Name, examplePrefix) {
			return
		}
		for _, testPrefix := range testPrefixes {
			if isGoTestName(identifier.Name, testPrefix) {
				prefix = testPrefix
				break
			}
		}
	}
	body := strings.TrimPrefix(identifier.Name, prefix)
	// What follows a test prefix must open with a capital for go test to still run it, so it is spelled as exported.
	suggestion := prefix + mixedCaps(body, exported || prefix != "")
	// A name that only escapes a keyword, type_, or is nothing but underscores, or would open with a digit, has no
	// MixedCaps form that compiles, and a suggestion that cannot be typed is worse than none.
	if !token.IsIdentifier(suggestion) {
		return
	}
	message := underscoreInNameMessage
	if isScreamingSnakeCase(body) {
		message = screamingSnakeCaseMessage
	}
	report(pass, identifier, message, suggestion)
}

// judgeLocalOpening reports a local value whose name, with no underscore in it, opens with a capital: `MaxSize :=
// 10` inside a function. Go reads a capital first letter as exported, which a local can never be, so the lowerCamel
// form is suggested, its opening lowered as an unexported MixedCaps name's is.
func judgeLocalOpening(pass *analysis.Pass, identifier *ast.Ident) {
	first, _ := utf8.DecodeRuneInString(identifier.Name)
	if !unicode.IsUpper(first) {
		return
	}
	suggestion := mixedCaps(identifier.Name, false)
	// Lowered, a name can become a keyword, Type to type, and a suggestion that cannot be typed is worse than none.
	if !token.IsIdentifier(suggestion) {
		return
	}
	report(pass, identifier, capitalizedLocalValueMessage, suggestion)
}

func report(pass *analysis.Pass, identifier *ast.Ident, message policy.MessageHandle, suggestion string) {
	pass.Report(analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{"name": identifier.Name, "suggestion": suggestion}),
	})
}

// isGoTestName is go test's own reading of a name with prefix: the prefix alone, or followed by anything but a
// lower-case letter, so Testify is a helper and Test_parse a test.
func isGoTestName(name string, prefix string) bool {
	rest, found := strings.CutPrefix(name, prefix)
	if !found {
		return false
	}
	if rest == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(first)
}

// isScreamingSnakeCase is the shouting shape, as the TypeScript twin reads it: a capital first, then capitals, digits
// and underscores, with at least one underscore and none last. Any other name with an underscore gets the plainer
// message about the underscore.
func isScreamingSnakeCase(name string) bool {
	if !strings.Contains(name, "_") || strings.HasSuffix(name, "_") {
		return false
	}
	for index, character := range name {
		if index == 0 && !unicode.IsUpper(character) {
			return false
		}
		if !unicode.IsUpper(character) && !unicode.IsDigit(character) && character != '_' {
			return false
		}
	}
	return true
}

// mixedCaps is name with its underscores folded into word boundaries: each word after the first raised, and the first
// opening with a capital only when exported. A word with no lower-case letter has no inner boundary to keep, so it is
// lowered whole first: `MAX_RETRIES` becomes `MaxRetries` and `parse_URL` `parseUrl`, never `MAXRETRIES` or
// `parseURL`. A word in mixed case keeps its inside, since its capitals are boundaries already. Only the casing
// changes, never the words.
func mixedCaps(name string, exported bool) string {
	var words []string
	for _, part := range strings.Split(name, "_") {
		if part == "" {
			continue
		}
		if strings.IndexFunc(part, unicode.IsLower) < 0 {
			part = strings.ToLower(part)
		}
		words = append(words, part)
	}
	if len(words) == 0 {
		return ""
	}
	var builder strings.Builder
	if exported {
		builder.WriteString(raiseFirstCharacter(words[0]))
	} else {
		builder.WriteString(lowerOpening(words[0]))
	}
	for _, word := range words[1:] {
		builder.WriteString(raiseFirstCharacter(word))
	}
	return builder.String()
}

func raiseFirstCharacter(word string) string {
	first, size := utf8.DecodeRuneInString(word)
	return string(unicode.ToUpper(first)) + word[size:]
}

// lowerOpening lowers a word's opening capitals, up to the capital that opens its next word, so Http becomes http and
// HTTPClient httpClient rather than hTTPClient.
func lowerOpening(word string) string {
	runes := []rune(word)
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

// isSystemPackage is whether path is syscall or a package of golang.org/x/sys, whose constants keep the operating
// system's spelling, `O_RDONLY` and `SIGTERM`, so a reader can find them in the system's own documentation.
func isSystemPackage(path string) bool {
	return path == "syscall" || path == "golang.org/x/sys" || strings.HasPrefix(path, "golang.org/x/sys/")
}

// mirroredConstants is every constant declared in a file importing a system package that mirrors one of that
// package's constants, decided by types and spelled exactly as it: its value is computed from the system constant of
// its own name, `O_CLOEXEC = unix.O_CLOEXEC`, or it is compared directly against that constant somewhere in the
// package. Such a name keeps the system's spelling so it reads as the thing it stands for. `O_CLOEXEC_FLAGS` is not
// the system's spelling of anything, so it mirrors nothing, and a constant in such a file with no such tie is judged
// like any other.
func mirroredConstants(pass *analysis.Pass) map[types.Object]bool {
	systemFiles := map[*token.File]bool{}
	for _, file := range pass.Files {
		for _, specification := range file.Imports {
			if path, unquoteError := strconv.Unquote(specification.Path.Value); unquoteError == nil &&
				isSystemPackage(path) {
				systemFiles[pass.Fset.File(file.Package)] = true
			}
		}
	}
	mirrored := map[types.Object]bool{}
	if len(systemFiles) == 0 {
		return mirrored
	}
	isOurs := func(object types.Object) bool {
		constant, isConstant := object.(*types.Const)
		return isConstant && constant.Pkg() == pass.Pkg && systemFiles[pass.Fset.File(constant.Pos())]
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch inspected := node.(type) {
			case *ast.GenDecl:
				if inspected.Tok == token.CONST && systemFiles[pass.Fset.File(inspected.Pos())] {
					markAssignedConstants(pass, inspected, mirrored)
				}
			case *ast.BinaryExpr:
				if !isComparison(inspected.Op) {
					return true
				}
				left, right := constantOf(pass, inspected.X), constantOf(pass, inspected.Y)
				// Both sides are constants here, so neither is nil when the names are read.
				if isOurs(left) && isSystemConstant(right) && left.Name() == right.Name() {
					mirrored[left] = true
				}
				if isOurs(right) && isSystemConstant(left) && right.Name() == left.Name() {
					mirrored[right] = true
				}
			}
			return true
		})
	}
	return mirrored
}

// markAssignedConstants marks each constant in declaration whose own value mentions the system constant of its name,
// so `O_RDONLY = uint32(syscall.O_RDONLY)` keeps the system's spelling through a conversion. A specification with no
// values repeats the last one that had them, as Go reads a constant group, but that value is spelled for the name it
// was written beside, never the next one, so only a specification's own values are read.
//
// Mutates: mirrored gains each constant whose value mentions the system constant of its name, for the caller to leave
// unreported
func markAssignedConstants(pass *analysis.Pass, declaration *ast.GenDecl, mirrored map[types.Object]bool) {
	for _, specification := range declaration.Specs {
		valueSpecification, isValueSpecification := specification.(*ast.ValueSpec)
		if !isValueSpecification {
			continue
		}
		for index, name := range valueSpecification.Names {
			if index < len(valueSpecification.Values) &&
				mentionsSystemConstant(pass, valueSpecification.Values[index], name.Name) {
				mirrored[pass.TypesInfo.Defs[name]] = true
			}
		}
	}
}

// mentionsSystemConstant is whether expression uses the constant of a system package named name anywhere inside it.
func mentionsSystemConstant(pass *analysis.Pass, expression ast.Expr, name string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if isIdentifier && identifier.Name == name && isSystemConstant(pass.TypesInfo.Uses[identifier]) {
			found = true
		}
		return !found
	})
	return found
}

func isComparison(operator token.Token) bool {
	switch operator {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	}
	return false
}

// constantOf is the object an operand names, when it is a bare or qualified name, or nil for anything computed.
func constantOf(pass *analysis.Pass, operand ast.Expr) types.Object {
	switch named := ast.Unparen(operand).(type) {
	case *ast.Ident:
		return pass.TypesInfo.Uses[named]
	case *ast.SelectorExpr:
		return pass.TypesInfo.Uses[named.Sel]
	}
	return nil
}

func isSystemConstant(object types.Object) bool {
	constant, isConstant := object.(*types.Const)
	return isConstant && constant.Pkg() != nil && isSystemPackage(constant.Pkg().Path())
}

// keyMirrors is every name declared from a call that reads an external key spelled exactly as the name is, as
// `DATABASE_URL := os.Getenv("DATABASE_URL")` reads the environment's. Such a name keeps the other system's spelling so
// a reader can find it on both sides of the boundary. Only the initializer of that one declaration counts, and the key
// is the argument's constant value as go/types computed it, so a named string constant counts and a key spelled any
// other way does not.
func keyMirrors(pass *analysis.Pass) map[types.Object]bool {
	mirrored := map[types.Object]bool{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch declaring := node.(type) {
			case *ast.AssignStmt:
				if declaring.Tok != token.DEFINE {
					return true
				}
				for index, target := range declaring.Lhs {
					identifier, isIdentifier := target.(*ast.Ident)
					if isIdentifier && readsKey(pass, initializerOf(declaring.Rhs, index, len(declaring.Lhs)),
						identifier.Name) {
						markDeclared(pass, identifier, mirrored)
					}
				}
			case *ast.ValueSpec:
				for index, identifier := range declaring.Names {
					if readsKey(pass, initializerOf(declaring.Values, index, len(declaring.Names)), identifier.Name) {
						markDeclared(pass, identifier, mirrored)
					}
				}
			}
			return true
		})
	}
	return mirrored
}

// initializerOf is the expression that gives the name at index of count its value: its own, or the one call that
// gives every name a value, `key, found := os.LookupEnv("KEY")`, or nil when there is none.
func initializerOf(values []ast.Expr, index int, count int) ast.Expr {
	if len(values) == count {
		return values[index]
	}
	if len(values) == 1 {
		return values[0]
	}
	return nil
}

// readsKey is whether expression is a call with a constant string argument exactly name. A conversion, string("KEY"),
// reads nothing from outside, so it mirrors no key.
func readsKey(pass *analysis.Pass, expression ast.Expr, name string) bool {
	call, isCall := ast.Unparen(expression).(*ast.CallExpr)
	if !isCall || pass.TypesInfo.Types[call.Fun].IsType() {
		return false
	}
	for _, argument := range call.Args {
		value := pass.TypesInfo.Types[argument].Value
		if value != nil && value.Kind() == constant.String && constant.StringVal(value) == name {
			return true
		}
	}
	return false
}

// markDeclared marks the object identifier declares, when it declares one: in `value, found := lookup()` a name
// already in scope is assigned, not declared, and its own declaration is judged where it stands.
//
// Mutates: mirrored gains identifier's object, for the caller to leave unreported
func markDeclared(pass *analysis.Pass, identifier *ast.Ident, mirrored map[types.Object]bool) {
	if object := pass.TypesInfo.Defs[identifier]; object != nil {
		mirrored[object] = true
	}
}

// directedNames are the package-level names a directive hands to something outside Go's naming: `//export name`, which
// cgo writes into a C header, and `//go:linkname localname`, which the linker binds to another package's symbol. The
// spelling there is whatever the other side needs. Directives are read from the comments' AST, never the source text.
func directedNames(pass *analysis.Pass) map[string]bool {
	directed := map[string]bool{}
	for _, file := range pass.Files {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				for _, directive := range []string{"//export ", "//go:linkname "} {
					arguments, isDirective := strings.CutPrefix(comment.Text, directive)
					if fields := strings.Fields(arguments); isDirective && len(fields) > 0 {
						directed[fields[0]] = true
					}
				}
			}
		}
	}
	return directed
}
