package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/ecmascript/reference"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// independentAwaitLoopAnalysis holds one loop and what the walks over its body found.
type independentAwaitLoopAnalysis struct {
	ruleContext rule.Context
	loop        *ast.Node
	body        *ast.Node

	// collection is the expression the loop iterates: the for-of iterable, or `xs` in `xs.length`.
	collection *ast.Node

	// indexSymbol is the indexed form's counter, nil for a for-of.
	indexSymbol *ast.Symbol

	// taintedSymbols are bindings declared in the body whose value derives from an await.
	taintedSymbols map[*ast.Symbol]bool

	// orderedBodies memoizes which called function bodies make an ordered call.
	orderedBodies map[*ast.Node]bool
}

// isIndependent runs the four judgments, cheapest first.
func (analysis *independentAwaitLoopAnalysis) isIndependent() bool {
	if !analysis.holdsItsOwnAwait() {
		return false
	}
	if analysis.hasOrderedSideEffect() {
		return false
	}
	if analysis.carriesState() {
		return false
	}
	analysis.computeTaint()
	return !analysis.exitsOnAwaitedValue()
}

// holdsItsOwnAwait answers whether an await in the body runs once per iteration of this loop rather
// than of a loop nested inside it.
func (analysis *independentAwaitLoopAnalysis) holdsItsOwnAwait() bool {
	found := false
	var visit func(node *ast.Node, owned bool)
	visit = func(node *ast.Node, owned bool) {
		if node == nil || found || independentAwaitLoopIsBoundary(node) {
			return
		}
		switch node.Kind {
		case ast.KindAwaitExpression:
			if owned && !analysis.awaitsTheItemItself(node.AsAwaitExpression().Expression) {
				found = true
				return
			}
		case ast.KindVariableDeclarationList:
			// `await using` waits at scope exit, once per iteration of the loop it sits in.
			if owned && ast.IsVarAwaitUsing(node) {
				found = true
				return
			}
		case ast.KindForStatement:
			// The initializer runs once per outer iteration; everything else belongs to the inner loop.
			statement := node.AsForStatement()
			visit(statement.Initializer, owned)
			visit(statement.Condition, false)
			visit(statement.Incrementor, false)
			visit(statement.Statement, false)
			return
		case ast.KindForOfStatement, ast.KindForInStatement:
			statement := node.AsForInOrOfStatement()
			// A nested `for await` waits for its first item inside this iteration.
			if owned && node.Kind == ast.KindForOfStatement && statement.AwaitModifier != nil {
				found = true
				return
			}
			visit(statement.Expression, owned)
			visit(statement.Initializer, false)
			visit(statement.Statement, false)
			return
		case ast.KindWhileStatement:
			statement := node.AsWhileStatement()
			visit(statement.Expression, false)
			visit(statement.Statement, false)
			return
		case ast.KindDoStatement:
			statement := node.AsDoStatement()
			visit(statement.Statement, false)
			visit(statement.Expression, false)
			return
		}
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child, owned)
			return found
		})
	}
	visit(analysis.body, true)
	return found
}

// awaitsTheItemItself answers whether an await's operand is the collection's own element: the
// for-of binding, or `xs[i]` in the indexed form. Such a promise was started before the loop
// began, so awaiting it in turn serializes no work, and `Promise.all` would change nothing but
// the spelling. Measured on ahra: four loops in PromiseGroup.test.ts collect already-running
// promises this way and were the largest false-positive class before this test existed.
func (analysis *independentAwaitLoopAnalysis) awaitsTheItemItself(operand *ast.Node) bool {
	unwrapped := independentAwaitLoopSkipWrappers(operand)
	switch unwrapped.Kind {
	case ast.KindIdentifier:
		if analysis.loop.Kind != ast.KindForOfStatement {
			return false
		}
		symbol := analysis.symbolOf(unwrapped)
		if symbol == nil {
			return false
		}
		initializer := analysis.loop.AsForInOrOfStatement().Initializer
		for _, declaration := range symbol.Declarations {
			if declaration.Pos() >= initializer.Pos() && declaration.End() <= initializer.End() {
				return true
			}
		}
	case ast.KindElementAccessExpression:
		if analysis.indexSymbol == nil {
			return false
		}
		access := unwrapped.AsElementAccessExpression()
		argument := independentAwaitLoopSkipWrappers(access.ArgumentExpression)
		collection := independentAwaitLoopSegments(analysis.collection)
		return argument.Kind == ast.KindIdentifier &&
			analysis.symbolOf(argument) == analysis.indexSymbol &&
			collection != nil &&
			strings.Join(independentAwaitLoopSegments(access.Expression), ".") == strings.Join(collection, ".")
	}
	return false
}

