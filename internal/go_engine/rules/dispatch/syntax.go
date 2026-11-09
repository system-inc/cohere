package dispatch

import (
	"fmt"
	"go/ast"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Syntax calls the listeners registered on it that a package's pass starts (see start) over one inspector walk of the
// package's files: on entering a file, every FileEnter; each node, in preorder, to every listener whose Nodes list
// its kind, with its ancestors and, through its context, its cursor; on leaving the file, every FileExit; and once
// every file is walked, every PackageExit. Listeners are called in the order they were registered.
type Syntax struct {
	*listeners
}

// NewSyntax is a dispatcher over the syntax with no rule registered on it. Its analyzer runs despite errors in the
// package, as inspect and staticcheck's generated pass do, so a rule's analyzer on a package with errors is skipped by
// the checker for the errors, exactly as a rule that walked on its own was, never failed for its dispatcher.
func NewSyntax() *Syntax {
	dispatcher := &Syntax{}
	dispatcher.listeners = newListeners(
		"dispatch_syntax",
		"walks each package's syntax once, calling every listener registered on it, for the rule analyzers that "+
			"report what each found",
		[]*analysis.Analyzer{inspect.Analyzer},
		true,
		dispatcher.run,
	)
	return dispatcher
}

// RuleAnalyzer registers listener, which must hear the syntax walk, as the rule named ruleName, and returns the
// rule's own analyzer, named analyzerName and documented by doc, which requires this dispatcher.
func (dispatcher *Syntax) RuleAnalyzer(
	ruleName string,
	analyzerName string,
	doc string,
	listener *rule.Listener,
) *analysis.Analyzer {
	if !listener.IsSyntax() {
		// Invariant: RuleAnalyzer hands a listener that is not syntax to Ir or IntermediateRepresentation, so one here
		// is a mistake in building the registry, never in a package.
		panic("rule " + ruleName + " registers a listener that does not hear syntax on the syntax dispatcher, " +
			"which would never call it")
	}
	kinds := []reflect.Type{}
	for _, kind := range listener.Nodes {
		kinds = append(kinds, reflect.TypeOf(kind))
	}
	return dispatcher.ruleAnalyzer(ruleName, analyzerName, doc, listener, kinds)
}

// run walks the package's files once, calling every started listener, each with its own context and its own recover,
// and returns each started rule's outcome.
func (dispatcher *Syntax) run(pass *analysis.Pass) (any, error) {
	// Started first, which refuses a pass whose inspector failed, so that failure is named as one.
	started, startError := dispatcher.start(pass)
	if startError != nil {
		return nil, startError
	}
	walker, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		return nil, fmt.Errorf("reading %s's result: it is %T, where an inspector was expected", inspect.Analyzer.Name,
			pass.ResultOf[inspect.Analyzer])
	}
	// The inspector walks each file for every kind a started listener lists, and skips a subtree holding none of them,
	// so a kind that only an unstarted listener lists is never visited. It reads the kinds as a set, so a kind two
	// listeners list is listed twice here at no cost.
	kinds := []ast.Node{}
	for _, run := range started.runs {
		kinds = append(kinds, run.registered.listener.Nodes...)
	}
	walk := &syntaxWalk{started: started}
	visit := walk.visit
	// The root's children are the package's files, in the order the pass holds them.
	for fileCursor := range walker.Root().Children() {
		file, isFile := fileCursor.Node().(*ast.File)
		if !isFile {
			return nil, fmt.Errorf("walking %s: the inspector's root holds a %T, where only files were expected",
				pass.Pkg.Path(), fileCursor.Node())
		}
		for _, run := range started.runs {
			run.fileEnter(file)
		}
		// An empty list is every kind to the inspector, so listeners that list none, hearing only FileEnter and
		// FileExit, are no reason to walk the file's nodes.
		if len(kinds) > 0 {
			walk.spine = walk.spine[:0]
			walk.ancestors = walk.ancestors[:0]
			fileCursor.Inspect(kinds, visit)
		}
		for _, run := range started.runs {
			run.fileExit(file)
		}
	}
	return finish(started), nil
}
