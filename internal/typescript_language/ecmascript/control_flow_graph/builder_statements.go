package control_flow_graph

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

func (builder *Builder[E]) statements(list *ast.NodeList) {
	if list == nil {
		return
	}
	for _, statement := range list.Nodes {
		builder.statement(statement)
	}
}

func (builder *Builder[E]) statement(node *ast.Node) {
	if node == nil {
		return
	}
	builder.reachedStatement(node)

	switch node.Kind {
	case ast.KindBlock:
		builder.statements(node.AsBlock().Statements)

	case ast.KindVariableStatement:
		builder.variableDeclarationList(node.AsVariableStatement().DeclarationList)

	case ast.KindExpressionStatement:
		builder.expression(node.AsExpressionStatement().Expression)

	case ast.KindIfStatement:
		builder.ifStatement(node)

	case ast.KindWhileStatement:
		builder.whileStatement(node)

	case ast.KindDoStatement:
		builder.doStatement(node)

	case ast.KindForStatement:
		builder.forStatement(node)

	case ast.KindForInStatement, ast.KindForOfStatement:
		builder.forInOfStatement(node)

	case ast.KindSwitchStatement:
		builder.switchStatement(node)

	case ast.KindTryStatement:
		builder.tryStatement(node)

	case ast.KindLabeledStatement:
		builder.labeledStatement(node)

	case ast.KindReturnStatement:
		builder.expression(node.AsReturnStatement().Expression)
		builder.makeReturn()

	case ast.KindThrowStatement:
		builder.expression(node.AsThrowStatement().Expression)
		builder.makeThrow()

	case ast.KindBreakStatement:
		builder.makeBreak(node.AsBreakStatement().Label)

	case ast.KindContinueStatement:
		builder.makeContinue(node.AsContinueStatement().Label)

	case ast.KindWithStatement:
		with := node.AsWithStatement()
		builder.expression(with.Expression)
		builder.statement(with.Statement)

	case ast.KindEmptyStatement, ast.KindDebuggerStatement,
		ast.KindImportDeclaration, ast.KindImportEqualsDeclaration,
		ast.KindFunctionDeclaration:
		// Nothing executes here in the enclosing code path. A function
		// declaration's parameters and body are its own code path root.

	case ast.KindClassDeclaration:
		builder.classLike(node)

	case ast.KindExportAssignment:
		builder.expression(node.AsExportAssignment().Expression)

	case ast.KindExportDeclaration:
		// Export bindings are resolved statically; nothing is evaluated.

	default:
		node.ForEachChild(func(child *ast.Node) bool {
			builder.visitUnknown(child)
			return false
		})
	}
}

func (builder *Builder[E]) variableDeclarationList(list *ast.Node) {
	if list == nil {
		return
	}
	declList := list.AsVariableDeclarationList()
	if declList == nil || declList.Declarations == nil {
		return
	}
	for _, decl := range declList.Declarations.Nodes {
		builder.variableDeclaration(decl)
	}
}

func (builder *Builder[E]) variableDeclaration(node *ast.Node) {
	if node == nil {
		return
	}
	decl := node.AsVariableDeclaration()
	if decl == nil {
		return
	}
	// A `typeof x` inside the annotation references the variable, so the
	// annotation is walked even though nothing in it runs.
	builder.expression(decl.Type)
	name := decl.Name()
	if name != nil && (name.Kind == ast.KindObjectBindingPattern || name.Kind == ast.KindArrayBindingPattern) {
		// Run-time evaluation order: the initializer produces the value
		// first, then the pattern binds element by element.
		builder.expression(decl.Initializer)
		builder.patternBind(name)
		return
	}
	if decl.Initializer == nil {
		// `let x;` stores no value.
		builder.patternReads(name)
		return
	}
	builder.patternReads(name)
	builder.expression(decl.Initializer)
	builder.patternWrites(name)
}

func (builder *Builder[E]) ifStatement(node *ast.Node) {
	statement := node.AsIfStatement()
	after := builder.newBlock()
	thenEntry := builder.newBlock()
	elseEntry := after
	if statement.ElseStatement != nil {
		elseEntry = builder.newBlock()
	}
	builder.condition(statement.Expression, thenEntry, elseEntry)

	builder.enter(thenEntry)
	builder.statement(statement.ThenStatement)
	builder.link(builder.current, after)

	if statement.ElseStatement != nil {
		builder.enter(elseEntry)
		builder.statement(statement.ElseStatement)
		builder.link(builder.current, after)
	}

	builder.enter(after)
}

