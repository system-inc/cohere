package marker

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
)

// lazySources are a package of two files, each holding a call a marker above excuses, and a file the package does not
// hold.
var lazySources = map[string]string{
	"asked.go":     "package lazy\n\nfunc target() {}\n\nfunc asked() {\n\t// Checked: why.\n\ttarget()\n}\n",
	"unasked.go":   "package lazy\n\nfunc unasked() {\n\t// Checked: why.\n\ttarget()\n}\n",
	"elsewhere.go": "package lazy\n\nfunc elsewhere() {\n\t// Checked: why.\n\ttarget()\n}\n",
}

// The analyzer makes an index for every file of its package, and a file's index finds its comment groups only when it
// is first asked, so the file no rule asks of is never walked, and the one asked gives the answer it always gave. A
// file the package does not hold is indexed the same way.
func TestAnIndexNeverAskedIsNeverBuilt(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	files := map[string]*ast.File{}
	for name, source := range lazySources {
		file, parseError := parser.ParseFile(fileSet, name, source, parser.ParseComments)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", name, parseError)
		}
		files[name] = file
	}
	syntax := []*ast.File{files["asked.go"], files["unasked.go"]}
	pass := &analysis.Pass{
		Fset:     fileSet,
		Files:    syntax,
		ResultOf: map[*analysis.Analyzer]any{positions.Analyzer: positions.New(fileSet, syntax)},
	}
	result, runError := run(pass)
	if runError != nil {
		test.Fatal(runError)
	}
	indexes, isIndexes := result.(*Indexes)
	if !isIndexes {
		test.Fatalf("the analyzer gave %T, expected the indexes", result)
	}
	for name, file := range files {
		if index := indexes.ForFile(file); index.built.Load() {
			test.Fatalf("%s's index was built before it was asked anything", name)
		}
	}
	for _, name := range []string{"asked.go", "elsewhere.go"} {
		index := indexes.ForFile(files[name])
		if found := index.ExcuseFor(targetCall(test, files[name]), "Checked:"); found != WithReason {
			test.Errorf("%s: ExcuseFor is %d, expected WithReason", name, found)
		}
		if !index.built.Load() {
			test.Errorf("%s's index answered without being built", name)
		}
	}
	if indexes.ForFile(files["unasked.go"]).built.Load() {
		test.Error("unasked.go's index was built though nothing asked it")
	}
}

// TestMakingTheIndexesWalksNoFile holds the laziness by what it costs, not by the flag the build sets: the analyzer's
// run over a package of one real file, marker.go, allocates the indexes, their map, and each file's index and its
// unbuilt groups, 7 allocations, while an eager walk of that file adds its maps, their growth and its slice of lines,
// 34 more. A walk put back anywhere the run reaches, inside the closure or beside it, takes the run past the bound,
// which leaves room for a compiler that allocates a little differently and none for a walk.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would add
// to.
func TestMakingTheIndexesWalksNoFile(test *testing.T) {
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "marker.go", nil, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	syntax := []*ast.File{file}
	table := positions.New(fileSet, syntax)
	// A walk of a file with few comments costs little, so a bound held over one would pass for the wrong reason.
	if walked := testing.AllocsPerRun(1, func() { _ = findCommentLines(table, file) }); walked < 20 {
		test.Fatalf("walking marker.go allocated %.0f times, too few for the bound to tell a walk from none", walked)
	}
	pass := &analysis.Pass{Fset: fileSet, Files: syntax,
		ResultOf: map[*analysis.Analyzer]any{positions.Analyzer: table}}
	allocations := testing.AllocsPerRun(10, func() {
		if _, runError := run(pass); runError != nil {
			test.Fatal(runError)
		}
	})
	test.Logf("allocations making the indexes of a package of marker.go: %.0f", allocations)
	if allocations > 12 {
		test.Errorf("making the indexes of a package of marker.go allocated %.0f times, more than 12, so a file was "+
			"walked before anything asked its index", allocations)
	}
}

// Several rules, and one rule's shards of a large package, ask one file's index at once. Each asker gets what an index
// built up front answers, for every node of a real file, and the race detector, under which the gate runs this
// package, sees the groups found once and read by all.
func TestAnIndexAskedAtOnceAnswersAsOneBuiltUpFront(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "marker.go", nil, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	table := positions.New(fileSet, []*ast.File{file})
	reference := findCommentLines(table, file)
	nodes := []ast.Node{}
	ast.Inspect(file, func(node ast.Node) bool {
		if node != nil {
			nodes = append(nodes, node)
		}
		return true
	})
	index := NewIndex(table, file)
	const askers = 16
	mismatches := make([]int, askers)
	var waitGroup sync.WaitGroup
	for asker := range askers {
		waitGroup.Go(func() {
			for _, node := range nodes {
				above := reference.ownLinesEndingOn[table.Line(node.Pos())-1]
				trailing := []*ast.CommentGroup{}
				for _, group := range reference.startingOn[table.Line(node.End())] {
					if group.Pos() >= node.End() {
						trailing = append(trailing, group)
					}
				}
				if index.Above(node) != above || !slices.Equal(index.Trailing(node), trailing) {
					mismatches[asker]++
				}
			}
		})
	}
	waitGroup.Wait()
	for asker, count := range mismatches {
		if count > 0 {
			test.Errorf("asker %d got %d answers an index built up front does not give", asker, count)
		}
	}
	// Asking of a file with few nodes or comments would pass for the wrong reason.
	if len(nodes) < 500 || len(reference.startingOn) < 10 {
		test.Fatalf("asked of %d nodes and %d commented lines, want more than 500 and 10", len(nodes),
			len(reference.startingOn))
	}
}

// targetCall is file's one statement that calls target.
func targetCall(test *testing.T, file *ast.File) ast.Node {
	test.Helper()
	var statement ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		expression, isExpression := node.(*ast.ExprStmt)
		if !isExpression {
			return true
		}
		if call, isCall := expression.X.(*ast.CallExpr); isCall {
			if name, isIdentifier := call.Fun.(*ast.Ident); isIdentifier && name.Name == "target" {
				statement = expression
			}
		}
		return true
	})
	if statement == nil {
		test.Fatal("finding the call to target")
	}
	return statement
}
