package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// strictWalker carries the mode and the nesting state through one file.
//
// Upstream keeps two stacks, pushed and popped by enter and exit listeners: one of whether each
// enclosing function is strict, and one of whether we are inside a class body. There is no exit
// listener here, so the walk is explicit and the two stacks are ordinary recursion parameters.
type strictWalker struct {
	ruleContext rule.Context
	mode        StrictMode
}

// fileLevelMessage is what a directive at the top of the file means under the current mode.
func (walker strictWalker) fileLevelMessage() rule.Message {
	switch walker.mode {
	case strictModeModule:
		return messageStrictModule
	case strictModeImplied:
		return messageStrictImplied
	case StrictNever:
		return messageStrictNever
	case StrictFunction:
		// Under the function form a file-level directive is in the wrong place, and upstream says
		// so with the same id it uses to ask for the function form.
		return messageStrictFunction
	}
	return messageStrictGlobal
}

// walkStatements descends the tree, tracking strictness and class nesting.
//
// `insideAnyFunction` says whether any function encloses this node, strict or not, which is
// upstream's `scopes.length > 0` and is what suppresses the global-level arms. `parentIsStrict`
// says whether the nearest enclosing function runs strict, which is upstream's `scopes.at(-1)`
// and turns a directive into `unnecessary`. The two are genuinely independent: a non-strict
// enclosing function sets the first and not the second. `inClass`
// whether we are inside a class body, which is always strict. Both are only consulted under the
// function form; the other modes report every directive they find wherever it is.
func (walker strictWalker) walkStatements(
	statements []*ast.Node,
	insideAnyFunction bool,
	parentIsStrict bool,
	inClass bool,
) {
	for _, statement := range statements {
		walker.walkNode(statement, insideAnyFunction, parentIsStrict, inClass)
	}
}

// walkNode visits one node, entering a function or a class body when it meets one.
func (walker strictWalker) walkNode(node *ast.Node, insideAnyFunction bool, parentIsStrict bool, inClass bool) {
	if node == nil {
		return
	}

	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
		walker.enterFunction(node, insideAnyFunction, parentIsStrict, inClass)
		return
	case ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor,
		ast.KindSetAccessor:
		// A class member carries a function body without being one of the three function
		// kinds upstream lists, because ESTree hangs a FunctionExpression under the member
		// while our parser folds the two into one node. Omitting this arm left every
		// directive inside a method invisible, which upstream's own `class A { foo() {
		// "use strict"; } }` case caught.
		walker.enterFunction(node, insideAnyFunction, parentIsStrict, inClass)
		return
	case ast.KindClassDeclaration, ast.KindClassExpression:
		// A class body is always strict, and that fact travels down rather than the class itself
		// being checked.
		node.ForEachChild(func(child *ast.Node) bool {
			walker.walkNode(child, insideAnyFunction, parentIsStrict, true)
			return false
		})
		return
	}

	node.ForEachChild(func(child *ast.Node) bool {
		walker.walkNode(child, insideAnyFunction, parentIsStrict, inClass)
		return false
	})
}

// enterFunction applies the rule to one function and continues into its body.
func (walker strictWalker) enterFunction(node *ast.Node, insideAnyFunction bool, parentIsStrict bool, inClass bool) {
	body := strictFunctionBody(node)
	var directives []*ast.Node
	var bodyStatements []*ast.Node
	if body != nil && body.Kind == ast.KindBlock {
		bodyStatements = body.AsBlock().Statements.Nodes
		directives = strictDirectivesIn(walker.ruleContext, bodyStatements)
	}

	isStrict := len(directives) > 0

	if walker.mode == StrictFunction {
		walker.enterFunctionInFunctionMode(node, directives, insideAnyFunction, parentIsStrict, inClass)
	} else if len(directives) > 0 {
		// Every other mode reports the directives it finds, unless the parameter list makes them a
		// syntax error, in which case that is the more urgent thing to say.
		if strictHasSimpleParameterList(node) {
			walker.reportAll(directives, walker.fileLevelMessage(), strictShouldFix(walker.mode))
		} else {
			walker.ruleContext.ReportNode(directives[0], messageStrictNonSimpleParameterList)
			walker.reportAllExceptFirst(directives, messageStrictMultiple, true)
		}
	}

	// A function body is strict if it says so or if its parent was. A class body stays strict
	// throughout, which is why `inClass` travels down unchanged.
	walker.walkStatements(bodyStatements, true, parentIsStrict || isStrict, inClass)
	if body != nil && body.Kind != ast.KindBlock {
		// A concise arrow body is an expression rather than a block, and can still contain a
		// function of its own.
		walker.walkNode(body, true, parentIsStrict || isStrict, inClass)
	}
	// The parameter list can hold functions too, in a default value.
	walker.walkParameters(node, true, parentIsStrict || isStrict, inClass)
}

