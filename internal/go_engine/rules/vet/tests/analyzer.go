// Package tests is vet's tests pass, the mistakes in a _test.go file that keep go test or go doc from reading a test,
// benchmark, fuzz test or example as its author meant: a Test, Benchmark or Fuzz function with go test's signature
// whose name has a lowercase letter after its prefix, or that takes type parameters; an Example that takes
// parameters, returns something or takes type parameters, whose name refers to an identifier, field or method that
// does not exist or ends in a malformed suffix, or whose output comment is not its last comment block; and the misuse
// of *testing.F in a Fuzz function (see fuzz.go).
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/tests/tests.go, with the helper it
// calls from x/tools' internal packages, typesinternal.IsTypeNamed, translated beside it, and it reports the
// original's messages word for word, at the same position and over the same range. Like the original it requires no
// other analyzer: it reads each test file's top-level declarations in order and walks a Fuzz function's body with
// ast.Inspect, whose pruning the fuzz checks rely on. It sits on the engine's syntax dispatcher, hearing each file as
// the walk the dispatcher makes once for every syntax rule enters it, and no node within. The registry runs the port
// in the original's place, and the original stays the oracle: the parity tests beside this file run both over the
// same packages.
//
// It differs from the original only where the original's finding is false, each recorded in the differences fixture
// with its reason, and all of them in a Fuzz function, where what go test checks when the fuzz test runs, through
// reflection, decides what is wrong:
//   - A call of a *testing.F method that the argument of Fuzz makes as Go evaluates it, before Fuzz runs anything,
//     `f.Fuzz(targetIn(f.TempDir()))`, is not a call the fuzz target makes, so it is not reported as one. A call
//     inside a function literal the argument does not call on the spot may run in the target, and is reported.
//   - An argument of Fuzz of interface type, or of a type parameter's, may hold a function, so it is not reported as
//     no function.
//   - A fuzz target whose first parameter is an alias of *testing.T takes a *testing.T, the same type to reflection,
//     so it is not reported as taking something else.
//   - A call of Add that spreads a slice, `f.Add(seed...)`, hands it values whose number and types the call does not
//     show, so it is not judged.
//   - A value of interface type, or of a type parameter's, handed to Add has the type of what it holds, which the call
//     does not show, so it is not counted as mismatched.
//   - A call of the method expression `(*testing.F).Add` takes the receiver as its first argument, which is not a
//     value Add records, so the values judged are the arguments after it.
package tests

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "tests"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for common mistaken usages of tests and examples\n\n" +
	"The tests checker walks Test, Benchmark, Fuzzing and Example functions checking\n" +
	"malformed names, wrong signatures and examples documenting non-existent\n" +
	"identifiers.\n\n" +
	"Please see the documentation for package testing in golang.org/pkg/testing\n" +
	"for the conventions that are enforced for Tests, Benchmarks, and Examples."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/tests"

// Listener is what the port hears of each package from the dispatcher: every file, as the walk enters it, in the order
// the original reads the package's files. It reads a file's top-level declarations and comments itself, so it lists
// no node kind.
var Listener = rule.Listener{
	FileLocal: true,
	FileEnter: checkFile,
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

// checkFile judges each test, benchmark, fuzz test and example a _test.go file declares at its top level.
func checkFile(context *rule.Context, file *ast.File) {
	// Only a _test.go file holds what go test runs. The name is the file's own, not one a //line directive gives it,
	// as the original reads it.
	if !strings.HasSuffix(context.FileSet.File(file.FileStart).Name(), "_test.go") {
		return
	}
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		// go test runs no method, whatever its name.
		if !isFunction || function.Recv != nil {
			continue
		}
		switch {
		case strings.HasPrefix(function.Name.Name, "Example"):
			checkExampleName(context, function)
			checkExampleOutput(context, function, file.Comments)
		case strings.HasPrefix(function.Name.Name, "Test"):
			checkTest(context, function, "Test")
		case strings.HasPrefix(function.Name.Name, "Benchmark"):
			checkTest(context, function, "Benchmark")
		case strings.HasPrefix(function.Name.Name, "Fuzz"):
			checkTest(context, function, "Fuzz")
			checkFuzz(context, function)
		}
	}
}

