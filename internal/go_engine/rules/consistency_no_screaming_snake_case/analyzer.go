// Package consistency_no_screaming_snake_case is the Go engine's rule that a name we declare is spelled in MixedCaps,
// with no underscore inside it, and that a value local to a function opens lower-case.
//
// Go already says whether a name is exported by the case of its first letter, and a capital already marks where one
// word ends and the next begins, so `MAX_RETRIES` shouts a fact the casing carries and `max_retries` marks its word
// boundary twice. The Go form of `nexus/consistency-no-screaming-snake-case`, widened to every underscore because Go
// has no file-local casing to argue for one: Effective Go spells every name in MixedCaps. Where the underscore is
// someone else's grammar it is left alone: the blank identifier, a test function's name as go test and go doc read
// it (see testNameIsGo), a name cgo or the linker sees, a constant named exactly as one of syscall's or
// golang.org/x/sys's it mirrors, and a name read from an external key spelled exactly as the name is,
// `os.Getenv("DATABASE_URL")`. A local value's capital first letter would say exported, which a local never is, so it
// is reported too.
//
// Both exemptions hold only at the other system's exact spelling (P4 of the text rules' decisions): the shouting form
// earns its place by being greppable across the boundary, and a name spelled otherwise matches nothing on the other
// side. So `O_RDONLY = syscall.O_RDONLY` is exempt, and `O_RDONLY_MODE = syscall.O_RDONLY` is reported (us08), as is
// `O_RDONLY = syscall.O_WRONLY`, which spells a constant its value does not name.
package consistency_no_screaming_snake_case