// hasOrderedSideEffect looks for output, pacing, progress and yields anywhere the iteration runs,
// callbacks included.
//
// Inside a `catch` the question narrows to the one asked of a callee: a `console.warn` reporting
// that one item failed is not output whose order the reader follows. Measured on ahra: a recall
// sample of upstream sites this rule declined found GmailApi.ts:583 silenced only by
// `console.warn('[Gmail] Failed to fetch draft:', ...)` in its catch, a loop that is otherwise the
// same shape as EmailApi.ts:504, which reports.
func (analysis *independentAwaitLoopAnalysis) hasOrderedSideEffect() bool {
	found := false
	var visit func(node *ast.Node, insideFunction bool, insideCatch bool)
	visit = func(node *ast.Node, insideFunction bool, insideCatch bool) {
		if node == nil || found {
			return
		}
		switch node.Kind {
		case ast.KindYieldExpression:
			if !insideFunction {
				found = true
				return
			}
		case ast.KindCallExpression:
			callee := node.AsCallExpression().Expression
			ordered := independentAwaitLoopIsOrderedCall(callee)
			if insideCatch {
				ordered = independentAwaitLoopIsCalleeOrderedCall(callee)
			}
			if ordered || analysis.calleeMakesOrderedCall(callee) {
				found = true
				return
			}
		}
		nextInsideFunction := insideFunction || independentAwaitLoopIsBoundary(node)
		nextInsideCatch := insideCatch || node.Kind == ast.KindCatchClause
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child, nextInsideFunction, nextInsideCatch)
			return found
		})
	}
	visit(analysis.body, false, false)
	return found
}

// calleeMakesOrderedCall follows a call to the function it names, one level, and answers whether
// that function's own body makes an ordered call.
//
// The loop body is not where a CLI prints. `await generateFacetMonthly(facet, month)` prints a
// multi-line block per month from inside the callee, and `announceDelivery(recipient, await ...)`
// prints a line per recipient, so the order the loop imposes is the order the reader sees.
// Measured on ahra: three of the first pass's thirteen false positives were exactly this, each with
// `console.log` directly in the callee.
//
// One level, deliberately. Following calls further reaches the shared helpers every module calls,
// and a log line three frames down in a request wrapper would silence nearly every API loop. The
// callee is resolved through the checker, so an imported function is followed to its source; a
// declaration file has no body and ends the walk.
func (analysis *independentAwaitLoopAnalysis) calleeMakesOrderedCall(callee *ast.Node) bool {
	unwrapped := independentAwaitLoopSkipWrappers(callee)
	var name *ast.Node
	switch unwrapped.Kind {
	case ast.KindIdentifier:
		name = unwrapped
	case ast.KindPropertyAccessExpression:
		name = unwrapped.AsPropertyAccessExpression().Name()
	default:
		return false
	}
	symbol := analysis.ruleContext.TypeChecker.GetSymbolAtLocation(name)
	if symbol == nil {
		return false
	}
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		symbol = checker.SkipAlias(symbol, analysis.ruleContext.TypeChecker)
		if symbol == nil {
			return false
		}
	}
	for _, declaration := range symbol.Declarations {
		if body := independentAwaitLoopFunctionBody(declaration); body != nil && analysis.bodyMakesOrderedCall(body) {
			return true
		}
	}
	return false
}

