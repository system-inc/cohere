// Package consistency_no_uppercase_initialism is the Go engine's rule that an initialism in a name is spelled as a
// word.
//
// The house writes an initialism the way it writes any other word in a name, capitalized at its first letter only:
// `Url`, `Id`, `Json`, `Http`. A run of capitals hides where one word ends and the next begins (`JSONEncoder`), and a
// name spelled one way here and the other way there (`userID`, `userId`) is two names to search for. Go's convention,
// and staticcheck's ST1003, ask for the opposite, so ST1003's initialism check is off and this rule speaks for the
// house instead.
//
// What is someone else's keeps its spelling: a name the code only uses, a package name, a method whose name an
// interface of another package fixes, a name a //go:linkname or cgo //export directive fixes, an Example function,
// whose name is go doc's reference to what it documents, and a screaming snake case name, which
// consistency-no-screaming-snake-case judges. An interface is seen when its package is in this package's import
// graph, so a method fixed by an interface of a package nothing here imports, such as a MarshalJSON in a package that
// never imports encoding/json, is judged as ours.
package consistency_no_uppercase_initialism

import (
	"go/ast"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-uppercase-initialism"

// listener reads the package's directed names as the syntax dispatcher's walk enters its first file, and whether each
// file is a test, and hears every identifier and type switch of the walk.
var listener = rule.Listener{
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.Ident)(nil), (*ast.TypeSwitchStmt)(nil)},
	Node:      judgeNode,
}

// analyzerName and doc are the rule's analyzer's name and documentation, the same on every analyzer built for it.
const (
	analyzerName = "consistency_no_uppercase_initialism"
	doc          = "Flags a declared name holding an initialism of two or more capitals, userID, ParseURL, " +
		"JSONEncoder or ID, and suggests it spelled as a word, userId, ParseUrl, JsonEncoder, Id, since the house " +
		"capitalizes only an initialism's first letter. Uses, package names, methods an interface of another " +
		"package fixes, //go:linkname and cgo //export names, Example functions and screaming snake case are left. " +
		"The finding offers the suggested name as a rename, which the engine applies at every reference. The " +
		"renameExports option lets that rename reach an exported name of an importable package, and never changes " +
		"a finding."
)

// NewAnalyzer builds the rule's analyzer on a syntax dispatcher of its own, with a flag set of its own, so a test can
// run it with renameExports set without setting the flag another test's analyzer reads. The shared dispatcher holds
// each rule's name once, so a second analyzer for the rule is registered on a dispatcher of its own.
func NewAnalyzer() *analysis.Analyzer {
	return newAnalyzer(dispatch.NewSyntax())
}

// newAnalyzer registers the rule's listener on dispatcher and gives the analyzer it returns its renameExports flag,
// which the engine sets from the rule's settings and the context's Option reads off this analyzer.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(Name, analyzerName, doc, &listener)
	rule.AddRenameExportsOption(analyzer)
	return analyzer
}

// Analyzer runs the rule over one package, reporting what the shared syntax dispatcher's walk found for it.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// Rule is this rule's entry in the Go engine's registry. It reads the types to tell a method an interface fixes.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var uppercaseInitialismMessage = policy.MessageOf(Name, "uppercaseInitialism")

// wordCasedRenameMessage is the rename every finding offers, to the name its message suggests.
var wordCasedRenameMessage = policy.MessageOf(Name, "wordCasedRename")

// packageState is what the rule reads once for its package and once for each file, held in its context's state.
type packageState struct {
	// directed is directedObjects of the package, read as the walk enters its first file, since a //go:linkname in
	// any file names a declaration in any other.
	directed map[types.Object]bool
	// interfaces is the package's index of foreign interfaces, built the first time a method's name could be a
	// finding.
	interfaces *foreignInterfaces
	// isTestFile is whether the file being walked is a _test.go file.
	isTestFile bool
}

