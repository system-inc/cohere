package nexus

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// correctnessRequireBlockingStandardStreamsAnalysis is one entry being read.
type correctnessRequireBlockingStandardStreamsAnalysis struct {
	ruleContext rule.Context
	index       *correctnessRequireBlockingStandardStreamsIndex

	// ordered is whether this file can reach the streams, so that where a block happens matters.
	ordered bool

	// functionMayBlock memoizes whether running a function may call `blockStandardStreams`: 1 yes,
	// 2 no, 3 being decided (a recursive call counts as no; the outer decision still sees every other
	// call in the cycle).
	functionMayBlock map[*ast.Node]uint8
}

// importsBlockAtLoad says whether a file this one imports, transitively, may block while it loads,
// before this file's own body runs. Only a file that can reach the streams can, and only through a
// call its top-level code makes.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) importsBlockAtLoad(entry tspath.PathKey) bool {
	seen := map[tspath.PathKey]bool{entry: true}
	queue := []tspath.PathKey{entry}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range analysis.index.edges[current] {
			if seen[next] {
				continue
			}
			seen[next] = true
			queue = append(queue, next)
			if !analysis.index.canBlock[next] {
				continue
			}
			file := analysis.index.files[next]
			// A compiled Nexus runs code this program cannot read.
			if file == nil || file.IsDeclarationFile || analysis.topLevelMayBlock(file.AsNode()) {
				return true
			}
		}
	}
	return false
}

// topLevelMayBlock says whether a module's load may call something that blocks: a call in its
// top-level statements, in a class's static or field initializers, or in a function it hands to a
// call there. Function and method bodies are reached only through a call, which the call's own
// verdict covers.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) topLevelMayBlock(sourceFile *ast.Node) bool {
	blocks := false
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if blocks {
			return true
		}
		if correctnessRequireBlockingStandardStreamsIsFunctionWithBody(node) {
			return false
		}
		if correctnessRequireBlockingStandardStreamsIsCallLike(node) && analysis.callMayBlock(node) {
			blocks = true
			return true
		}
		node.ForEachChild(visit)
		return blocks
	}
	sourceFile.ForEachChild(visit)
	return blocks
}

// callMayBlock says whether running a call may call `blockStandardStreams`: through what it calls,
// or through a function it hands on.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) callMayBlock(call *ast.Node) bool {
	var callee *ast.Node
	var arguments []*ast.Node
	switch call.Kind {
	case ast.KindCallExpression:
		if ast.IsImportCall(call) {
			return true
		}
		expression := call.AsCallExpression()
		callee = expression.Expression
		if expression.Arguments != nil {
			arguments = expression.Arguments.Nodes
		}
	case ast.KindNewExpression:
		expression := call.AsNewExpression()
		callee = expression.Expression
		if expression.Arguments != nil {
			arguments = expression.Arguments.Nodes
		}
	case ast.KindTaggedTemplateExpression:
		callee = call.AsTaggedTemplateExpression().Tag
	default:
		return false
	}
	for _, argument := range arguments {
		if analysis.handsOnMayBlock(argument) {
			return true
		}
	}
	callee = ast.SkipParentheses(callee)
	if callee.Kind == ast.KindFunctionExpression || callee.Kind == ast.KindArrowFunction {
		return analysis.mayBlockWhenRun(callee)
	}
	var name *ast.Node
	switch callee.Kind {
	case ast.KindIdentifier:
		name = callee
	case ast.KindPropertyAccessExpression:
		name = callee.AsPropertyAccessExpression().Name()
	default:
		// An element access, a call's result, or anything else the checker names no declaration for.
		return true
	}
	return analysis.symbolMayBlock(analysis.ruleContext.TypeChecker.GetSymbolAtLocation(name), name)
}

// handsOnMayBlock says whether an argument is a function, or names one, that may block when the
// callee runs it.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) handsOnMayBlock(argument *ast.Node) bool {
	unwrapped := ast.SkipParentheses(argument)
	switch unwrapped.Kind {
	case ast.KindFunctionExpression, ast.KindArrowFunction:
		return analysis.mayBlockWhenRun(unwrapped)
	case ast.KindIdentifier:
		symbol := analysis.ruleContext.TypeChecker.GetSymbolAtLocation(unwrapped)
		if symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
			symbol = analysis.ruleContext.TypeChecker.GetAliasedSymbol(symbol)
		}
		if symbol == nil || symbol.Flags&ast.SymbolFlagsFunction == 0 {
			return false
		}
		return analysis.symbolMayBlock(symbol, unwrapped)
	}
	return false
}