func (builder *Builder[E]) whileStatement(node *ast.Node) {
	statement := node.AsWhileStatement()
	test := builder.newBlock()
	builder.link(builder.current, test)
	builder.enter(test)
	after := builder.newBlock()
	body := builder.newBlock()
	falseEntry := after
	if isAlwaysTruthyTest(statement.Expression) {
		falseEntry = nil
	}
	builder.condition(statement.Expression, body, falseEntry)

	builder.pushJump(node, after, test, node)
	builder.enter(body)
	builder.statement(statement.Statement)
	builder.loop(node)
	builder.link(builder.current, test)
	builder.popJump()

	builder.enter(after)
}

func (builder *Builder[E]) doStatement(node *ast.Node) {
	statement := node.AsDoStatement()
	body := builder.newBlock()
	builder.link(builder.current, body)
	test := builder.newBlock()
	after := builder.newBlock()

	// A `continue` restarts a `do`…`while` at its test, not at its body, so it
	// is not what starts another iteration: the test passing is.
	builder.pushJump(node, after, test, nil)
	builder.enter(body)
	builder.statement(statement.Statement)
	builder.link(builder.current, test)
	builder.popJump()

	builder.enter(test)
	builder.expression(statement.Expression)
	builder.loop(node)
	builder.link(builder.current, body)
	if !isAlwaysTruthyTest(statement.Expression) {
		builder.link(builder.current, after)
	}

	builder.enter(after)
}

func (builder *Builder[E]) forStatement(node *ast.Node) {
	statement := node.AsForStatement()
	if statement.Initializer != nil {
		if statement.Initializer.Kind == ast.KindVariableDeclarationList {
			builder.variableDeclarationList(statement.Initializer)
		} else {
			builder.expression(statement.Initializer)
		}
	}
	if statement.Condition == nil && statement.Incrementor == nil {
		// ESLint keeps the body of `for (;;) { ... }` on the loop-header
		// segment itself. Model that shape as one self-looping block: events at
		// the top of the body stay on the repeated header, while a branch in the
		// body still creates later blocks that cycle analysis can identify.
		body := builder.newBlock()
		builder.link(builder.current, body)
		after := builder.newBlock()
		builder.pushJump(node, after, body, node)
		builder.enter(body)
		builder.statement(statement.Statement)
		builder.loop(node)
		builder.link(builder.current, body)
		builder.popJump()
		builder.enter(after)
		return
	}

	test := builder.newBlock()
	builder.link(builder.current, test)
	builder.enter(test)
	after := builder.newBlock()
	body := builder.newBlock()
	falseEntry := after
	if statement.Condition == nil || isAlwaysTruthyTest(statement.Condition) {
		falseEntry = nil
	}
	builder.condition(statement.Condition, body, falseEntry)

	// The incrementor runs after the body and is laid out before it, so a body
	// that always leaves abruptly does not make it unreachable and lose the
	// fork a throwable node in it makes.
	update := test
	if statement.Incrementor != nil {
		update = builder.newBlock()
		builder.enterDisconnected(update)
		builder.expression(statement.Incrementor)
		builder.link(builder.current, test)
	}

	builder.pushJump(node, after, update, node)
	builder.enter(body)
	builder.statement(statement.Statement)
	builder.loop(node)
	builder.link(builder.current, update)
	builder.popJump()

	builder.enter(after)
}

func (builder *Builder[E]) forInOfStatement(node *ast.Node) {
	statement := node.AsForInOrOfStatement()
	builder.expression(statement.Expression)

	head := builder.newBlock()
	builder.link(builder.current, head)
	builder.enter(head)

	after := builder.newBlock()
	builder.link(head, after)
	body := builder.newBlock()
	builder.link(head, body)

	builder.pushJump(node, after, head, node)
	builder.enter(body)
	if statement.Initializer != nil {
		// The loop variable takes its value from the iteration, so it binds
		// with no expression of its own; its computed keys and destructuring
		// defaults evaluate once per iteration.
		if statement.Initializer.Kind == ast.KindVariableDeclarationList {
			declList := statement.Initializer.AsVariableDeclarationList()
			if declList != nil && declList.Declarations != nil {
				for _, decl := range declList.Declarations.Nodes {
					builder.patternBind(decl.Name())
				}
			}
		} else {
			builder.patternBind(statement.Initializer)
		}
	}
	builder.statement(statement.Statement)
	builder.loop(node)
	builder.link(builder.current, head)
	builder.popJump()

	builder.enter(after)
}

