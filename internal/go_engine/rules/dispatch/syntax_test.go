package dispatch

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// The syntax walk calls each listener for what it declares and nothing else, in order: for each file, every
// FileEnter, then each node of a listed kind in preorder, to every listener listing it in the order they were
// registered, with its ancestors and not itself, then every FileExit; and PackageExit once, after every file. The
// expected log is built by walking the very files the pass holds with ast.Inspect.
func TestTheSyntaxWalkCallsEachListenerForWhatItDeclaresInOrder(test *testing.T) {
	test.Parallel()
	heard := []string{}
	dispatcher := NewSyntax()
	listening := dispatcher.RuleAnalyzer(rule.Namespace+"listening", "listening", "records each call",
		&rule.Listener{
			FileEnter: func(_ *rule.Context, file *ast.File) {
				heard = append(heard, "enter "+file.Name.Name)
			},
			Nodes: []ast.Node{(*ast.IfStmt)(nil), (*ast.LabeledStmt)(nil)},
			Node: func(context *rule.Context, node ast.Node, stack []ast.Node) {
				heard = append(heard, nodeHeard(context.Fset, node, stack))
			},
			FileExit: func(_ *rule.Context, file *ast.File) {
				heard = append(heard, "exit "+file.Name.Name)
			},
			PackageExit: func(*rule.Context) {
				heard = append(heard, "package")
			},
		})
	alongside := dispatcher.RuleAnalyzer(rule.Namespace+"alongside", "alongside", "records each if",
		&rule.Listener{
			Nodes: []ast.Node{(*ast.IfStmt)(nil)},
			Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
				heard = append(heard, fmt.Sprintf("alongside if at line %d", context.Fset.Position(node.Pos()).Line))
			},
		})
	probe, files := filesProbe()
	for _, action := range analyzeFixture(test, "walked", listening, alongside, probe).Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}

	expected := []string{}
	ifs, labels := 0, 0
	for _, file := range files.files {
		expected = append(expected, "enter "+file.Name.Name)
		stack := []ast.Node{}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			switch node.(type) {
			case *ast.IfStmt:
				ifs++
				expected = append(expected, nodeHeard(files.fileSet, node, stack),
					fmt.Sprintf("alongside if at line %d", files.fileSet.Position(node.Pos()).Line))
			case *ast.LabeledStmt:
				labels++
				expected = append(expected, nodeHeard(files.fileSet, node, stack))
			}
			stack = append(stack, node)
			return true
		})
		expected = append(expected, "exit "+file.Name.Name)
	}
	expected = append(expected, "package")
	if len(files.files) != 2 || ifs < 4 || labels == 0 {
		test.Fatalf("the fixture read as %d files with %d ifs and %d labels, too few to prove the walk",
			len(files.files), ifs, labels)
	}
	if !slices.Equal(heard, expected) {
		test.Errorf("the listeners heard\n%s\nwhere they declared\n%s", strings.Join(heard, "\n"),
			strings.Join(expected, "\n"))
	}
}

// nodeHeard is one node as the walk test records it: its kind, its line, its parent's kind and how many ancestors it
// has, so a stack that held the node itself, or lost an ancestor, reads differently.
func nodeHeard(fileSet *token.FileSet, node ast.Node, stack []ast.Node) string {
	parent := "nothing"
	if len(stack) > 0 {
		parent = fmt.Sprintf("%T", stack[len(stack)-1])
	}
	return fmt.Sprintf("node %T at line %d under %s, %d ancestors", node, fileSet.Position(node.Pos()).Line,
		parent, len(stack))
}

// A syntax listener that panics is a crash under its own rule on the package, with none of its findings there, and
// it is called no more on that package, neither for later nodes nor at a file's exit nor the package's. A listener
// beside it still hears every node it lists and reports all its findings.
func TestAPanickingSyntaxListenerCrashesItsOwnRuleAlone(test *testing.T) {
	test.Parallel()
	dispatcher := NewSyntax()
	crashingCalls := 0
	exits := 0
	crashing := dispatcher.RuleAnalyzer(rule.Namespace+"crashing", "crashing", "reports, then panics on the second if",
		&rule.Listener{
			Nodes: []ast.Node{(*ast.IfStmt)(nil)},
			Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
				crashingCalls++
				context.Report(analysis.Diagnostic{Pos: node.Pos(), Message: "before the panic"})
				if crashingCalls == 2 {
					panic("planted panic")
				}
			},
			FileExit: func(*rule.Context, *ast.File) {
				exits++
			},
			PackageExit: func(*rule.Context) {
				exits++
			},
		})
	steady := dispatcher.RuleAnalyzer(rule.Namespace+"steady", "steady", "reports every if", &rule.Listener{
		Nodes: []ast.Node{(*ast.IfStmt)(nil)},
		Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
			context.Report(analysis.Diagnostic{Pos: node.Pos(), Message: "if"})
		},
	})
	probe, files := filesProbe()
	graph := analyzeFixture(test, "walked", crashing, steady, probe)
	ifs := 0
	for _, file := range files.files {
		ast.Inspect(file, func(node ast.Node) bool {
			if _, isIf := node.(*ast.IfStmt); isIf {
				ifs++
			}
			return true
		})
	}
	for _, action := range graph.Roots {
		switch action.Analyzer {
		case crashing:
			if action.Err == nil || !strings.HasPrefix(action.Err.Error(),
				"running analyzer crashing on walked: it panicked with planted panic, at ") {
				test.Errorf("the panicking rule ended with %v, where a crash naming the panic was expected", action.Err)
			}
			if len(action.Diagnostics) != 0 {
				test.Errorf("the panicking rule reported %d findings on the package it crashed on",
					len(action.Diagnostics))
			}
		case steady:
			if action.Err != nil || ifs < 4 || len(action.Diagnostics) != ifs {
				test.Errorf("the rule beside the panic ended with %v and reported %d findings, one for each of %d ifs "+
					"was expected", action.Err, len(action.Diagnostics), ifs)
			}
		}
	}
	if crashingCalls != 2 || exits != 0 {
		test.Errorf("the panicking listener was called %d times and exited %d times, where it should stop at its "+
			"panic, the second call", crashingCalls, exits)
	}
}

// probedFiles is what filesProbe leaves once the checker has run: the pass's files, and the file set their positions
// are read through.
type probedFiles struct {
	files   []*ast.File
	fileSet *token.FileSet
}

// filesProbe is an analyzer that reads the files the syntax dispatcher walks, in the same checker, and where it
// leaves them.
func filesProbe() (*analysis.Analyzer, *probedFiles) {
	probed := &probedFiles{}
	probe := &analysis.Analyzer{
		Name: "files_probe",
		Doc:  "reads the files the dispatcher walks",
		Run: func(pass *analysis.Pass) (any, error) {
			probed.files = pass.Files
			probed.fileSet = pass.Fset
			return nil, nil
		},
	}
	return probe, probed
}