// symbolMayBlock says whether calling what a symbol names may block. Resolved through an import alias
// to its declaration, it may when any declaration may.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) symbolMayBlock(
	symbol *ast.Symbol,
	name *ast.Node,
) bool {
	resolved := symbol
	if resolved != nil && resolved.Flags&ast.SymbolFlagsAlias != 0 {
		resolved = analysis.ruleContext.TypeChecker.GetAliasedSymbol(resolved)
	}
	if resolved == nil || len(resolved.Declarations) == 0 {
		return true
	}
	for _, declaration := range resolved.Declarations {
		if analysis.declarationMayBlock(declaration, name) {
			return true
		}
	}
	return false
}

// declarationMayBlock says whether calling what one declaration declares may block.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) declarationMayBlock(
	declaration *ast.Node,
	name *ast.Node,
) bool {
	file := ast.GetSourceFileOfNode(declaration)
	if file == nil {
		return true
	}
	if !analysis.index.canBlock[file.PathKey()] {
		// `require` is declared by `@types/node` and loads whatever it is handed.
		return file.IsDeclarationFile && name.Text() == "require"
	}
	if file.IsDeclarationFile || correctnessRequireBlockingStandardStreamsIsNexusDeclaration(declaration) {
		return true
	}
	switch declaration.Kind {
	case ast.KindFunctionDeclaration, ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor,
		ast.KindSetAccessor:
		if declaration.Body() == nil {
			// An overload signature, whose implementation is another declaration of the same
			// symbol and is read there. A `declare`d function, or an abstract or interface-shaped
			// member, is implemented somewhere this file cannot see.
			return declaration.Kind != ast.KindFunctionDeclaration || declaration.Flags&ast.NodeFlagsAmbient != 0
		}
		return analysis.mayBlockWhenRun(declaration)
	case ast.KindVariableDeclaration:
		if initializer := declaration.Initializer(); initializer != nil {
			initializer = ast.SkipParentheses(initializer)
			if initializer.Kind == ast.KindFunctionExpression || initializer.Kind == ast.KindArrowFunction {
				return analysis.mayBlockWhenRun(initializer)
			}
		}
	case ast.KindClassDeclaration, ast.KindClassExpression:
		return analysis.mayBlockWhenRun(declaration)
	}
	// A value that holds a function nobody here can name.
	return true
}

// mayBlockWhenRun says whether running a function, a class's construction and initializers, or
// anything they hand on, may block: any call anywhere inside it that may.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) mayBlockWhenRun(node *ast.Node) bool {
	switch analysis.functionMayBlock[node] {
	case 1:
		return true
	case 2, 3:
		return false
	}
	analysis.functionMayBlock[node] = 3
	blocks := false
	var visit func(child *ast.Node) bool
	visit = func(child *ast.Node) bool {
		if blocks {
			return true
		}
		if correctnessRequireBlockingStandardStreamsIsCallLike(child) && analysis.callMayBlock(child) {
			blocks = true
			return true
		}
		child.ForEachChild(visit)
		return blocks
	}
	node.ForEachChild(visit)
	if blocks {
		analysis.functionMayBlock[node] = 1
	} else {
		analysis.functionMayBlock[node] = 2
	}
	return blocks
}

// localFunction is the function of this file an expression names: a function declaration, or a
// `const` bound to a function expression or an arrow. Anything else answers nil.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) localFunction(expression *ast.Node) *ast.Node {
	unwrapped := ast.SkipParentheses(expression)
	switch unwrapped.Kind {
	case ast.KindFunctionExpression, ast.KindArrowFunction:
		return unwrapped
	case ast.KindIdentifier:
	default:
		return nil
	}
	symbol := analysis.ruleContext.TypeChecker.GetSymbolAtLocation(unwrapped)
	if symbol == nil {
		return nil
	}
	for _, declaration := range symbol.Declarations {
		if ast.GetSourceFileOfNode(declaration) != analysis.ruleContext.SourceFile {
			continue
		}
		switch declaration.Kind {
		case ast.KindFunctionDeclaration:
			if declaration.Body() != nil {
				return declaration
			}
		case ast.KindVariableDeclaration:
			list := declaration.Parent
			initializer := declaration.Initializer()
			if list == nil ||
				list.Kind != ast.KindVariableDeclarationList ||
				list.Flags&ast.NodeFlagsConst == 0 ||
				initializer == nil {
				continue
			}
			initializer = ast.SkipParentheses(initializer)
			if initializer.Kind == ast.KindFunctionExpression || initializer.Kind == ast.KindArrowFunction {
				return initializer
			}
		}
	}
	return nil
}