func (builder *Builder[E]) switchStatement(node *ast.Node) {
	statement := node.AsSwitchStatement()
	builder.expression(statement.Expression)

	after := builder.newBlock()
	var clauses []*ast.Node
	if statement.CaseBlock != nil {
		if caseBlock := statement.CaseBlock.AsCaseBlock(); caseBlock != nil && caseBlock.Clauses != nil {
			clauses = caseBlock.Clauses.Nodes
		}
	}
	if len(clauses) == 0 {
		builder.link(builder.current, after)
		builder.enter(after)
		return
	}

	bodies := make([]*Block[E], len(clauses))
	dispatchFrom := make([]*Block[E], len(clauses))
	dispatchEdge := make([]int, len(clauses))
	for index := range dispatchEdge {
		dispatchEdge[index] = -1
	}
	for index := range clauses {
		bodies[index] = builder.newBlock()
	}

	defaultIndex := -1
	firstCaseIndex := -1
	testCur := builder.current
	for index, clause := range clauses {
		if clause.Kind == ast.KindDefaultClause {
			defaultIndex = index
			continue
		}
		if firstCaseIndex == -1 {
			firstCaseIndex = index
		}
		builder.current = testCur
		builder.expression(clause.AsCaseOrDefaultClause().Expression)
		dispatchFrom[index] = builder.current
		dispatchEdge[index] = builder.linkWithCycleBarrier(builder.current, bodies[index], false)
		nextTest := builder.newBlock()
		builder.link(builder.current, nextTest)
		builder.enter(nextTest)
		testCur = builder.current
	}
	if defaultIndex >= 0 {
		dispatchFrom[defaultIndex] = testCur
		dispatchEdge[defaultIndex] = builder.linkWithCycleBarrier(testCur, bodies[defaultIndex], false)
	} else {
		builder.link(testCur, after)
	}

	builder.pushJump(node, after, nil, nil)
	switchJump := len(builder.jumps) - 1
	hadSwitchBreak := false
	var fallthroughFrom *Block[E]
	for index, clause := range clauses {
		beforeFirstCase := firstCaseIndex == -1 || clause.Kind == ast.KindDefaultClause && index < firstCaseIndex
		barrier := hadSwitchBreak || beforeFirstCase
		if from := dispatchFrom[index]; from != nil && dispatchEdge[index] >= 0 {
			builder.setCycleBarrier(from, dispatchEdge[index], barrier)
		}
		builder.linkWithCycleBarrier(fallthroughFrom, bodies[index], barrier)
		builder.enter(bodies[index])
		builder.statements(clause.AsCaseOrDefaultClause().Statements)
		fallthroughFrom = builder.current
		if index >= firstCaseIndex && firstCaseIndex != -1 {
			hadSwitchBreak = builder.jumps[switchJump].broken
		}
	}
	builder.link(fallthroughFrom, after)
	builder.popJump()

	builder.enter(after)
}

func (builder *Builder[E]) tryStatement(node *ast.Node) {
	statement := node.AsTryStatement()
	hasCatch := statement.CatchClause != nil
	hasFinally := statement.FinallyBlock != nil

	frame := &tryFrame[E]{position: positionTry, hasFinally: hasFinally}
	if hasCatch {
		frame.catchEntry = builder.newBlock()
	}
	if hasFinally {
		frame.finallyEntry = builder.newBlock()
	}
	builder.tryStack = append(builder.tryStack, frame)

	builder.statement(statement.TryBlock)

	// The normal completion of a `try` statement carries on from the end of its
	// `try` block and from the end of its `catch` block, whether or not either
	// of them is somewhere control arrives.
	var normalEnds []*Block[E]
	if hasCatch {
		// ESLint routes the end of a `try` block into the `catch` block as well
		// as past the statement.
		builder.link(builder.current, frame.catchEntry)
		normalEnds = append(normalEnds, builder.current)
		frame.position = positionCatch
		frame.thrownForked = false
		builder.enter(frame.catchEntry)
		catchClause := statement.CatchClause.AsCatchClause()
		if catchClause.VariableDeclaration != nil {
			builder.patternBind(catchClause.VariableDeclaration.Name())
		}
		builder.statement(catchClause.Block)
	}
	normalEnds = append(normalEnds, builder.current)

	builder.tryStack = builder.tryStack[:len(builder.tryStack)-1]

	after := builder.newBlock()
	if !hasFinally {
		for _, end := range normalEnds {
			builder.link(end, after)
		}
		builder.enter(after)
		return
	}

	snapshot := builder.snapshotForks()
	normalEntry := builder.newBlock()
	for _, end := range normalEnds {
		builder.link(end, normalEntry)
	}
	for _, source := range frame.implicit {
		builder.link(source, normalEntry)
	}
	builder.enter(normalEntry)
	builder.statement(statement.FinallyBlock)
	builder.link(builder.current, after)

	if frame.finallyEntry.hasIncoming {
		// The `finally` block also runs on the path that leaves the statement
		// abruptly, so it is laid out a second time. Rewinding the enclosing
		// frames lets that copy fork at the same node the first copy did.
		builder.restoreForks(snapshot)
		builder.enter(frame.finallyEntry)
		builder.statement(statement.FinallyBlock)
		if builder.current.Reachable {
			// A `return` inside this `try`/`catch` still needs to run every
			// enclosing `finally` on its way out, not just this one. So this
			// second copy's own abrupt exit must keep propagating outward.
			if frame.returnedAny {
				if index := builder.returnFrame(); index >= 0 {
					outer := builder.tryStack[index]
					outer.returnedAny = true
					builder.link(builder.current, outer.finallyEntry)
				} else {
					builder.markFinal(builder.current)
				}
			}
			if frame.thrownAny {
				if index := builder.throwFrame(); index >= 0 {
					outer := builder.tryStack[index]
					outer.thrownAny = true
					builder.link(builder.current, throwTarget(outer))
				} else {
					builder.markThrown(builder.current)
				}
			}
		}
	}

	builder.enter(after)
}