import (
	"fmt"
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
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-screaming-snake-case"

// listener reads the package's system files and directed names as the syntax dispatcher's walk enters its first file,
// and whether each file is a test. It hears every declaring identifier and type switch, which it judges, and every
// declaration, comparison and assignment, which say whether a name mirrors a system constant or an external key. A
// comparison in any file can mirror a constant declared in any other, so findings are held until the whole package is
// walked, and reported then.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{positions.Analyzer},
	FileEnter: enterFile,
	Nodes: []ast.Node{
		(*ast.Ident)(nil),
		(*ast.TypeSwitchStmt)(nil),
		(*ast.GenDecl)(nil),
		(*ast.BinaryExpr)(nil),
		(*ast.AssignStmt)(nil),
		(*ast.ValueSpec)(nil),
	},
	Node:        hearNode,
	PackageExit: reportUnmirrored,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_screaming_snake_case", "Flags a name we declare that is "+
	"in SCREAMING_SNAKE_CASE or has an underscore in it, suggesting its MixedCaps form with exported names kept "+
	"exported, and a value local to a function that opens with a capital, suggesting its lowerCamel form, because "+
	"Go marks word boundaries and export with capitals. Not judged: the blank identifier, the name of a test, "+
	"benchmark, fuzz test or example in a _test.go file with go test's signature for it, whose underscores separate "+
	"UpperCamel words or digits (TestParser_Errors, TestTokenizer_310, ExampleClient_Send_withRetries), a name an "+
	"//export or //go:linkname directive gives out, a constant named exactly as the constant of syscall or "+
	"golang.org/x/sys it is assigned from or compared against, a name declared from a call whose constant string "+
	"argument is exactly that name (os.Getenv(\"DATABASE_URL\")), and a name whose suggested form would not be an "+
	"identifier (type_ would be the keyword type).", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var screamingSnakeCaseMessage = policy.MessageOf(Name, "screamingSnakeCase")

var underscoreInNameMessage = policy.MessageOf(Name, "underscoreInName")

var capitalizedLocalValueMessage = policy.MessageOf(Name, "capitalizedLocalValue")

// testWords open the names go test runs, each with the type of the one parameter go test hands it, a pointer to that
// type of package testing. An example takes none.
var testWords = []struct {
	word      string
	parameter string
}{{"Test", "T"}, {"Benchmark", "B"}, {"Fuzz", "F"}}

// examplePrefix opens an example's name, which go doc reads: ExampleType_Method attaches the example to a method, and
// a last segment in lower case, Example_suffix, names one of several.
const examplePrefix = "Example"

// packageState is what the rule gathers over its package, held in its context's state.
type packageState struct {
	// table is the package's line table, which reads a declaration's file without the FileSet's lock.
	table *positions.Table
	// systemFiles are the package's files importing a system package, by name as table reads them, read as the walk
	// enters its first file, since a comparison in one file can name a constant declared in another.
	systemFiles map[string]bool
	// directed is directedNames of the package, read as the walk enters its first file.
	directed map[string]bool
	// mirrored is every constant that mirrors a system constant (markAssignedConstants, markComparedConstants).
	mirrored map[types.Object]bool
	// keyed is every name declared from a call reading an external key spelled as it (markKeyMirrorsAssigned,
	// markKeyMirrorsDeclared).
	keyed map[types.Object]bool
	// held is each finding of the walk, in the order the walk made it, reported once the package is walked unless
	// its name turns out to mirror a constant or a key.
	held []heldFinding
	// isTestFile is whether the file being walked is a _test.go file.
	isTestFile bool
}

// heldFinding is a finding on a declared name, held until the package is walked.
type heldFinding struct {
	// object is the object the name declares, or nil for a type switch's binding, which mirrors nothing.
	object     types.Object
	diagnostic analysis.Diagnostic
}

// enterFile reads the line table, the system files and the directed names the first time, and whether file is a test
// file.
func enterFile(context *rule.Context, file *ast.File) {
	state := rule.State[packageState](context)
	if state.table == nil {
		table, isTable := context.ResultOf(positions.Analyzer).(*positions.Table)
		if !isTable {
			// Invariant: the listener requires positions, whose result is always a table, so anything else is a
			// mistake in building the rule, which the dispatcher's recover makes this rule's crash on the package.
			panic(fmt.Sprintf("reading the line table: positions gave %T", context.ResultOf(positions.Analyzer)))
		}
		state.table = table
		state.systemFiles = systemFilesOf(context, table)
		state.directed = directedNames(context)
		state.mirrored = map[types.Object]bool{}
		state.keyed = map[types.Object]bool{}
	}
	// The file's own name, not one a //line directive gives it, since only a _test.go file holds tests.
	state.isTestFile = strings.HasSuffix(context.FileSet.PositionFor(file.Package, false).Filename, "_test.go")
}

// hearNode judges a declaring identifier or a type switch's binding, holding its finding, and marks the names a
// declaration, a comparison or an assignment says mirror a system constant or an external key.
func hearNode(context *rule.Context, node ast.Node, _ []ast.Node) {
	state := rule.State[packageState](context)
	switch heard := node.(type) {
	case *ast.Ident:
		// Defs holds an identifier only where it declares, so a use is never judged, and a name another package
		// declared, `syscall.O_RDONLY` or `unix.Stat_t`, is never reported here.
		object := context.TypesInfo.Defs[heard]
		if !isJudged(object) {
			return
		}
		if object.Parent() == context.Package.Scope() && state.directed[object.Name()] {
			return
		}
		testWord := ""
		if state.isTestFile && isTopLevelFunction(object) {
			testWord = goTestWord(object)
		}
		diagnostic, found := judge(context, heard, isExported(object), testWord, isLocalValue(context, object))
		if found {
			state.held = append(state.held, heldFinding{object: object, diagnostic: diagnostic})
		}
	case *ast.TypeSwitchStmt:
		// go/types gives the name a type switch binds, `switch value := subject.(type)`, no object of its own, only
		// one per case clause, so it is judged here. It is a value local to the switch, so never exported.
		if identifier := typeSwitchGuard(heard); identifier != nil {
			if diagnostic, found := judge(context, identifier, false, "", true); found {
				state.held = append(state.held, heldFinding{diagnostic: diagnostic})
			}
		}
	case *ast.GenDecl:
		if heard.Tok == token.CONST && state.systemFiles[state.table.Position(heard.Pos()).Filename] {
			markAssignedConstants(context, heard, state.mirrored)
		}
	case *ast.BinaryExpr:
		markComparedConstants(context, state, heard)
	case *ast.AssignStmt:
		markKeyMirrorsAssigned(context, heard, state.keyed)
	case *ast.ValueSpec:
		markKeyMirrorsDeclared(context, heard, state.keyed)
	}
}

// reportUnmirrored reports every held finding whose name mirrors neither a system constant nor an external key, once
// every file has said which do.
func reportUnmirrored(context *rule.Context) {
	state := rule.State[packageState](context)
	for _, held := range state.held {
		if held.object != nil && (state.mirrored[held.object] || state.keyed[held.object]) {
			continue
		}
		context.Report(held.diagnostic)
	}
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
func isLocalValue(context *rule.Context, object types.Object) bool {
	switch declared := object.(type) {
	case *types.Var:
		if declared.IsField() {
			return false
		}
	case *types.Const:
	default:
		return false
	}
	return object.Parent() != nil && object.Parent() != context.Package.Scope()
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

// judge is the finding on identifier when its name has an underscore in it, suggesting the MixedCaps form, or when it
// names a local value and opens with a capital, suggesting the lowerCamel form. exported decides whether the
// MixedCaps form opens with a capital, testWord is the word go test runs identifier's function by, empty for any other
// name (see goTestWord), and localValue is whether it names a value declared inside a function. A test's name spelled
// in go test's grammar is clean; one that is not keeps its word, go test's, and has the rest judged as ours.
func judge(
	context *rule.Context, identifier *ast.Ident, exported bool, testWord string, localValue bool,
) (analysis.Diagnostic, bool) {
	// The blank identifier is Go's grammar for no name, not a name.
	if identifier.Name == "_" {
		return analysis.Diagnostic{}, false
	}
	if !strings.Contains(identifier.Name, "_") {
		if localValue {
			return judgeLocalOpening(identifier)
		}
		return analysis.Diagnostic{}, false
	}
	prefix := ""
	if testWord != "" {
		if testNameIsGo(context, identifier.Name, testWord) {
			return analysis.Diagnostic{}, false
		}
		prefix = testWord
	}
	body := strings.TrimPrefix(identifier.Name, prefix)
	// What follows a test prefix must open with a capital for go test to still run it, so it is spelled as exported.
	suggestion := prefix + mixedCaps(body, exported || prefix != "")
	// A name that only escapes a keyword, type_, or is nothing but underscores, or would open with a digit, has no
	// MixedCaps form that compiles, and a suggestion that cannot be typed is worse than none.
	if !token.IsIdentifier(suggestion) {
		return analysis.Diagnostic{}, false
	}
	message := underscoreInNameMessage
	if isScreamingSnakeCase(body) {
		message = screamingSnakeCaseMessage
	}
	return diagnosticOf(identifier, message, suggestion), true
}

// judgeLocalOpening is the finding on a local value whose name, with no underscore in it, opens with a capital:
// `MaxSize := 10` inside a function. Go reads a capital first letter as exported, which a local can never be, so the
// lowerCamel form is suggested, its opening lowered as an unexported MixedCaps name's is.
func judgeLocalOpening(identifier *ast.Ident) (analysis.Diagnostic, bool) {
	first, _ := utf8.DecodeRuneInString(identifier.Name)
	if !unicode.IsUpper(first) {
		return analysis.Diagnostic{}, false
	}
	suggestion := mixedCaps(identifier.Name, false)
	// Lowered, a name can become a keyword, Type to type, and a suggestion that cannot be typed is worse than none.
	if !token.IsIdentifier(suggestion) {
		return analysis.Diagnostic{}, false
	}
	return diagnosticOf(identifier, capitalizedLocalValueMessage, suggestion), true
}

// diagnosticOf is the finding on identifier, rendering message with its name and suggestion.
func diagnosticOf(identifier *ast.Ident, message policy.MessageHandle, suggestion string) analysis.Diagnostic {
	return analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{"name": identifier.Name, "suggestion": suggestion}),
	}
}

// goTestWord is the word go test runs a top-level function in a _test.go file by, Test, Benchmark, Fuzz or Example,
// when its name opens with that word as go test reads one (isGoTestName) and its signature is the one go test calls:
// one parameter of type *testing.T, *testing.B or *testing.F, or none for an example, no results and no type
// parameters. Any other function is no test, whatever its name, and is empty here.
func goTestWord(object types.Object) string {
	function, isFunction := object.(*types.Func)
	if !isFunction {
		return ""
	}
	signature := function.Signature()
	if signature.TypeParams().Len() > 0 || signature.Results().Len() > 0 {
		return ""
	}
	for _, each := range testWords {
		if isGoTestName(function.Name(), each.word) && signature.Params().Len() == 1 &&
			isTestingPointer(signature.Params().At(0).Type(), each.parameter) {
			return each.word
		}
	}
	if isGoTestName(function.Name(), examplePrefix) && signature.Params().Len() == 0 {
		return examplePrefix
	}
	return ""
}

// isTestingPointer is whether parameter is a pointer to the named type of package testing.
func isTestingPointer(parameter types.Type, name string) bool {
	pointer, isPointer := parameter.(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := pointer.Elem().(*types.Named)
	return isNamed && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "testing" && named.Obj().Name() == name
}

// testNameIsGo is whether a test's name, run by word, spells its underscores in go test's grammar, whose segments
// `go test -run` patterns and go doc read: after the word and whatever opens the name, each segment an UpperCamel word
// or digits, TestTokenizer_Events_310. An example's last segment may be its suffix, a lower-case word, and its method
// segment may be the exact name of a method or field of the type it names, `ExampleResource_URL`. A segment in
// capitals alone, `TestFOO_BAR`, one in lower case anywhere else, `Test_parse`, and an empty one are not go's grammar
// but ours.
func testNameIsGo(context *rule.Context, name string, word string) bool {
	segments := strings.Split(strings.TrimPrefix(name, word), "_")
	later := segments[1:]
	for index, segment := range later {
		switch {
		case segment == "":
			return false
		case strings.Trim(segment, "0123456789") == "":
		case isUpperCamelWord(segment):
		case word == examplePrefix && index == len(later)-1 && isLowerWord(segment):
		case word == examplePrefix && index == 0 && isMemberOf(context, segments[0], segment):
		default:
			return false
		}
	}
	return true
}

// isUpperCamelWord is whether segment opens with a capital, holds only letters and digits, and has a lower-case letter
// unless it is one letter: Events, V2 and X. A segment of two letters or more in capitals alone, `FOO`, shouts.
func isUpperCamelWord(segment string) bool {
	first, _ := utf8.DecodeRuneInString(segment)
	if !unicode.IsUpper(first) {
		return false
	}
	letters, lower := 0, false
	for _, character := range segment {
		switch {
		case unicode.IsLetter(character):
			letters++
			lower = lower || unicode.IsLower(character)
		case !unicode.IsDigit(character):
			return false
		}
	}
	return lower || letters == 1
}

// isLowerWord is whether segment opens with a lower-case letter and holds only letters and digits, as an example's
// suffix must for go doc to read it as one.
func isLowerWord(segment string) bool {
	first, _ := utf8.DecodeRuneInString(segment)
	if !unicode.IsLower(first) {
		return false
	}
	return strings.IndexFunc(segment, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}) < 0
}

// isMemberOf is whether member is the exact name of a method or field of the type named typeName in the package under
// test: this package, or for an external test package the package it tests. An example names it as go doc reads it,
// ExampleResource_URL, whatever its case. A look into the tested package is recorded in the context's reads, its answer
// a type whose fields and methods the answer's own rendering holds (#b2t9xmf).
func isMemberOf(context *rule.Context, typeName string, member string) bool {
	declaring := []*types.Package{context.Package}
	if tested, isExternal := strings.CutSuffix(context.Package.Path(), "_test"); isExternal {
		for _, imported := range context.Package.Imports() {
			if imported.Path() == tested {
				declaring = append(declaring, imported)
			}
		}
	}
	for _, underTest := range declaring {
		named, isTypeName := context.LookupIn(underTest, typeName).(*types.TypeName)
		if !isTypeName {
			continue
		}
		found, _, _ := types.LookupFieldOrMethod(named.Type(), true, named.Pkg(), member)
		if found != nil && found.Name() == member {
			return true
		}
	}
	return false
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

// systemFilesOf is the package's files that import a system package, by name, which table reads for every constant
// and declaration without the FileSet's lock.
//
// A constant declared in such a file mirrors one of that package's constants when it is decided by types and spelled
// exactly as it: its value is computed from the system constant of its own name, `O_CLOEXEC = unix.O_CLOEXEC`
// (markAssignedConstants), or it is compared directly against that constant somewhere in the package
// (markComparedConstants). Such a name keeps the system's spelling so it reads as the thing it stands for.
// `O_CLOEXEC_FLAGS` is not the system's spelling of anything, so it mirrors nothing, and a constant in such a file with
// no such tie is judged like any other.
func systemFilesOf(context *rule.Context, table *positions.Table) map[string]bool {
	systemFiles := map[string]bool{}
	for _, file := range context.Files {
		for _, specification := range file.Imports {
			if path, unquoteError := strconv.Unquote(specification.Path.Value); unquoteError == nil &&
				isSystemPackage(path) {
				systemFiles[table.Position(file.Package).Filename] = true
			}
		}
	}
	return systemFiles
}

// markComparedConstants marks a constant of ours, declared in a system file, that comparison compares directly
// against the system constant of its own name.
//
// Mutates: state gains in mirrored each constant comparison ties to a system constant, for reportUnmirrored to leave
// unreported
func markComparedConstants(context *rule.Context, state *packageState, comparison *ast.BinaryExpr) {
	if len(state.systemFiles) == 0 || !isComparison(comparison.Op) {
		return
	}
	isOurs := func(object types.Object) bool {
		constant, isConstant := object.(*types.Const)
		return isConstant && constant.Pkg() == context.Package &&
			state.systemFiles[state.table.Position(constant.Pos()).Filename]
	}
	left, right := constantOf(context, comparison.X), constantOf(context, comparison.Y)
	// Both sides are constants here, so neither is nil when the names are read.
	if isOurs(left) && isSystemConstant(right) && left.Name() == right.Name() {
		state.mirrored[left] = true
	}
	if isOurs(right) && isSystemConstant(left) && right.Name() == left.Name() {
		state.mirrored[right] = true
	}
}

// markAssignedConstants marks each constant in declaration whose own value mentions the system constant of its name,
// so `O_RDONLY = uint32(syscall.O_RDONLY)` keeps the system's spelling through a conversion. A specification with no
// values repeats the last one that had them, as Go reads a constant group, but that value is spelled for the name it
// was written beside, never the next one, so only a specification's own values are read.
//
// Mutates: mirrored gains each constant whose value mentions the system constant of its name, for the caller to leave
// unreported
func markAssignedConstants(context *rule.Context, declaration *ast.GenDecl, mirrored map[types.Object]bool) {
	for _, specification := range declaration.Specs {
		valueSpecification, isValueSpecification := specification.(*ast.ValueSpec)
		if !isValueSpecification {
			continue
		}
		for index, name := range valueSpecification.Names {
			if index < len(valueSpecification.Values) &&
				mentionsSystemConstant(context, valueSpecification.Values[index], name.Name) {
				mirrored[context.TypesInfo.Defs[name]] = true
			}
		}
	}
}

// mentionsSystemConstant is whether expression uses the constant of a system package named name anywhere inside it.
func mentionsSystemConstant(context *rule.Context, expression ast.Expr, name string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if isIdentifier && identifier.Name == name && isSystemConstant(context.TypesInfo.Uses[identifier]) {
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
func constantOf(context *rule.Context, operand ast.Expr) types.Object {
	switch named := ast.Unparen(operand).(type) {
	case *ast.Ident:
		return context.TypesInfo.Uses[named]
	case *ast.SelectorExpr:
		return context.TypesInfo.Uses[named.Sel]
	}
	return nil
}

func isSystemConstant(object types.Object) bool {
	constant, isConstant := object.(*types.Const)
	return isConstant && constant.Pkg() != nil && isSystemPackage(constant.Pkg().Path())
}

// markKeyMirrorsAssigned marks every name assignment declares from a call that reads an external key spelled exactly
// as the name is, as `DATABASE_URL := os.Getenv("DATABASE_URL")` reads the environment's. Such a name keeps the other
// system's spelling so a reader can find it on both sides of the boundary. Only the initializer of that one
// declaration counts, and the key is the argument's constant value as go/types computed it, so a named string
// constant counts and a key spelled any other way does not.
//
// Mutates: mirrored gains each name assignment declares from its own key, for the caller to leave unreported
func markKeyMirrorsAssigned(context *rule.Context, assignment *ast.AssignStmt, mirrored map[types.Object]bool) {
	if assignment.Tok != token.DEFINE {
		return
	}
	for index, target := range assignment.Lhs {
		identifier, isIdentifier := target.(*ast.Ident)
		if isIdentifier && readsKey(context, initializerOf(assignment.Rhs, index, len(assignment.Lhs)),
			identifier.Name) {
			markDeclared(context, identifier, mirrored)
		}
	}
}

// markKeyMirrorsDeclared marks every name a var or const specification declares from its own key, as
// markKeyMirrorsAssigned does for an assignment.
//
// Mutates: mirrored gains each name specification declares from its own key, for the caller to leave unreported
func markKeyMirrorsDeclared(context *rule.Context, specification *ast.ValueSpec, mirrored map[types.Object]bool) {
	for index, identifier := range specification.Names {
		if readsKey(context, initializerOf(specification.Values, index, len(specification.Names)), identifier.Name) {
			markDeclared(context, identifier, mirrored)
		}
	}
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
func readsKey(context *rule.Context, expression ast.Expr, name string) bool {
	call, isCall := ast.Unparen(expression).(*ast.CallExpr)
	if !isCall || context.TypesInfo.Types[call.Fun].IsType() {
		return false
	}
	for _, argument := range call.Args {
		value := context.TypesInfo.Types[argument].Value
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
func markDeclared(context *rule.Context, identifier *ast.Ident, mirrored map[types.Object]bool) {
	if object := context.TypesInfo.Defs[identifier]; object != nil {
		mirrored[object] = true
	}
}

// directedNames are the package-level names a directive hands to something outside Go's naming: `//export name`, which
// cgo writes into a C header, and `//go:linkname localname`, which the linker binds to another package's symbol. The
// spelling there is whatever the other side needs. Directives are read from the comments' AST, never the source text.
func directedNames(context *rule.Context) map[string]bool {
	directed := map[string]bool{}
	for _, file := range context.Files {
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