// startedFunctions are the functions of this file a call starts: the ones it runs before it returns
// (its callee, when that is this file's function, unless a generator, whose body waits for its
// iterator), and the ones it hands on as arguments, which may run now or on any later turn.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) startedFunctions(
	call *ast.Node,
) (runs []*ast.Node, handsOn []*ast.Node) {
	var callee *ast.Node
	var arguments []*ast.Node
	switch call.Kind {
	case ast.KindCallExpression:
		expression := call.AsCallExpression()
		callee = expression.Expression
		if expression.Arguments != nil {
			arguments = expression.Arguments.Nodes
		}
	case ast.KindNewExpression:
		if expression := call.AsNewExpression(); expression.Arguments != nil {
			arguments = expression.Arguments.Nodes
		}
	case ast.KindTaggedTemplateExpression:
		callee = call.AsTaggedTemplateExpression().Tag
	}
	if callee != nil {
		if function := analysis.localFunction(callee); function != nil {
			if ast.GetFunctionFlags(function)&ast.FunctionFlagsGenerator != 0 {
				handsOn = append(handsOn, function)
			} else {
				runs = append(runs, function)
			}
		}
	}
	for _, argument := range arguments {
		if function := analysis.localFunction(argument); function != nil {
			handsOn = append(handsOn, function)
		}
	}
	return runs, handsOn
}

// argumentsMayBlock says whether evaluating a call's arguments may block, which they do before the
// callee's body runs.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) argumentsMayBlock(call *ast.Node) bool {
	var arguments []*ast.Node
	switch call.Kind {
	case ast.KindCallExpression:
		if expression := call.AsCallExpression(); expression.Arguments != nil {
			arguments = expression.Arguments.Nodes
		}
	case ast.KindNewExpression:
		if expression := call.AsNewExpression(); expression.Arguments != nil {
			arguments = expression.Arguments.Nodes
		}
	}
	blocks := false
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if blocks || correctnessRequireBlockingStandardStreamsIsFunctionWithBody(node) {
			return blocks
		}
		if node.Kind == ast.KindAwaitExpression ||
			(correctnessRequireBlockingStandardStreamsIsCallLike(node) && analysis.callMayBlock(node)) {
			blocks = true
			return true
		}
		node.ForEachChild(visit)
		return blocks
	}
	for _, argument := range arguments {
		visit(argument)
	}
	return blocks
}

// reachedExits walks the process from the module's top level, function by function, and returns every
// exit a path reaches having written and not having blocked.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) reachedExits(sourceFile *ast.Node) []*ast.Node {
	queued := map[*ast.Node]bool{sourceFile: true}
	queue := []*ast.Node{sourceFile}
	reported := map[*ast.Node]bool{}
	var exits []*ast.Node
	for len(queue) > 0 {
		root := queue[0]
		queue = queue[1:]
		found, started := analysis.analyzeRoot(root)
		for _, exit := range found {
			if !reported[exit] {
				reported[exit] = true
				exits = append(exits, exit)
			}
		}
		for _, function := range started {
			if !queued[function] {
				queued[function] = true
				queue = append(queue, function)
			}
		}
	}
	return exits
}