// enterFile reads the package's directed names the first time, and whether file is a test file.
func enterFile(context *rule.Context, file *ast.File) {
	state := rule.State[packageState](context)
	if state.directed == nil {
		state.directed = directedObjects(context)
		state.interfaces = &foreignInterfaces{current: context.Package, reads: context.Reads()}
	}
	// The file's own name, not one a //line directive gives it, since only a _test.go file holds examples.
	state.isTestFile = strings.HasSuffix(context.FileSet.PositionFor(file.Package, false).Filename, "_test.go")
}

// judgeNode judges a declaring identifier, or the name a type switch binds.
func judgeNode(context *rule.Context, node ast.Node, _ []ast.Node) {
	switch declaring := node.(type) {
	case *ast.Ident:
		// Defs holds an identifier only where it declares, so a use of a name declared elsewhere, another package's
		// included, is never judged here. The spelling is read first, so the interface index is built only for a
		// package where a method's name could be a finding.
		object := context.TypesInfo.Defs[declaring]
		if object == nil || wordCased(declaring.Name) == declaring.Name {
			return
		}
		state := rule.State[packageState](context)
		if state.directed[object] {
			return
		}
		if isOurs(object, state.isTestFile, state.interfaces) {
			judge(context, declaring)
		}
	case *ast.TypeSwitchStmt:
		// go/types gives the name a type switch binds no object of its own, only one per clause, so it is judged here
		// by its spelling alone.
		assignment, isAssignment := declaring.Assign.(*ast.AssignStmt)
		if !isAssignment || len(assignment.Lhs) != 1 {
			return
		}
		if guard, isIdentifier := assignment.Lhs[0].(*ast.Ident); isIdentifier {
			judge(context, guard)
		}
	}
}

// isOurs is whether object's name is ours to spell. A package name is the imported package's, an embedded field is
// named by its type, which is judged where that type is declared, and a method may be named by an interface.
func isOurs(object types.Object, isTestFile bool, interfaces *foreignInterfaces) bool {
	switch declared := object.(type) {
	case *types.Var:
		return !declared.Embedded()
	case *types.Const, *types.TypeName, *types.Label:
		return true
	case *types.Func:
		if declared.Signature().Recv() != nil {
			return !interfaces.fix(declared)
		}
		// An example's name is go doc's grammar for what it documents, ExampleRecord_MarshalJSON, so it spells
		// whatever that is spelled, and that name is judged where it is declared.
		return !isTestFile || !isExampleName(declared.Name())
	}
	return false
}

// isExampleName is go test's reading of an example's name: Example, or Example followed by anything but a lower-case
// letter, so Examples and Exampled are helpers like any other function.
func isExampleName(name string) bool {
	rest, found := strings.CutPrefix(name, "Example")
	if !found {
		return false
	}
	if rest == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(first)
}

// directedObjects are the package-level declarations whose names a directive fixes for something outside Go's own
// naming: the local name of a //go:linkname, which the linker matches by name, and a function a cgo //export comment
// hands to C under its Go name.
func directedObjects(context *rule.Context) map[types.Object]bool {
	directed := map[types.Object]bool{}
	for _, file := range context.Files {
		// A //go:linkname may sit anywhere in any file of the package, and names its local declaration by name.
		for _, group := range file.Comments {
			for _, comment := range group.List {
				fields := strings.Fields(comment.Text)
				if len(fields) >= 2 && fields[0] == "//go:linkname" {
					if object := context.Package.Scope().Lookup(fields[1]); object != nil {
						directed[object] = true
					}
				}
			}
		}
		// cgo reads //export from the function's own doc comment, and only when it names that function.
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Recv != nil || function.Doc == nil {
				continue
			}
			for _, comment := range function.Doc.List {
				if comment.Text == "//export "+function.Name.Name {
					if object := context.TypesInfo.Defs[function.Name]; object != nil {
						directed[object] = true
					}
				}
			}
		}
	}
	return directed
}

