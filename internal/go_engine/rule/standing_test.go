package rule

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// standingSource declares, in order, a function with a doc comment, a variable whose initializer is a function
// literal, a method, and a function with no body, for the standing spans to be read over.
const standingSource = `package planted

// documented has a doc comment,
// of two lines.
func documented() int {
	return 1
}

var literal = func() int { return 2 }

type holder struct{}

func (holder) method() int {
	return 3
}

func external() int
`

// everyFunctionStands is a StandingBodies in which every function asked of stands and no rule is confined, which
// records each offset it was asked of, and each file name.
type everyFunctionStands struct {
	asked      []int
	askedFiles []string
}

// Stands records filename and offset and answers that the function there stands.
func (standing *everyFunctionStands) Stands(filename string, offset int) bool {
	standing.asked = append(standing.asked, offset)
	standing.askedFiles = append(standing.askedFiles, filename)
	return true
}

// Confines answers that no rule is confined.
func (*everyFunctionStands) Confines(string) bool { return false }

// Findings answers that the host holds no findings.
func (*everyFunctionStands) Findings(string, string) ([]InheritedFinding, bool) { return nil, false }

// Took records nothing.
func (*everyFunctionStands) Took(string, string, int, int) {
	// No rule is confined, so none takes anything.
}

// standingPass is a pass over standingSource, parsed as planted.go, holding standing under the Standing key, with its
// file and the file's function declarations in source order.
func standingPass(test *testing.T, standing StandingBodies) (*analysis.Pass, *ast.File, []*ast.FuncDecl) {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "planted.go", standingSource, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	functions := []*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
			functions = append(functions, function)
		}
	}
	pass := &analysis.Pass{Fset: fileSet, Files: []*ast.File{file}}
	if standing != nil {
		pass.ResultOf = map[*analysis.Analyzer]any{Standing: standing}
	}
	return pass, file, functions
}

// A standing function's span runs from its doc comment, or its `func` when it has none, to its end, with its body's
// opening brace between, and each function is asked of by the offset of its `func`. Only a function declaration with
// a body is asked of: a function literal in a variable's initializer and a declaration with no body never stand. A
// pass that holds no standing bodies has no spans.
func TestAStandingSpanRunsFromAFunctionsDocCommentToItsEnd(test *testing.T) {
	test.Parallel()
	standing := &everyFunctionStands{}
	pass, file, functions := standingPass(test, standing)
	documented, method := functions[0], functions[1]
	tokenFile := pass.Fset.File(file.FileStart)

	spans := StandingSpans(pass)
	expected := []StandingSpan{
		{Start: documented.Doc.Pos(), Body: documented.Body.Lbrace, End: documented.End()},
		{Start: method.Pos(), Body: method.Body.Lbrace, End: method.End()},
	}
	if len(spans) != 1 || !slices.Equal(spans[tokenFile], expected) {
		test.Errorf("the spans are %v, want the documented function's from its doc comment and the method's from its "+
			"func, %v", spans, expected)
	}
	asked := []int{tokenFile.Offset(documented.Pos()), tokenFile.Offset(method.Pos())}
	if !slices.Equal(standing.asked, asked) {
		test.Errorf("the standing bodies were asked of offsets %v, want each function with a body at its func, %v",
			standing.asked, asked)
	}

	bare, _, _ := standingPass(test, nil)
	if spans := StandingSpans(bare); spans != nil {
		test.Errorf("a pass with no standing bodies has the spans %v, want none", spans)
	}
}

// A file's spans are sorted by where each starts whatever order the file holds its declarations in, since the search
// of a file's spans and the syntax walk's pass over them read them in order: with a parsed file's declarations
// reversed, the spans are the same as before.
func TestAFilesStandingSpansAreSortedByWhereEachStarts(test *testing.T) {
	test.Parallel()
	pass, file, _ := standingPass(test, &everyFunctionStands{})
	tokenFile := pass.Fset.File(file.FileStart)
	inOrder := slices.Clone(StandingSpans(pass)[tokenFile])
	slices.Reverse(file.Decls)

	reversed := StandingSpans(pass)[tokenFile]
	if len(inOrder) != 2 || inOrder[0].Start >= inOrder[1].Start || !slices.Equal(reversed, inOrder) {
		test.Errorf("with the declarations reversed the spans are %v, want them sorted as before, %v", reversed,
			inOrder)
	}
}