// enterFunctionInFunctionMode is the nesting-aware arm, and the only one that uses the stacks.
func (walker strictWalker) enterFunctionInFunctionMode(
	node *ast.Node,
	directives []*ast.Node,
	insideAnyFunction bool,
	parentIsStrict bool,
	inClass bool,
) {
	// Upstream's `scopes.length === 0 && classScopes.length === 0`. `scopes` is pushed on
	// entering any function, strict or not, so this asks whether a function encloses this one
	// rather than whether a strict one does. Deriving it from strictness instead reported both
	// the `function` and the `wrap` message on a nested function upstream leaves alone, caught
	// by `(function() { function foo(a = 0) { } }())`.
	isParentGlobal := !insideAnyFunction && !inClass

	if len(directives) > 0 {
		switch {
		case !strictHasSimpleParameterList(node):
			// A syntax error since ES2016, and the most urgent thing to say about this function.
			walker.ruleContext.ReportNode(directives[0], messageStrictNonSimpleParameterList)
		case parentIsStrict:
			walker.ruleContext.ReportNodeWithFixes(directives[0], messageStrictUnnecessary,
				strictRemoveDirective(walker.ruleContext, directives[0]))
		case inClass:
			walker.ruleContext.ReportNodeWithFixes(directives[0], messageStrictUnnecessaryInClasses,
				strictRemoveDirective(walker.ruleContext, directives[0]))
		}
		walker.reportAllExceptFirst(directives, messageStrictMultiple, true)
		return
	}

	// No directive. Only a function whose parent is neither strict nor a class needs one, because
	// everywhere else strictness is already inherited.
	//
	// Both arms below sit behind that one gate, and putting `wrap` outside it was a defect: a
	// function with a default parameter nested inside an already-strict function got asked to be
	// wrapped when upstream says nothing, because its strictness is inherited and there is
	// nothing to fix. Caught by upstream's `(function() { function foo(a = 0) { } }())`.
	if !isParentGlobal {
		return
	}
	if strictHasSimpleParameterList(node) {
		walker.ruleContext.ReportNode(node, messageStrictFunction)
		return
	}
	// The function cannot carry a directive at all, so upstream asks for a wrapper instead and
	// names the function so the reader can find it.
	walker.ruleContext.ReportNode(node, messageStrictWrapFor(strictDescribeFunction(walker.ruleContext, node)))
}

// walkParameters descends into parameter default values, which can hold functions.
func (walker strictWalker) walkParameters(node *ast.Node, insideAnyFunction bool, parentIsStrict bool, inClass bool) {
	for _, parameter := range strictParametersOf(node) {
		if initializer := parameter.AsParameterDeclaration().Initializer; initializer != nil {
			walker.walkNode(initializer, insideAnyFunction, parentIsStrict, inClass)
		}
	}
}

// reportAll reports every directive with one message.
func (walker strictWalker) reportAll(directives []*ast.Node, message rule.Message, fix bool) {
	walker.reportSlice(directives, 0, message, fix)
}

// reportAllExceptFirst reports every directive after the first, which is upstream's `multiple`.
func (walker strictWalker) reportAllExceptFirst(
	directives []*ast.Node,
	message rule.Message,
	fix bool,
) {
	walker.reportSlice(directives, 1, message, fix)
}

// reportSlice reports directives from an index onward.
func (walker strictWalker) reportSlice(
	directives []*ast.Node,
	from int,
	message rule.Message,
	fix bool,
) {
	for index := from; index < len(directives); index++ {
		if fix {
			walker.ruleContext.ReportNodeWithFixes(directives[index], message,
				strictRemoveDirective(walker.ruleContext, directives[index]))
			continue
		}
		walker.ruleContext.ReportNode(directives[index], message)
	}
}
