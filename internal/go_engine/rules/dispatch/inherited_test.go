package dispatch

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// inheritedOnSecond is what the planted inheritance hands the inheriting rule on the walked fixture's second.go: a
// finding over its first if, from line 5 column 3 to line 7 column 4, with a fix replacing bytes 30 to 34, and one at
// its function's line with no end.
var inheritedOnSecond = []rule.InheritedFinding{
	{
		Line: 5, Column: 3, EndLine: 7, EndColumn: 4, MessageId: "inheritedIf", Message: "the host's if",
		Suggestions: []rule.InheritedSuggestion{{Message: "replace it", Edits: []rule.InheritedEdit{
			{Start: 30, End: 34, Text: "planted"},
		}}},
	},
	{Line: 3, Column: 1, MessageId: "inheritedFunction", Message: "the host's function"},
}

// A listener marked SyntaxOnly, on a package where the build's inheritance says second.go is inherited, never hears
// second.go: no FileEnter, no node, no FileExit there, while a listener beside it, not marked, hears both files. Its
// rule reports the host's findings on second.go as its own, after its own, at the lines, columns, ends and fix offsets
// the host's name, under the host's message identifiers. A syntax-only listener that reads TypesInfo, which its
// context drops, crashes as its own rule's crash, every other rule running on. Each holds whether the package is walked
// whole or split into shards.
func TestASyntaxOnlyListenerHearsNoInheritedFileAndReportsTheHostsFindings(test *testing.T) {
	test.Parallel()
	for _, shardBytes := range []int{defaultShardBytes, 1} {
		test.Run(fmt.Sprintf("shards of %d bytes", shardBytes), func(test *testing.T) {
			test.Parallel()
			dispatcher := NewSyntax()
			dispatcher.shardBytes = shardBytes
			// A split package's shards walk at once.
			var lock sync.Mutex
			heard := map[string][]string{}
			record := func(listener string, context *rule.Context, what string, node ast.Node) {
				lock.Lock()
				defer lock.Unlock()
				heard[listener] = append(heard[listener],
					what+" "+filepath.Base(context.FileSet.PositionFor(node.Pos(), false).Filename))
			}
			inheritingName := rule.Namespace + "inheriting"
			inheriting := dispatcher.RuleAnalyzer(inheritingName, "inheriting", "reports every if", &rule.Listener{
				FileLocal:  true,
				SyntaxOnly: true,
				FileEnter: func(context *rule.Context, file *ast.File) {
					record("inheriting", context, "enter", file)
				},
				Nodes: []ast.Node{(*ast.IfStmt)(nil)},
				Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
					record("inheriting", context, "if in", node)
					context.Report(analysis.Diagnostic{Pos: node.Pos(), Message: "its own if"})
				},
				FileExit: func(context *rule.Context, file *ast.File) { record("inheriting", context, "exit", file) },
			})
			beside := dispatcher.RuleAnalyzer(rule.Namespace+"beside", "beside", "hears every file", &rule.Listener{
				FileEnter: func(context *rule.Context, file *ast.File) { record("beside", context, "enter", file) },
			})
			typed := dispatcher.RuleAnalyzer(rule.Namespace+"typed", "typed", "reads type information",
				&rule.Listener{
					FileLocal:  true,
					SyntaxOnly: true,
					FileEnter: func(context *rule.Context, file *ast.File) {
						context.Reportf(file.Pos(), "defines %v", context.TypesInfo.Defs[file.Name])
					},
				})
			inheritance := plantedInheritance{ruleName: inheritingName,
				planted: map[string][]rule.InheritedFinding{"second.go": inheritedOnSecond}}
			graph, analyzeError := checker.Analyze([]*analysis.Analyzer{inheriting, beside, typed},
				loadFixture(test, "walked"), &checker.Options{Inheritance: inheritance, Inherited: rule.Inherited})
			if analyzeError != nil {
				test.Fatalf("analyzing the fixture: %v", analyzeError)
			}

			heardInheriting := slices.Sorted(slices.Values(heard["inheriting"]))
			expectedInheriting := []string{"enter first.go", "exit first.go", "if in first.go", "if in first.go"}
			if !slices.Equal(heardInheriting, expectedInheriting) {
				test.Errorf("the syntax-only listener heard %q, want only first.go's %q", heardInheriting,
					expectedInheriting)
			}
			heardBeside := slices.Sorted(slices.Values(heard["beside"]))
			if !slices.Equal(heardBeside, []string{"enter first.go", "enter second.go"}) {
				test.Errorf("the listener beside it heard %q, want both files", heardBeside)
			}
			for _, action := range graph.Roots {
				switch action.Analyzer {
				case inheriting:
					if action.Err != nil {
						test.Fatalf("the inheriting rule ended with %v", action.Err)
					}
					reported := []string{}
					for _, diagnostic := range action.Diagnostics {
						reported = append(reported, diagnosticPlaced(action, diagnostic))
					}
					expected := []string{
						"first.go:5:2 its own if",
						"first.go:6:3 its own if",
						"second.go:5:3-7:4 inheritedIf the host's if, fix replace it: 30-34 planted",
						"second.go:3:1 inheritedFunction the host's function",
					}
					if !slices.Equal(reported, expected) {
						test.Errorf("the inheriting rule reported\n%s\nwant\n%s", strings.Join(reported, "\n"),
							strings.Join(expected, "\n"))
					}
				case typed:
					if action.Err == nil || !strings.Contains(action.Err.Error(),
						"running analyzer typed on walked: it panicked with runtime error: invalid memory address") {
						test.Errorf("the rule reading type information ended with %v, want its own crash", action.Err)
					}
				case beside:
					if action.Err != nil {
						test.Errorf("the rule beside the crash ended with %v", action.Err)
					}
				}
			}
		})
	}
}