// A function is asked of as the facts' reach keys a body (#e4rd5dw): by the file a reader sees, past any `//line`
// directive, at the offset of its `func` in the file as parsed. A function a directive places in another file is
// asked of by that file's name, made whole against the parsed file's directory, and one before the directive by the
// parsed file's own, each at its offset as parsed.
func TestAFunctionIsAskedOfByItsLineDirectivesFileAtItsParsedOffset(test *testing.T) {
	test.Parallel()
	source := "package planted\n\nfunc placed() int {\n\treturn 1\n}\n\n//line directed.rl:10\n" +
		"func directed() int {\n\treturn 2\n}\n"
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "/project/planted.go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	standing := &everyFunctionStands{}
	pass := &analysis.Pass{Fset: fileSet, Files: []*ast.File{file},
		ResultOf: map[*analysis.Analyzer]any{Standing: standing}}
	tokenFile := fileSet.File(file.FileStart)

	spans := StandingSpans(pass)
	placed, isPlaced := file.Decls[0].(*ast.FuncDecl)
	directed, isDirected := file.Decls[1].(*ast.FuncDecl)
	if !isPlaced || !isDirected {
		test.Fatalf("the source's declarations are %T and %T, want two functions", file.Decls[0], file.Decls[1])
	}
	files := []string{"/project/planted.go", "/project/directed.rl"}
	offsets := []int{tokenFile.Offset(placed.Pos()), tokenFile.Offset(directed.Pos())}
	if !slices.Equal(standing.askedFiles, files) || !slices.Equal(standing.asked, offsets) {
		test.Errorf("the standing bodies were asked of %v at %v, want %v at %v", standing.askedFiles,
			standing.asked, files, offsets)
	}
	if len(spans[tokenFile]) != 2 {
		test.Errorf("the parsed file has the spans %v, want both functions', under the file as parsed", spans)
	}
}

// A function whose body's brace is not between its start and its end stands for nothing, though the standing bodies
// would say it stands: a span read from it would leave a confined rule deaf to what is not the body's. The brace is
// knocked out of a parsed declaration, as a parse past an error can leave one.
func TestAFunctionWhoseBraceIsOutOfPlaceNeverStands(test *testing.T) {
	test.Parallel()
	standing := &everyFunctionStands{}
	pass, file, functions := standingPass(test, standing)
	documented, method := functions[0], functions[1]
	documented.Body.Lbrace = token.NoPos
	method.Body.Lbrace = method.End()

	if spans := StandingSpans(pass); len(spans[pass.Fset.File(file.FileStart)]) != 0 || len(standing.asked) != 0 {
		test.Errorf("with each brace out of place the spans are %v and the standing bodies were asked of %v, want no "+
			"span and nothing asked", spans, standing.asked)
	}
}

// A rule's own finding is dropped from a span's start, its doc comment's first byte, through its end, the byte past
// its closing brace, and kept on either side, the byte before the doc comment and the byte after the end. A finding
// with no position, and one in a file the rule is not confined on, are kept. A rule confined on no file keeps the
// very findings it was handed.
func TestARulesOwnFindingIsDroppedFromASpansStartThroughItsEnd(test *testing.T) {
	test.Parallel()
	pass, file, _ := standingPass(test, &everyFunctionStands{})
	tokenFile := pass.Fset.File(file.FileStart)
	spans := StandingSpans(pass)
	documented, method := spans[tokenFile][0], spans[tokenFile][1]
	other := pass.Fset.AddFile("other.go", -1, 100)

	findings := []analysis.Diagnostic{
		{Pos: documented.Start - 1, Message: "before the doc comment"},
		{Pos: documented.Start, Message: "at the doc comment"},
		{Pos: documented.Body, Message: "at the brace"},
		{Pos: documented.End, Message: "at the end"},
		{Pos: documented.End + 1, Message: "past the end"},
		{Pos: method.Start - 1, Message: "before the method"},
		{Pos: method.Start, Message: "at the method"},
		{Pos: method.End, Message: "at the method's end"},
		{Pos: method.End + 1, Message: "past the method's end"},
		{Pos: token.NoPos, Message: "nowhere"},
		{Pos: token.Pos(other.Base() + 10), Message: "in another file"},
	}
	kept := []string{}
	for _, finding := range OutsideStanding(pass.Fset, spans, findings) {
		kept = append(kept, finding.Message)
	}
	expected := []string{"before the doc comment", "past the end", "before the method", "past the method's end",
		"nowhere", "in another file"}
	if !slices.Equal(kept, expected) {
		test.Errorf("outside the standing spans the rule kept %q, want %q", kept, expected)
	}
	if unconfined := OutsideStanding(pass.Fset, nil, findings); len(unconfined) != len(findings) {
		test.Errorf("a rule confined on no file kept %d of its %d findings, want every one", len(unconfined),
			len(findings))
	}
}