// checkTest judges a function whose name opens with prefix, Test, Benchmark or Fuzz, when it has go test's shape for
// one: no results and one parameter, a pointer to a type named T, B or F. Such a function with type parameters, or
// with a lowercase letter after its prefix, is one go test does not run. x/tools' checkTest.
func checkTest(context *rule.Context, function *ast.FuncDecl, prefix string) {
	// The parser gives every function declaration its parameter list, so Params is never nil here; the original's
	// guard is kept with the rest of its condition.
	if function.Type.Results != nil && len(function.Type.Results.List) > 0 ||
		function.Type.Params == nil ||
		len(function.Type.Params.List) != 1 ||
		len(function.Type.Params.List[0].Names) > 1 {
		return
	}
	if !isTestParameter(function.Type.Params.List[0].Type, prefix[:1]) {
		return
	}
	if typeParameters := function.Type.TypeParams; typeParameters != nil && len(typeParameters.List) > 0 {
		// The range runs from the opening bracket to the closing one, the closing bracket itself left out, as x/tools'
		// astutil.RangeOf(Opening, Closing) gives it.
		context.Report(analysis.Diagnostic{
			Pos: typeParameters.Opening,
			End: typeParameters.Closing,
			Message: fmt.Sprintf(
				"%s has type parameters: it will not be run by go test as a %sXXX function",
				function.Name.Name, prefix,
			),
		})
	}
	if !isTestSuffix(function.Name.Name[len(prefix):]) {
		context.ReportRangef(
			function.Name,
			"%s has malformed name: first letter after '%s' must not be lowercase", function.Name.Name, prefix,
		)
	}
}

// isTestParameter is whether parameterType is written as a pointer to a type named want, `*T` or `*testing.T` for
// want "T": syntax alone cannot tell testing's T from another, so the name decides, as it does for go test. x/tools'
// isTestParam.
func isTestParameter(parameterType ast.Expr, want string) bool {
	pointer, isPointer := parameterType.(*ast.StarExpr)
	if !isPointer {
		return false
	}
	switch pointed := pointer.X.(type) {
	case *ast.Ident:
		return pointed.Name == want
	case *ast.SelectorExpr:
		return pointed.Sel.Name == want
	}
	return false
}

// isTestSuffix is whether suffix, what follows Test, Benchmark or Fuzz in a name, lets go test run the function: it
// is empty, or does not open with a lowercase letter. x/tools' isTestSuffix.
func isTestSuffix(suffix string) bool {
	if suffix == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(suffix)
	return !unicode.IsLower(first)
}

// isExampleSuffix is whether text is a suffix go doc reads on an example's name: it opens with a lowercase letter.
// x/tools' isExampleSuffix.
func isExampleSuffix(text string) bool {
	first, size := utf8.DecodeRuneInString(text)
	return size > 0 && unicode.IsLower(first)
}