// analyzeRoot builds one root's graph and walks it from its entry, unblocked. It returns the exits a
// path reaches having written, and the functions of this file a path starts while still unblocked.
func (analysis *correctnessRequireBlockingStandardStreamsAnalysis) analyzeRoot(
	root *ast.Node,
) ([]*ast.Node, []*ast.Node) {
	ruleContext := analysis.ruleContext
	type event = correctnessRequireBlockingStandardStreamsEvent
	type builder = control_flow_graph.Builder[event]
	recorded := map[*ast.Node]bool{}
	unrecordable := false
	graph := control_flow_graph.Build(root, control_flow_graph.Hooks[event]{
		Expression: func(graphBuilder *builder, node *ast.Node) {
			callLike := correctnessRequireBlockingStandardStreamsIsCallLike(node)
			if !callLike && node.Kind != ast.KindAwaitExpression {
				return
			}
			if correctnessNoProcessExitAfterOutputInCatchBinding(node, root) {
				// It runs before the marker the write state relies on. An exit or a block there
				// cannot be placed, so the root is given up below.
				return
			}
			if node.Kind == ast.KindAwaitExpression {
				if analysis.ordered {
					recorded[node] = true
					graphBuilder.Emit(event{kind: correctnessRequireBlockingStandardStreamsBlock, node: node})
				}
				return
			}
			if node.Kind == ast.KindCallExpression && correctnessNoProcessExitAfterOutputIsExit(ruleContext, node) {
				recorded[node] = true
				graphBuilder.Emit(event{kind: correctnessRequireBlockingStandardStreamsExit, node: node})
				return
			}
			if node.Kind == ast.KindCallExpression && correctnessNoProcessExitAfterOutputIsWrite(ruleContext, node) &&
				!correctnessNoProcessExitAfterOutputHoldsExit(ruleContext, node) {
				graphBuilder.Emit(event{kind: correctnessRequireBlockingStandardStreamsWrite, node: node})
			}
			runs, handsOn := analysis.startedFunctions(node)
			if len(runs) > 0 || len(handsOn) > 0 {
				graphBuilder.Emit(event{
					kind: correctnessRequireBlockingStandardStreamsCall, node: node, runs: runs, handsOn: handsOn,
					argumentsMayBlock: analysis.ordered && len(runs) > 0 && analysis.argumentsMayBlock(node),
				})
			}
			if analysis.ordered && analysis.callMayBlock(node) {
				recorded[node] = true
				graphBuilder.Emit(event{kind: correctnessRequireBlockingStandardStreamsBlock, node: node})
			}
		},
		Statement: func(graphBuilder *builder, node *ast.Node) {
			if node.Kind == ast.KindBlock && node.Parent != nil && node.Parent.Kind == ast.KindCatchClause {
				graphBuilder.Emit(event{
					kind: correctnessRequireBlockingStandardStreamsCatchEntered, node: node.Parent.Parent,
				})
			}
			if !analysis.ordered {
				return
			}
			awaits := (node.Kind == ast.KindForOfStatement && node.AsForInOrOfStatement().AwaitModifier != nil) ||
				(node.Kind == ast.KindVariableStatement &&
					node.AsVariableStatement().DeclarationList.Flags&ast.NodeFlagsAwaitUsing == ast.NodeFlagsAwaitUsing)
			if awaits {
				graphBuilder.Emit(event{kind: correctnessRequireBlockingStandardStreamsBlock, node: node})
			}
		},
	})

	// Every exit of this root, and in an ordered file every call that may block and every `await`
	// that runs while it does, must have been placed in the graph. One the graph never placed (in a
	// catch binding, or a class initializer that runs with the statement declaring it) could let a
	// path pass it unseen, so the root is given up: a missed finding, never a false one.
	var check func(node *ast.Node) bool
	check = func(node *ast.Node) bool {
		if unrecordable {
			return true
		}
		if node.Kind == ast.KindCallExpression && control_flow_graph.RootOf(node) == root &&
			correctnessNoProcessExitAfterOutputIsExit(ruleContext, node) && !recorded[node] {
			unrecordable = true
			return true
		}
		if analysis.ordered && !recorded[node] && correctnessRequireBlockingStandardStreamsIsInRunOf(node, root) &&
			(node.Kind == ast.KindAwaitExpression ||
				(correctnessRequireBlockingStandardStreamsIsCallLike(node) && analysis.callMayBlock(node))) {
			unrecordable = true
			return true
		}
		node.ForEachChild(check)
		return unrecordable
	}
	root.ForEachChild(check)
	if unrecordable {
		return nil, nil
	}

	walk := correctnessNoProcessExitAfterOutputWalk{
		root:   root,
		chains: map[*ast.Node]string{},
		tries:  map[*ast.Node]int{},
	}
	reported := map[*ast.Node]bool{}
	started := map[*ast.Node]bool{}
	var startedOrder []*ast.Node
	start := func(function *ast.Node) {
		if !started[function] {
			started[function] = true
			startedOrder = append(startedOrder, function)
		}
	}
	type position struct {
		block *control_flow_graph.Block[event]
		state correctnessNoProcessExitAfterOutputState
	}
	visited := make([]map[string]bool, len(graph.Blocks))
	queue := []position{{block: graph.Blocks[0]}}
	visited[0] = map[string]bool{"": true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		state := current.state
		ended := false
		for _, recordedEvent := range current.block.Events {
			switch recordedEvent.kind {
			case correctnessRequireBlockingStandardStreamsWrite:
				state = state.with(walk.chainOf(recordedEvent.node))
			case correctnessRequireBlockingStandardStreamsCatchEntered:
				state = state.entering(walk.tryId(recordedEvent.node))
			case correctnessRequireBlockingStandardStreamsCall:
				if !recordedEvent.argumentsMayBlock {
					for _, function := range recordedEvent.runs {
						start(function)
					}
				}
				// In a file that blocks anywhere, a function handed on may run after the block.
				if !analysis.ordered {
					for _, function := range recordedEvent.handsOn {
						start(function)
					}
				}
			case correctnessRequireBlockingStandardStreamsExit:
				if len(state) > 0 {
					reported[recordedEvent.node] = true
				}
				ended = true
			case correctnessRequireBlockingStandardStreamsBlock:
				ended = true
			}
			if ended {
				break
			}
		}
		if ended {
			continue
		}
		key := state.key()
		for _, successor := range current.block.Successors {
			if successor == nil || !successor.Reachable {
				continue
			}
			index := successor.Index()
			if visited[index] == nil {
				visited[index] = map[string]bool{}
			}
			if visited[index][key] {
				continue
			}
			visited[index][key] = true
			queue = append(queue, position{block: successor, state: state})
		}
	}

	var exits []*ast.Node
	for exit := range reported {
		exits = append(exits, exit)
	}
	return exits, startedOrder
}
