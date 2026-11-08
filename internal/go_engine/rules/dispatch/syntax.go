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

// Syntax calls the listeners registered on it over one inspector walk of each package's files: on entering a file,
// every FileEnter; each node, in preorder, to every listener whose Nodes list its kind, with its ancestors; on leaving
// the file, every FileExit; and once every file is walked, every PackageExit. Listeners are called in the order they
// were registered.
type Syntax struct {
	*listeners

	// walked is the kinds the inspector walks for: every kind a listener lists, and the file, which FileEnter and
	// FileExit hear. The inspector skips a subtree holding none of them.
	walked []ast.Node
}

// NewSyntax is a dispatcher over the syntax with no rule registered on it. Its analyzer runs despite errors in the
// package, as inspect and staticcheck's generated pass do, so a rule's analyzer on a package with errors is skipped by
// the checker for the errors, exactly as a rule that walked on its own was, never failed for its dispatcher.
func NewSyntax() *Syntax {
	dispatcher := &Syntax{walked: []ast.Node{(*ast.File)(nil)}}
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
		// Invariant: RuleAnalyzer hands a listener that is not syntax to Ir, so one here is a mistake in building the
		// registry, never in a package.
		panic("rule " + ruleName + " registers a listener that does not hear syntax on the syntax dispatcher, " +
			"which would never call it")
	}
	kinds := []reflect.Type{}
	for _, kind := range listener.Nodes {
		kindType := reflect.TypeOf(kind)
		kinds = append(kinds, kindType)
		if _, walked := dispatcher.byKind[kindType]; !walked && kindType != nil {
			dispatcher.walked = append(dispatcher.walked, kind)
		}
	}
	return dispatcher.ruleAnalyzer(ruleName, analyzerName, doc, listener, kinds)
}

// run walks the package's files once, calling every registered listener, each with its own context and its own
// recover, and returns each rule's outcome.
func (dispatcher *Syntax) run(pass *analysis.Pass) (any, error) {
	walker, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		return nil, fmt.Errorf("reading %s's result: it is %T, where an inspector was expected", inspect.Analyzer.Name,
			pass.ResultOf[inspect.Analyzer])
	}
	runs, startError := dispatcher.start(pass)
	if startError != nil {
		return nil, startError
	}
	walker.WithStack(dispatcher.walked, func(node ast.Node, entering bool, stack []ast.Node) bool {
		file, isFile := node.(*ast.File)
		if !entering {
			if isFile {
				for _, run := range runs {
					run.fileExit(file)
				}
			}
			return true
		}
		if isFile {
			for _, run := range runs {
				run.fileEnter(file)
			}
		}
		// The inspector's stack ends with the node itself, which a listener is handed on its own.
		ancestors := stack[:len(stack)-1]
		for _, index := range dispatcher.byKind[reflect.TypeOf(node)] {
			runs[index].node(node, ancestors)
		}
		return true
	})
	return finish(runs), nil
}