// checkExampleName judges an example's signature and what its name refers to: `Example`, `Example_suffix`,
// `ExampleName`, `ExampleName_suffix`, `ExampleName_Member` and `ExampleName_Member_suffix`, where Name is an
// identifier the package or one of its imports declares, Member is a field or method of it, and a suffix opens with a
// lowercase letter. x/tools' checkExampleName.
func checkExampleName(context *rule.Context, function *ast.FuncDecl) {
	if len(function.Type.Params.List) != 0 {
		context.Reportf(function.Pos(), "%s should be niladic", function.Name.Name)
	}
	if results := function.Type.Results; results != nil && len(results.List) != 0 {
		context.Reportf(function.Pos(), "%s should return nothing", function.Name.Name)
	}
	if typeParameters := function.Type.TypeParams; typeParameters != nil && len(typeParameters.List) > 0 {
		context.Reportf(function.Pos(), "%s should not have type params", function.Name.Name)
	}
	// The package's own example names nothing more.
	if function.Name.Name == "Example" {
		return
	}
	exampleName := strings.TrimPrefix(function.Name.Name, "Example")
	elements := strings.SplitN(exampleName, "_", 3)
	objects := lookupObjects(context, elements[0])
	if elements[0] != "" && len(objects) == 0 {
		// Nothing is named, so there is no member or suffix left to judge.
		context.Reportf(function.Pos(), "%s refers to unknown identifier: %s", function.Name.Name, elements[0])
		return
	}
	if len(elements) < 2 {
		return
	}
	if elements[0] == "" {
		// `Example_suffix`, the package's own example with a suffix.
		if residual := strings.TrimPrefix(exampleName, "_"); !isExampleSuffix(residual) {
			context.Reportf(function.Pos(), "%s has malformed example suffix: %s", function.Name.Name, residual)
		}
		return
	}
	// The second element is a member when it does not open with a lowercase letter, and a suffix when it does.
	if member := elements[1]; !isExampleSuffix(member) && !hasFieldOrMethod(objects, member) {
		context.Reportf(
			function.Pos(), "%s refers to unknown field or method: %s.%s", function.Name.Name, elements[0], member,
		)
	}
	if len(elements) == 3 && !isExampleSuffix(elements[2]) {
		context.Reportf(function.Pos(), "%s has malformed example suffix: %s", function.Name.Name, elements[2])
	}
}

// hasFieldOrMethod is whether any of objects, the declarations an example's name refers to, has a field or method
// named member, reached through embedding and through a pointer as an addressable value would reach it.
func hasFieldOrMethod(objects []types.Object, member string) bool {
	for _, object := range objects {
		if found, _, _ := types.LookupFieldOrMethod(object.Type(), true, object.Pkg(), member); found != nil {
			return true
		}
	}
	return false
}

// lookupObjects is the object package declares as name, or, when it declares none, every object one of its imports
// declares as name. Which package an example documents is hard to tell, so an import's declaration is enough: the
// example must reach what it names, so there is no false finding, only the rare missed one. x/tools' lookup. Each
// look into an import is recorded in the context's reads (#b2t9xmf).
func lookupObjects(context *rule.Context, name string) []types.Object {
	if object := context.Package.Scope().Lookup(name); object != nil {
		return []types.Object{object}
	}
	objects := []types.Object{}
	for _, imported := range context.Package.Imports() {
		if object := context.LookupIn(imported, name); object != nil {
			objects = append(objects, object)
		}
	}
	return objects
}

// outputPattern opens an example's output comment, go doc's own pattern in go/doc/example.go.
var outputPattern = regexp.MustCompile(`(?i)^[[:space:]]*(unordered )?output:`)

// exampleComment is one comment block inside an example, where it starts and whether it opens with an output
// comment. x/tools' commentMetadata.
type exampleComment struct {
	isOutput bool
	position token.Pos
}

// checkExampleOutput reports each output comment block of an example that is not the example's last comment block,
// since go doc reads output only from the last one. With more than one output block, each is reported as one too
// many. x/tools' checkExampleOutput.
func checkExampleOutput(context *rule.Context, function *ast.FuncDecl, fileComments []*ast.CommentGroup) {
	comments := []exampleComment{}
	outputs := 0
	// A file's comment blocks are in the order they appear, so the first to end past the function closes the search.
	for _, group := range fileComments {
		if group.Pos() < function.Pos() {
			continue
		}
		if group.End() > function.End() {
			break
		}
		isOutput := outputPattern.MatchString(group.Text())
		if isOutput {
			outputs++
		}
		comments = append(comments, exampleComment{isOutput: isOutput, position: group.Pos()})
	}
	message := "output comment block must be the last comment block"
	if outputs > 1 {
		message = "there can only be one output comment block per example"
	}
	for index, comment := range comments {
		if comment.isOutput && index != len(comments)-1 {
			context.Report(analysis.Diagnostic{Pos: comment.position, Message: message})
		}
	}
}