// diagnosticPlaced is diagnostic as the inheritance test reads it: its file's base name, its line and column, its end's
// when it has one, its category and message, and each fix's message and edits' offsets and text.
func diagnosticPlaced(action *checker.Action, diagnostic analysis.Diagnostic) string {
	fileSet := action.Package.Fset
	start := fileSet.PositionFor(diagnostic.Pos, false)
	placed := fmt.Sprintf("%s:%d:%d", filepath.Base(start.Filename), start.Line, start.Column)
	if diagnostic.End.IsValid() {
		end := fileSet.PositionFor(diagnostic.End, false)
		placed += fmt.Sprintf("-%d:%d", end.Line, end.Column)
	}
	if diagnostic.Category != "" {
		placed += " " + diagnostic.Category
	}
	placed += " " + diagnostic.Message
	for _, fix := range diagnostic.SuggestedFixes {
		placed += ", fix " + fix.Message + ":"
		for _, edit := range fix.TextEdits {
			placed += fmt.Sprintf(" %d-%d %s", fileSet.PositionFor(edit.Pos, false).Offset,
				fileSet.PositionFor(edit.End, false).Offset, edit.NewText)
		}
	}
	return placed
}

// A dispatched rule tells the build what it showed of the host's only once it reports it (#y6q1x2j): a syntax-only
// listener that finishes reports the two findings planted on second.go and the build is told two, while one that
// crashes on first.go reports none of them and the build is told nothing, so a crash never counts as inherited.
func TestADispatchedRuleCountsOnlyWhatItReported(test *testing.T) {
	test.Parallel()
	for _, crashing := range []bool{false, true} {
		dispatcher := NewSyntax()
		ruleName := rule.Namespace + "counted"
		counted := dispatcher.RuleAnalyzer(ruleName, "counted", "crashes on the files it hears, or hears them",
			&rule.Listener{
				FileLocal:  true,
				SyntaxOnly: true,
				FileEnter: func(*rule.Context, *ast.File) {
					if crashing {
						panic("the listener crashed")
					}
				},
			})
		took := &tookFindings{byFile: map[string]int{}}
		inheritance := plantedInheritance{ruleName: ruleName,
			planted: map[string][]rule.InheritedFinding{"second.go": inheritedOnSecond}, took: took}
		graph, analyzeError := checker.Analyze([]*analysis.Analyzer{counted}, loadFixture(test, "walked"),
			&checker.Options{Inheritance: inheritance, Inherited: rule.Inherited})
		if analyzeError != nil {
			test.Fatalf("analyzing the fixture: %v", analyzeError)
		}
		action := graph.Roots[0]
		if crashing {
			if action.Err == nil || len(action.Diagnostics) != 0 || len(took.byFile) != 0 {
				test.Errorf("the crashing rule ended with %v, reported %d findings and told the build %v, want its "+
					"crash, nothing reported and nothing told", action.Err, len(action.Diagnostics), took.byFile)
			}
			continue
		}
		if action.Err != nil || len(action.Diagnostics) != 2 || took.byFile["second.go"] != 2 || len(took.byFile) != 1 {
			test.Errorf("the rule ended with %v, reported %d findings and told the build %v, want the two planted "+
				"on second.go reported and told", action.Err, len(action.Diagnostics), took.byFile)
		}
	}
}
