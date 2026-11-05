// Package consistency_no_screaming_snake_case is the Go engine's rule that a name we declare is spelled in MixedCaps,
// with no underscore inside it.
//
// Go already says whether a name is exported by the case of its first letter, and a capital already marks where one
// word ends and the next begins, so `MAX_RETRIES` shouts a fact the casing carries and `max_retries` marks its word
// boundary twice. The Go form of `nexus/consistency-no-screaming-snake-case`, widened to every underscore because Go
// has no file-local casing to argue for one: Effective Go spells every name in MixedCaps. Where the underscore is
// someone else's grammar it is left alone: the blank identifier, the parts of a test function's name go test and go
// doc read, a name cgo or the linker sees, and a constant that mirrors one of syscall's or golang.org/x/sys's.
package consistency_no_screaming_snake_case

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-screaming-snake-case"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_screaming_snake_case",
	Doc: "Flags a name we declare that is in SCREAMING_SNAKE_CASE or has an underscore in it, suggesting its " +
		"MixedCaps form with exported names kept exported, because Go marks word boundaries and export with " +
		"capitals. Not judged: the blank identifier, an example's name, the test, benchmark or fuzz prefix of a " +
		"test's name, a name an //export or //go:linkname directive gives out, a constant assigned from or compared " +
		"against a constant of syscall or golang.org/x/sys, and a name whose MixedCaps form would not be an " +
		"identifier (type_ would be the keyword type).",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var screamingSnakeCaseMessage = policy.MessageOf(Name, "screamingSnakeCase")

var underscoreInNameMessage = policy.MessageOf(Name, "underscoreInName")

// testPrefixes open the names go test runs as tests. The prefix is go test's grammar and the rest of the name is ours,
// so TestParse_line is judged as Parse_line and suggests TestParseLine.
var testPrefixes = []string{"Test", "Benchmark", "Fuzz"}

// examplePrefix opens an example's name, which go doc reads whole: ExampleType_Method attaches the example to a method,
// and Example_suffix names one of several, so every underscore in it is go doc's grammar.
const examplePrefix = "Example"

func run(pass *analysis.Pass) (any, error) {
	mirrored := mirroredConstants(pass)
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
				if !isJudged(object) || mirrored[object] {
					return true
				}
				if object.Parent() == pass.Pkg.Scope() && directed[object.Name()] {
					return true
				}
				judge(pass, declaring, isExported(object), isTestFile && isTopLevelFunction(object))
			case *ast.TypeSwitchStmt:
				// go/types gives the name a type switch binds, `switch value := subject.(type)`, no object of its own,
				// only one per case clause, so it is judged here. It is local to the switch, so never exported.
				if identifier := typeSwitchGuard(declaring); identifier != nil {
					judge(pass, identifier, false, false)
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

// judge reports identifier when its name has an underscore in it, suggesting the MixedCaps form. exported decides
// whether that form opens with a capital, and testFunction is whether identifier names a top-level function in a
// _test.go file, whose test prefix or example name is go's grammar rather than ours.
func judge(pass *analysis.Pass, identifier *ast.Ident, exported bool, testFunction bool) {
	// The blank identifier is Go's grammar for no name, not a name.
	if identifier.Name == "_" || !strings.Contains(identifier.Name, "_") {
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
// package's constants, decided by types: its value is computed from a system constant, `oCloexec = unix.O_CLOEXEC`,
// or it is compared directly against one somewhere in the package. Such a name keeps the system's spelling so it
// reads as the thing it stands for. A constant in such a file with no such tie is judged like any other.
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
				if isOurs(left) && isSystemConstant(right) {
					mirrored[left] = true
				}
				if isOurs(right) && isSystemConstant(left) {
					mirrored[right] = true
				}
			}
			return true
		})
	}
	return mirrored
}

// markAssignedConstants marks each constant in declaration whose value mentions a system constant. A specification
// with no values repeats the last one that had them, as Go reads a constant group, so `SecondSignal` after
// `FirstSignal = unix.SignalBase + iota` is computed from unix's constant too.
func markAssignedConstants(pass *analysis.Pass, declaration *ast.GenDecl, mirrored map[types.Object]bool) {
	var values []ast.Expr
	for _, specification := range declaration.Specs {
		valueSpecification, isValueSpecification := specification.(*ast.ValueSpec)
		if !isValueSpecification {
			continue
		}
		if len(valueSpecification.Values) > 0 {
			values = valueSpecification.Values
		}
		for index, name := range valueSpecification.Names {
			if index < len(values) && mentionsSystemConstant(pass, values[index]) {
				mirrored[pass.TypesInfo.Defs[name]] = true
			}
		}
	}
}

// mentionsSystemConstant is whether expression uses a constant of a system package anywhere inside it.
func mentionsSystemConstant(pass *analysis.Pass, expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if isIdentifier && isSystemConstant(pass.TypesInfo.Uses[identifier]) {
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