func (builder *Builder[E]) labeledStatement(node *ast.Node) {
	statement := node.AsLabeledStatement()
	if isBreakableStatement(statement.Statement) {
		// The label belongs to the loop or switch itself; pushJump reads it
		// back off the parent chain.
		builder.statement(statement.Statement)
		return
	}
	after := builder.newBlock()
	builder.jumps = append(builder.jumps, jumpTarget[E]{labels: []string{statement.Label.Text()}, breakTo: after})
	builder.statement(statement.Statement)
	builder.popJump()
	builder.link(builder.current, after)
	builder.enter(after)
}

func (builder *Builder[E]) pushJump(node *ast.Node, breakTo, continueTo *Block[E], loop *ast.Node) {
	builder.jumps = append(
		builder.jumps,
		jumpTarget[E]{labels: labelsOf(node), breakTo: breakTo, continueTo: continueTo, loop: loop, breakable: true},
	)
}

func (builder *Builder[E]) popJump() {
	builder.jumps = builder.jumps[:len(builder.jumps)-1]
}

func (builder *Builder[E]) makeBreak(label *ast.Node) {
	if !builder.current.Reachable {
		return
	}
	name := ""
	if label != nil {
		name = label.Text()
	}
	for index := len(builder.jumps) - 1; index >= 0; index-- {
		target := builder.jumps[index]
		if name == "" {
			if !target.breakable {
				continue
			}
		} else if !slices.Contains(target.labels, name) {
			continue
		}
		builder.jumps[index].broken = true
		builder.link(builder.current, target.breakTo)
		break
	}
	builder.makeUnreachable()
}

func (builder *Builder[E]) makeContinue(label *ast.Node) {
	if !builder.current.Reachable {
		return
	}
	name := ""
	if label != nil {
		name = label.Text()
	}
	for index := len(builder.jumps) - 1; index >= 0; index-- {
		target := builder.jumps[index]
		if target.continueTo == nil {
			continue
		}
		if name == "" || slices.Contains(target.labels, name) {
			builder.loop(target.loop)
			builder.link(builder.current, target.continueTo)
			break
		}
	}
	builder.makeUnreachable()
}

func (builder *Builder[E]) makeReturn() {
	if !builder.current.Reachable {
		return
	}
	if index := builder.returnFrame(); index >= 0 {
		frame := builder.tryStack[index]
		frame.returnedAny = true
		builder.link(builder.current, frame.finallyEntry)
	} else {
		builder.markFinal(builder.current)
	}
	builder.makeUnreachable()
}

func (builder *Builder[E]) makeThrow() {
	if !builder.current.Reachable {
		return
	}
	if index := builder.throwFrame(); index >= 0 {
		frame := builder.tryStack[index]
		frame.thrownAny = true
		builder.link(builder.current, throwTarget(frame))
	} else {
		builder.markThrown(builder.current)
	}
	builder.makeUnreachable()
}

// makeYield mirrors ESLint's handling of a suspended generator: the caller may
// resume it with `.return()` or `.throw()`, so both leaving paths are recorded
// before the normal continuation resumes in a fresh block.
func (builder *Builder[E]) makeYield() {
	if !builder.current.Reachable {
		return
	}
	if index := builder.returnFrame(); index >= 0 {
		frame := builder.tryStack[index]
		frame.returnedAny = true
		builder.link(builder.current, frame.finallyEntry)
	} else {
		builder.markFinal(builder.current)
	}
	if index := builder.throwFrame(); index >= 0 {
		frame := builder.tryStack[index]
		frame.thrownAny = true
		builder.link(builder.current, throwTarget(frame))
	} else {
		builder.markThrown(builder.current)
	}
	next := builder.newBlock()
	builder.link(builder.current, next)
	builder.enter(next)
}