// bodyMakesOrderedCall scans one function body for a call ordered by name, memoized per body
// because the same helper is called from many loops in one file.
func (analysis *independentAwaitLoopAnalysis) bodyMakesOrderedCall(body *ast.Node) bool {
	if answer, known := analysis.orderedBodies[body]; known {
		return answer
	}
	found := false
	var visit func(node *ast.Node)
	visit = func(node *ast.Node) {
		if node == nil || found {
			return
		}
		// A failure path is not the output a reader watches. Measured on ahra: the first version of
		// this scan counted catch blocks and silenced thirteen loops, among them every boot loader
		// (`console.error` when a domain fails to load), FinanceSync (`delay` before its one retry)
		// and AhraOsTriggers (`delay` before a gh retry). All of them sit in a catch.
		if node.Kind == ast.KindCatchClause {
			return
		}
		if node.Kind == ast.KindCallExpression &&
			independentAwaitLoopIsCalleeOrderedCall(node.AsCallExpression().Expression) {
			found = true
			return
		}
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return found
		})
	}
	visit(body)
	analysis.orderedBodies[body] = found
	return found
}

// symbolOf resolves an identifier occurrence to its binding, reading a shorthand property's value
// rather than the property it creates.
func (analysis *independentAwaitLoopAnalysis) symbolOf(identifier *ast.Node) *ast.Symbol {
	if identifier.Parent != nil && identifier.Parent.Kind == ast.KindShorthandPropertyAssignment &&
		identifier.Parent.AsShorthandPropertyAssignment().Name() == identifier {
		return analysis.ruleContext.TypeChecker.GetShorthandAssignmentValueSymbol(identifier.Parent)
	}
	return analysis.ruleContext.TypeChecker.GetSymbolAtLocation(identifier)
}

// isOuter answers whether a binding outlives one iteration: no declaration of it lies inside the
// loop. An unresolved name is a global and outer by definition.
func (analysis *independentAwaitLoopAnalysis) isOuter(symbol *ast.Symbol) bool {
	if symbol == nil || len(symbol.Declarations) == 0 {
		return true
	}
	for _, declaration := range symbol.Declarations {
		if ast.GetSourceFileOfNode(declaration) == analysis.ruleContext.SourceFile &&
			declaration.Pos() >= analysis.loop.Pos() && declaration.End() <= analysis.loop.End() {
			return false
		}
	}
	return true
}

// rootIsOuter answers whether a property chain's root outlives an iteration.
func (analysis *independentAwaitLoopAnalysis) rootIsOuter(root *ast.Node) bool {
	if root == nil {
		return false
	}
	if root.Kind == ast.KindThisKeyword {
		return true
	}
	return analysis.isOuter(analysis.symbolOf(root))
}