// judge reports identifier when its name holds an initialism spelled in capitals, with the name it suggests.
func judge(context *rule.Context, identifier *ast.Ident) {
	// The blank identifier is Go's grammar for no name, and a screaming snake case name, MAX_RETRIES, is
	// consistency-no-screaming-snake-case's to report with its MixedCaps form, which this rule then reads.
	if identifier.Name == "_" || isScreamingSnakeCase(identifier.Name) {
		return
	}
	suggestedName := wordCased(identifier.Name)
	if suggestedName == identifier.Name {
		return
	}
	values := map[string]string{"name": identifier.Name, "suggestedName": suggestedName}
	context.Report(analysis.Diagnostic{
		Pos:      identifier.Pos(),
		End:      identifier.End(),
		Category: uppercaseInitialismMessage.Id,
		Message:  uppercaseInitialismMessage.Render(values),
		// The name the message suggests is the rename, so the fix and the finding can't name two spellings.
		SuggestedFixes: rule.RenameFixes(identifier, suggestedName, wordCasedRenameMessage.Render(values)),
	})
}

// isScreamingSnakeCase is whether name is capitals, digits and underscores, with at least one underscore.
func isScreamingSnakeCase(name string) bool {
	if !strings.Contains(name, "_") {
		return false
	}
	for _, character := range name {
		if unicode.IsLower(character) {
			return false
		}
	}
	return true
}

// wordCased is name with each run of capitals spelled as a word, its first capital kept and the rest lowered. When
// the run is followed by a lower-case letter, its last capital opens that next word and stays, so JSONEncoder becomes
// JsonEncoder and a run of two before a word, IPv4 or TKey, is one capital and a word and stays as it is. A run
// followed by a lone s that ends the word is a plural initialism, so IDs becomes Ids. A name with no initialism comes
// back unchanged, which is how the rule knows there is nothing to report.
func wordCased(name string) string {
	runes := []rune(name)
	start := 0
	for start < len(runes) {
		if !unicode.IsUpper(runes[start]) {
			start++
			continue
		}
		end := start
		for end < len(runes) && unicode.IsUpper(runes[end]) {
			end++
		}
		lowered := end
		if end < len(runes) && unicode.IsLower(runes[end]) && !isPluralEnding(runes, end) {
			lowered = end - 1
		}
		for index := start + 1; index < lowered; index++ {
			runes[index] = unicode.ToLower(runes[index])
		}
		start = end
	}
	return string(runes)
}

// isPluralEnding is whether the lower-case letter at index is an s that ends its word: the end of the name, or a rune
// that is no lower-case letter, as URLsToFetch's s is.
func isPluralEnding(runes []rune, index int) bool {
	return runes[index] == 's' && (index+1 == len(runes) || !unicode.IsLower(runes[index+1]))
}

// implementsInShape is whether implementer has every method of the generic interface candidate with as many
// parameters and results, and the same variadic ending. Some instantiation may then be what implementer implements,
// so its names are left to that interface: a finding there could be a rename that breaks the implementation, and
// silence on a near miss costs only a finding.
func implementsInShape(implementer types.Type, candidate *types.Interface) bool {
	for index := range candidate.NumMethods() {
		wanted := candidate.Method(index)
		object, _, _ := types.LookupFieldOrMethod(implementer, true, wanted.Pkg(), wanted.Name())
		found, isFunction := object.(*types.Func)
		if !isFunction {
			return false
		}
		if found.Signature().Params().Len() != wanted.Signature().Params().Len() ||
			found.Signature().Results().Len() != wanted.Signature().Results().Len() ||
			found.Signature().Variadic() != wanted.Signature().Variadic() {
			return false
		}
	}
	return true
}

// implementerOf is the type whose method set holds method: an interface type for a method an interface declares,
// and otherwise a pointer to the receiver's type, whose method set holds both value and pointer methods.
func implementerOf(method *types.Func) types.Type {
	receiverType := method.Signature().Recv().Type()
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	if types.IsInterface(receiverType) {
		return receiverType
	}
	return types.NewPointer(receiverType)
}
