package dispatch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/system-inc/cohere/internal/go_language/rule"
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
				heard = append(heard, nodeHeard(context.FileSet, node, stack))
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
				heard = append(heard, fmt.Sprintf("alongside if at line %d", context.FileSet.Position(node.Pos()).Line))
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

// A listener that appends to the stack it is handed changes no other listener's: the next listener at the same node,
// and every listener below it, the first child included, still hears its true ancestors. The walk keeps the node
// visited in the slot just past the stack it hands, as the parent of everything below, so a stack handed with room
// past its end would let the append overwrite that parent.
func TestASyntaxListenerAppendingToItsStackLeavesTheWalksAlone(test *testing.T) {
	test.Parallel()
	appends := 0
	planted := &ast.BadStmt{}
	dispatcher := NewSyntax()
	appending := dispatcher.RuleAnalyzer(rule.Namespace+"appending", "appending", "appends to every stack",
		&rule.Listener{
			Nodes: []ast.Node{(*ast.FuncDecl)(nil), (*ast.Ident)(nil)},
			Node: func(_ *rule.Context, _ ast.Node, stack []ast.Node) {
				grown := append(stack, planted, planted)
				appends += len(grown)
			},
		})
	heard := []string{}
	recording := dispatcher.RuleAnalyzer(rule.Namespace+"recording", "recording", "records each declaration",
		&rule.Listener{
			Nodes: []ast.Node{(*ast.FuncDecl)(nil), (*ast.Ident)(nil)},
			Node: func(context *rule.Context, node ast.Node, stack []ast.Node) {
				heard = append(heard, nodeHeard(context.FileSet, node, stack))
			},
		})
	probe, files := filesProbe()
	for _, action := range analyzeFixture(test, "walked", appending, recording, probe).Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}

	expected := []string{}
	for _, file := range files.files {
		stack := []ast.Node{}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			switch node.(type) {
			case *ast.FuncDecl, *ast.Ident:
				expected = append(expected, nodeHeard(files.fileSet, node, stack))
			}
			stack = append(stack, node)
			return true
		})
	}
	if appends == 0 || !slices.ContainsFunc(expected, func(line string) bool {
		return strings.Contains(line, "under *ast.FuncDecl")
	}) {
		test.Fatalf("the fixture has no identifier right under a function declaration, or nothing appended (%d), "+
			"so the walk's parents were never at risk", appends)
	}
	if !slices.Equal(heard, expected) {
		test.Errorf("after a listener appended to its stack, the next heard\n%s\nwhere the ancestors are\n%s",
			strings.Join(heard, "\n"), strings.Join(expected, "\n"))
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

// Every Node call on a node is made by the walk's one recover over that node's calls, never by a recover of its own
// per call (#fex6s0v): each listener's caller is the walk's callNodes. A listener that panics there is still its own
// rule's crash, spelled from its own frames and none of the walk's, and the listener after it on the same node is
// still called on that node and every later one.
func TestEveryNodeCallOnANodeSharesTheWalksRecover(test *testing.T) {
	test.Parallel()
	callers := map[string]int{}
	dispatcher := NewSyntax()
	panicking := dispatcher.RuleAnalyzer(rule.Namespace+"panicking", "panicking", "panics on the first if",
		&rule.Listener{
			Nodes: []ast.Node{(*ast.IfStmt)(nil)},
			Node: func(*rule.Context, ast.Node, []ast.Node) {
				programCounter, _, _, _ := runtime.Caller(1)
				callers[runtime.FuncForPC(programCounter).Name()]++
				panic("planted panic")
			},
		})
	ifs := 0
	after := dispatcher.RuleAnalyzer(rule.Namespace+"after", "after", "counts every if", &rule.Listener{
		Nodes: []ast.Node{(*ast.IfStmt)(nil)},
		Node: func(*rule.Context, ast.Node, []ast.Node) {
			programCounter, _, _, _ := runtime.Caller(1)
			callers[runtime.FuncForPC(programCounter).Name()]++
			ifs++
		},
	})
	graph := analyzeFixture(test, "walked", panicking, after)
	for _, action := range graph.Roots {
		if action.Analyzer == panicking && (action.Err == nil ||
			!strings.Contains(action.Err.Error(), "TestEveryNodeCallOnANodeSharesTheWalksRecover.func1") ||
			strings.Contains(action.Err.Error(), ".(*syntaxWalk).") ||
			strings.Contains(action.Err.Error(), ".(*listenerRun).")) {
			test.Errorf("the panicking rule ended with %v, where a crash at its own listener alone was expected",
				action.Err)
		}
	}
	walkCalls := "github.com/system-inc/cohere/internal/go_language/rules/dispatch.(*syntaxWalk).callNodes"
	if ifs < 4 || len(callers) != 1 || callers[walkCalls] != ifs+1 {
		test.Errorf("the listeners were called from %v over %d ifs, where every call from %s was expected", callers,
			ifs, walkCalls)
	}
}

// numberedSource is Go reaching every common kind of node, generics, a select and a labeled loop among them, for
// the walk's kind numbering to be read over.
const numberedSource = `package numbered

import "fmt"

type Pair[Key comparable, Value any] struct {
	key   Key
	value Value
}

func Lookup[Key comparable, Value any](pairs []Pair[Key, Value], wanted Key) (Value, bool) {
	for _, pair := range pairs {
		if pair.key == wanted {
			return pair.value, true
		}
	}
	var zero Value
	return zero, false
}

func Drain(values chan int, done <-chan struct{}) (total int) {
	defer fmt.Println("drained")
outer:
	for index := 0; index < 10; index++ {
		select {
		case value := <-values:
			total += value
		case <-done:
			break outer
		}
	}
	go func() { values <- -total }()
	switch typed := any(total).(type) {
	case int:
		total = typed * 2
	}
	lookup := map[string][]int{"a": {1, 2}}
	_ = lookup["a"][1:]
	_ = &Pair[int, string]{key: 1}
	return
}
`

// The walk finds a node's listeners by the number nodeKindOf gives its kind, never by hashing its reflect.Type, and
// finds the very listeners byKind holds for it (#fex6s0v): with byKind emptied once the walk is built, every listener
// still hears every node of each kind it lists, so the map is not what the walk reads. Every kind of the source is
// numbered, and no two kinds share a number.
func TestTheSyntaxWalkFindsListenersByTheNodesKindNumber(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "numbered.go", numberedSource, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing the source: %v", parseError)
	}
	numbers := map[int]reflect.Type{}
	expected := map[reflect.Type]int{}
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		number, kind := nodeKindOf(node), reflect.TypeOf(node)
		if number == 0 || (numbers[number] != nil && numbers[number] != kind) {
			test.Errorf("%v is numbered %d, which is no number or %v's as well", kind, number, numbers[number])
		}
		numbers[number] = kind
		expected[kind]++
		return true
	})
	pass := &analysis.Pass{Fset: fileSet, Files: []*ast.File{file}}
	started := &startedRuns{byKind: map[reflect.Type][]*listenerRun{}, notStarted: Result{}}
	heard := map[reflect.Type]int{}
	kinds := []ast.Node{}
	for kind := range expected {
		node, isNode := reflect.Zero(kind).Interface().(ast.Node)
		if !isNode {
			test.Fatalf("%v is no ast.Node", kind)
		}
		kinds = append(kinds, node)
		listener := &rule.Listener{Nodes: []ast.Node{node}, Node: func(_ *rule.Context, at ast.Node, _ []ast.Node) {
			heard[reflect.TypeOf(at)]++
		}}
		registered := &registeredRule{name: kind.String(), analyzer: &analysis.Analyzer{}, listener: listener,
			kinds: []reflect.Type{kind}}
		started.add(newListenerRun(registered, pass, func(string) bool { return false }, nil, false, nil, nil))
	}
	walk := newSyntaxWalk(started)
	clear(started.byKind)
	inspector.New([]*ast.File{file}).Root().Inspect(kinds, walk.visit)
	if len(expected) < 40 || !maps.Equal(heard, expected) {
		test.Errorf("the listeners heard %v, where the source holds %v, %d kinds", heard, expected, len(expected))
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