// carriesState looks for anything one iteration leaves for the next.
func (analysis *independentAwaitLoopAnalysis) carriesState() bool {
	var mutated []independentAwaitLoopPath
	var reads []independentAwaitLoopPath
	sinkRoots := map[*ast.Node]bool{}
	carried := false

	recordSink := func(target *ast.Node) {
		root := independentAwaitLoopRoot(target)
		if root == nil || !analysis.rootIsOuter(root) {
			return
		}
		mutated = append(mutated, independentAwaitLoopPath{segments: independentAwaitLoopSegments(target), root: root})
		sinkRoots[root] = true
	}

	// outerPropertyTarget answers the outer chain a property or element write lands in: the whole
	// chain for `a.b.c = x`, the object for `a.b[key] = x`. Nil when the target is not outer.
	outerPropertyTarget := func(target *ast.Node) *ast.Node {
		unwrapped := independentAwaitLoopSkipWrappers(target)
		switch unwrapped.Kind {
		case ast.KindPropertyAccessExpression:
			if analysis.rootIsOuter(independentAwaitLoopRoot(unwrapped)) {
				return unwrapped
			}
		case ast.KindElementAccessExpression:
			object := unwrapped.AsElementAccessExpression().Expression
			if analysis.rootIsOuter(independentAwaitLoopRoot(object)) {
				return object
			}
		}
		return nil
	}

	var visit func(node *ast.Node, insideFunction bool)
	visit = func(node *ast.Node, insideFunction bool) {
		if node == nil || carried {
			return
		}
		switch node.Kind {
		case ast.KindVariableDeclarationList:
			// A `var` is function-scoped, so the value one iteration writes is there for the next.
			if !insideFunction && node.Flags&(ast.NodeFlagsLet|ast.NodeFlagsConst|ast.NodeFlagsUsing) == 0 {
				carried = true
				return
			}

		case ast.KindBinaryExpression:
			binary := node.AsBinaryExpression()
			if ast.IsAssignmentOperator(binary.OperatorToken.Kind) {
				if target := outerPropertyTarget(binary.Left); target != nil {
					if binary.OperatorToken.Kind != ast.KindEqualsToken {
						carried = true
						return
					}
					recordSink(target)
				}
			}

		case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
			var operator ast.Kind
			var operand *ast.Node
			if node.Kind == ast.KindPrefixUnaryExpression {
				operator, operand = node.AsPrefixUnaryExpression().Operator, node.AsPrefixUnaryExpression().Operand
			} else {
				operator, operand = node.AsPostfixUnaryExpression().Operator, node.AsPostfixUnaryExpression().Operand
			}
			if (operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken) &&
				outerPropertyTarget(operand) != nil {
				carried = true
				return
			}

		case ast.KindDeleteExpression:
			if target := outerPropertyTarget(node.AsDeleteExpression().Expression); target != nil {
				recordSink(target)
			}

		case ast.KindCallExpression:
			call := node.AsCallExpression()
			callee := independentAwaitLoopSkipWrappers(call.Expression)
			if callee.Kind == ast.KindPropertyAccessExpression {
				access := callee.AsPropertyAccessExpression()
				method := access.Name().Text()
				receiverRoot := independentAwaitLoopRoot(access.Expression)
				if receiverRoot != nil && analysis.rootIsOuter(receiverRoot) {
					if independentAwaitLoopConsumingMethods[method] {
						carried = true
						return
					}
					if independentAwaitLoopIsSinkMethod(method) {
						recordSink(access.Expression)
					}
				}
				// `Object.assign(target, ...)` fills its first argument.
				if method == "assign" && call.Arguments != nil && len(call.Arguments.Nodes) > 0 {
					if objectSegments := independentAwaitLoopSegments(access.Expression); len(objectSegments) == 1 &&
						objectSegments[0] == "Object" {
						recordSink(call.Arguments.Nodes[0])
					}
				}
			}

		case ast.KindIdentifier:
			if independentAwaitLoopIsNameOnly(node) {
				break
			}
			symbol := analysis.symbolOf(node)
			if reference.WritesToBinding(node) {
				// The indexed form's counter is declared in the header, so it reads as local, and a
				// write to it in the body is exactly the loop-carried state this exists to catch.
				if analysis.isOuter(symbol) || (analysis.indexSymbol != nil && symbol == analysis.indexSymbol) {
					carried = true
					return
				}
				break
			}
			if analysis.isOuter(symbol) {
				reads = append(
					reads,
					independentAwaitLoopPath{segments: independentAwaitLoopReadSegments(node), root: node},
				)
			}

		case ast.KindThisKeyword:
			reads = append(
				reads,
				independentAwaitLoopPath{segments: independentAwaitLoopReadSegments(node), root: node},
			)
		}

		nextInsideFunction := insideFunction || independentAwaitLoopIsBoundary(node)
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child, nextInsideFunction)
			return carried
		})
	}
	visit(analysis.body, false)
	if carried {
		return true
	}
	if len(mutated) == 0 {
		return false
	}

	// The iterable is read by the loop itself, so a body that grows the collection it walks is
	// a work queue rather than a map.
	var collectionReads []independentAwaitLoopPath
	var collect func(node *ast.Node)
	collect = func(node *ast.Node) {
		if node == nil {
			return
		}
		if node.Kind == ast.KindIdentifier && !independentAwaitLoopIsNameOnly(node) ||
			node.Kind == ast.KindThisKeyword {
			collectionReads = append(
				collectionReads,
				independentAwaitLoopPath{segments: independentAwaitLoopReadSegments(node), root: node},
			)
		}
		node.ForEachChild(func(child *ast.Node) bool {
			collect(child)
			return false
		})
	}
	collect(analysis.collection)

	for _, sink := range mutated {
		for _, read := range append(reads, collectionReads...) {
			if sinkRoots[read.root] {
				continue
			}
			if independentAwaitLoopPathsOverlap(sink.segments, read.segments) {
				return true
			}
		}
	}
	return false
}

