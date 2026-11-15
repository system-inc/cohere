package correctness_require_invariant_reason

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
)

// quietSource has functions, methods, closures, defines, assignments, a range with `=`, a taken address and calls,
// but no panic and no Must call, so judging it reports nothing whatever it walks.
const quietSource = `package quiet

type box struct{ value int }

func (holder *box) set(value int) { holder.value = value }

func total(values []int) int {
	sum := 0
	var value int
	for _, value = range values {
		sum += value
	}
	spare := sum
	pointer := &spare
	add := func(count int) int {
		inner := count
		inner++
		return inner + *pointer
	}
	holder := &box{}
	holder.set(add(sum))
	return holder.value
}

func init() {
	_ = total([]int{1, 2})
}
`

// loudSource has a panic after a function literal holding another, a re-panic in a deferred literal, a recovered
// variable written again, and Must calls inside and outside initialization, so the order of its findings shows each
// function's panics reported in the order the functions begin, then the Must calls.
const loudSource = `package loud

func MustParse(text string) int { return len(text) }

var parsed = MustParse("initialization")

func outer() {
	defer func() {
		if recovered := recover(); recovered != nil {
			panic(recovered)
		}
	}()
	inner := func() {
		panic("inner")
	}
	inner()
	panic("outer")
}

func again() {
	caught := recover()
	caught = nil
	_ = MustParse("runtime")
	panic(caught)
}
`

// judgedFile is one source parsed and type-checked, with what judgeSyntax reads of it and the findings it reported.
type judgedFile struct {
	file     *ast.File
	context  *rule.Context
	comments *marker.Index
	reported []analysis.Diagnostic
	fileSet  *token.FileSet
}

// newJudgedFile parses and type-checks source, a file that imports nothing, for judgeSyntax.
func newJudgedFile(test *testing.T, source string) *judgedFile {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "case.go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing the case: %v", parseError)
	}
	information := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	files := []*ast.File{file}
	typesPackage, checkError := (&types.Config{}).Check(file.Name.Name, fileSet, files, information)
	if checkError != nil {
		test.Fatalf("type-checking the case: %v", checkError)
	}
	judged := &judgedFile{file: file, fileSet: fileSet}
	pass := &analysis.Pass{Pkg: typesPackage, TypesInfo: information, Fset: fileSet, Files: files}
	judged.context = rule.NewFileLocalContext(Name, pass, nil, nil, func(string) bool { return false },
		func(_ string, diagnostic analysis.Diagnostic) { judged.reported = append(judged.reported, diagnostic) })
	judged.comments = marker.NewIndex(positions.New(fileSet, files), file)
	return judged
}

// TestFindingsKeepTheirOrder holds the order judgeSyntax reports in: each function's panics in the order the
// functions begin, an outer function's before those of a literal it holds even when the literal comes first, then
// the Must calls, with a re-panic of a recovered value quiet and one of a recovered variable written again reported.
func TestFindingsKeepTheirOrder(test *testing.T) {
	test.Parallel()
	judged := newJudgedFile(test, loudSource)
	judgeSyntax(judged.context, judged.file, judged.comments)
	type finding struct {
		line     int
		category string
	}
	expected := []finding{
		{17, unexplainedPanicMessage.Id},
		{14, unexplainedPanicMessage.Id},
		{24, unexplainedPanicMessage.Id},
		{23, mustOutsideInitializationMessage.Id},
	}
	if len(judged.reported) != len(expected) {
		test.Fatalf("reported %d findings, expected %d: %v", len(judged.reported), len(expected), judged.reported)
	}
	for index, diagnostic := range judged.reported {
		line := judged.fileSet.Position(diagnostic.Pos).Line
		if line != expected[index].line || diagnostic.Category != expected[index].category {
			test.Errorf("finding %d is %s on line %d, expected %s on line %d", index, diagnostic.Category, line,
				expected[index].category, expected[index].line)
		}
	}
}

// TestAFileIsWalkedOnce holds that judging a file with no panic and no Must call walks it once and allocates only
// that walk's record and its stack of functions: each walk the rule once took over every function's body and over the
// file allocated its closures and maps, 33 allocations here, so a walk put back shows.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestAFileIsWalkedOnce(test *testing.T) {
	judged := newJudgedFile(test, quietSource)
	allocations := testing.AllocsPerRun(10, func() { judgeSyntax(judged.context, judged.file, judged.comments) })
	test.Logf("allocations judging a file with no panic and no Must call: %.0f", allocations)
	if len(judged.reported) != 0 {
		test.Fatalf("a file with no panic and no Must call reported %d findings", len(judged.reported))
	}
	if allocations > 2 {
		test.Errorf("judging a file with no panic and no Must call allocated %.0f times, more than one walk's two",
			allocations)
	}
}