// computeTaint finds the bindings declared in the body whose value derives from an await, to a
// fixpoint, so `const response = await f(); const ok = response.ok;` taints both.
func (analysis *independentAwaitLoopAnalysis) computeTaint() {
	taintNames := func(name *ast.Node) bool {
		changed := false
		var mark func(node *ast.Node)
		mark = func(node *ast.Node) {
			if node == nil {
				return
			}
			if node.Kind == ast.KindIdentifier {
				if symbol := analysis.ruleContext.TypeChecker.GetSymbolAtLocation(
					node,
				); symbol != nil && !analysis.taintedSymbols[symbol] {
					analysis.taintedSymbols[symbol] = true
					changed = true
				}
				return
			}
			node.ForEachChild(func(child *ast.Node) bool {
				// A default value inside a pattern is an expression, not a name being bound.
				if child.Kind == ast.KindIdentifier && node.Kind == ast.KindBindingElement &&
					node.AsBindingElement().Initializer == child {
					return false
				}
				mark(child)
				return false
			})
		}
		mark(name)
		return changed
	}

	for range 8 {
		changed := false
		var visit func(node *ast.Node)
		visit = func(node *ast.Node) {
			if node == nil || independentAwaitLoopIsBoundary(node) {
				return
			}
			switch node.Kind {
			case ast.KindVariableDeclaration:
				declaration := node.AsVariableDeclaration()
				if declaration.Initializer != nil && analysis.containsTaint(declaration.Initializer) {
					changed = taintNames(declaration.Name()) || changed
				}
			case ast.KindBinaryExpression:
				binary := node.AsBinaryExpression()
				if ast.IsAssignmentOperator(binary.OperatorToken.Kind) && analysis.containsTaint(binary.Right) {
					changed = taintNames(binary.Left) || changed
				}
			case ast.KindCatchClause:
				clause := node.AsCatchClause()
				if clause.VariableDeclaration != nil &&
					analysis.containsAwait(node.Parent.AsTryStatement().TryBlock, -1) {
					changed = taintNames(clause.VariableDeclaration.AsVariableDeclaration().Name()) || changed
				}
			case ast.KindForOfStatement, ast.KindForInStatement:
				statement := node.AsForInOrOfStatement()
				if analysis.containsTaint(statement.Expression) {
					changed = taintNames(statement.Initializer) || changed
				}
			}
			node.ForEachChild(func(child *ast.Node) bool {
				visit(child)
				return false
			})
		}
		visit(analysis.body)
		if !changed {
			return
		}
	}
}

// containsTaint answers whether an expression awaits, or reads a binding derived from an await.
func (analysis *independentAwaitLoopAnalysis) containsTaint(node *ast.Node) bool {
	found := false
	var visit func(current *ast.Node)
	visit = func(current *ast.Node) {
		if current == nil || found || independentAwaitLoopIsBoundary(current) {
			return
		}
		switch current.Kind {
		case ast.KindAwaitExpression:
			found = true
			return
		case ast.KindIdentifier:
			if !independentAwaitLoopIsNameOnly(current) && analysis.taintedSymbols[analysis.symbolOf(current)] {
				found = true
				return
			}
		}
		current.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return found
		})
	}
	visit(node)
	return found
}

// containsAwait answers whether a subtree awaits, optionally only before a position (-1 for
// anywhere).
func (analysis *independentAwaitLoopAnalysis) containsAwait(node *ast.Node, before int) bool {
	found := false
	var visit func(current *ast.Node)
	visit = func(current *ast.Node) {
		if current == nil || found || independentAwaitLoopIsBoundary(current) {
			return
		}
		if current.Kind == ast.KindAwaitExpression && (before < 0 || current.Pos() < before) {
			found = true
			return
		}
		current.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return found
		})
	}
	visit(node)
	return found
}

// exitsOnAwaitedValue looks for a jump out of the loop whose guard reads an awaited value.
func (analysis *independentAwaitLoopAnalysis) exitsOnAwaitedValue() bool {
	found := false
	var visit func(node *ast.Node)
	visit = func(node *ast.Node) {
		if node == nil || found || independentAwaitLoopIsBoundary(node) {
			return
		}
		switch node.Kind {
		case ast.KindBreakStatement, ast.KindContinueStatement, ast.KindReturnStatement, ast.KindThrowStatement:
			if analysis.exitsLoop(node) && analysis.isGuardedByTaint(node, analysis.body, true) {
				found = true
				return
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return found
		})
	}
	visit(analysis.body)
	return found
}

// exitsLoop answers whether a jump leaves this loop rather than a construct inside it.
func (analysis *independentAwaitLoopAnalysis) exitsLoop(jump *ast.Node) bool {
	switch jump.Kind {
	case ast.KindReturnStatement:
		return true

	case ast.KindThrowStatement:
		// Caught inside the body, a throw is a jump to the handler, not out of the loop.
		for child, parent := jump, jump.Parent; parent != nil &&
			child != analysis.body; child, parent = parent, parent.Parent {
			if parent.Kind == ast.KindTryStatement {
				try := parent.AsTryStatement()
				if try.TryBlock == child && try.CatchClause != nil {
					return false
				}
			}
		}
		return true

	case ast.KindBreakStatement, ast.KindContinueStatement:
		var label *ast.Node
		if jump.Kind == ast.KindBreakStatement {
			label = jump.AsBreakStatement().Label
		} else {
			label = jump.AsContinueStatement().Label
		}
		isBreak := jump.Kind == ast.KindBreakStatement

		if label == nil {
			// The nearest loop (or, for break, switch) is the target.
			for current := jump.Parent; current != nil; current = current.Parent {
				if current == analysis.loop {
					return isBreak
				}
				if ast.IsIterationStatement(current, false) || (isBreak && current.Kind == ast.KindSwitchStatement) {
					return false
				}
			}
			return false
		}

		for current := jump.Parent; current != nil && current != analysis.loop; current = current.Parent {
			if current.Kind == ast.KindLabeledStatement && current.AsLabeledStatement().Label.Text() == label.Text() {
				return false
			}
		}
		for current := analysis.loop.Parent; current != nil &&
			current.Kind == ast.KindLabeledStatement; current = current.Parent {
			if current.AsLabeledStatement().Label.Text() == label.Text() {
				return isBreak
			}
		}
		// A label outside this loop: the jump leaves it for an outer one.
		return true
	}
	return false
}

// isGuardedByTaint climbs from a jump to `limit`, asking at each step whether what decides that
// the jump runs reads an awaited value. `withSiblings` also asks whether an earlier statement jumps
// on such a value, which would decide whether this statement is reached at all.
func (analysis *independentAwaitLoopAnalysis) isGuardedByTaint(
	jump *ast.Node,
	limit *ast.Node,
	withSiblings bool,
) bool {
	switch jump.Kind {
	case ast.KindReturnStatement:
		if analysis.containsTaint(jump.AsReturnStatement().Expression) {
			return true
		}
	case ast.KindThrowStatement:
		if analysis.containsTaint(jump.AsThrowStatement().Expression) {
			return true
		}
	}

	for child, parent := jump, jump.Parent; parent != nil && child != limit; child, parent = parent, parent.Parent {
		switch parent.Kind {
		case ast.KindIfStatement:
			if analysis.containsTaint(parent.AsIfStatement().Expression) {
				return true
			}
		case ast.KindCaseClause, ast.KindDefaultClause:
			caseBlock := parent.Parent
			switchStatement := caseBlock.Parent.AsSwitchStatement()
			if analysis.containsTaint(switchStatement.Expression) {
				return true
			}
			for _, clause := range caseBlock.AsCaseBlock().Clauses.Nodes {
				if analysis.containsTaint(clause.AsCaseOrDefaultClause().Expression) {
					return true
				}
			}
		case ast.KindWhileStatement:
			if analysis.containsTaint(parent.AsWhileStatement().Expression) {
				return true
			}
		case ast.KindDoStatement:
			if analysis.containsTaint(parent.AsDoStatement().Expression) {
				return true
			}
		case ast.KindForStatement:
			if analysis.containsTaint(parent.AsForStatement().Condition) {
				return true
			}
		case ast.KindForOfStatement, ast.KindForInStatement:
			if analysis.containsTaint(parent.AsForInOrOfStatement().Expression) {
				return true
			}
		case ast.KindCatchClause:
			// Reached only when the try threw, and an await is what throws asynchronously.
			if analysis.containsAwait(parent.Parent.AsTryStatement().TryBlock, -1) {
				return true
			}
		case ast.KindTryStatement:
			// `try { return await probe(); } catch {}` exits only when the await resolved.
			try := parent.AsTryStatement()
			if try.TryBlock == child && try.CatchClause != nil && analysis.containsAwait(try.TryBlock, jump.Pos()) {
				return true
			}
		case ast.KindBlock:
			if withSiblings && analysis.earlierSiblingJumpsOnTaint(parent.AsBlock().Statements.Nodes, child) {
				return true
			}
		}
		if parent.Kind == ast.KindCaseClause || parent.Kind == ast.KindDefaultClause {
			if withSiblings &&
				analysis.earlierSiblingJumpsOnTaint(parent.AsCaseOrDefaultClause().Statements.Nodes, child) {
				return true
			}
		}
	}
	return false
}

// earlierSiblingJumpsOnTaint answers whether a statement before `child` holds a jump guarded by an
// awaited value. Conservative: any such jump counts, even one whose target lies inside the sibling.
func (analysis *independentAwaitLoopAnalysis) earlierSiblingJumpsOnTaint(statements []*ast.Node, child *ast.Node) bool {
	for _, sibling := range statements {
		if sibling == child || sibling.Pos() >= child.Pos() {
			return false
		}
		found := false
		var visit func(node *ast.Node)
		visit = func(node *ast.Node) {
			if node == nil || found || independentAwaitLoopIsBoundary(node) {
				return
			}
			switch node.Kind {
			case ast.KindBreakStatement, ast.KindContinueStatement, ast.KindReturnStatement, ast.KindThrowStatement:
				if analysis.isGuardedByTaint(node, sibling, false) {
					found = true
					return
				}
			}
			node.ForEachChild(func(grandchild *ast.Node) bool {
				visit(grandchild)
				return found
			})
		}
		visit(sibling)
		if found {
			return true
		}
	}
	return false
}
