package control_flow_graph

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

func (builder *Builder[E]) statements(list *ast.NodeList) {
	if list == nil {
		return
	}
	for _, stmt := range list.Nodes {
		builder.statement(stmt)
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
		builder.expr(node.AsExpressionStatement().Expression)

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
		builder.expr(node.AsReturnStatement().Expression)
		builder.makeReturn()

	case ast.KindThrowStatement:
		builder.expr(node.AsThrowStatement().Expression)
		builder.makeThrow()

	case ast.KindBreakStatement:
		builder.makeBreak(node.AsBreakStatement().Label)

	case ast.KindContinueStatement:
		builder.makeContinue(node.AsContinueStatement().Label)

	case ast.KindWithStatement:
		with := node.AsWithStatement()
		builder.expr(with.Expression)
		builder.statement(with.Statement)

	case ast.KindEmptyStatement, ast.KindDebuggerStatement,
		ast.KindImportDeclaration, ast.KindImportEqualsDeclaration,
		ast.KindFunctionDeclaration:
		// Nothing executes here in the enclosing code path. A function
		// declaration's parameters and body are its own code path root.

	case ast.KindClassDeclaration:
		builder.classLike(node)

	case ast.KindExportAssignment:
		builder.expr(node.AsExportAssignment().Expression)

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
	builder.expr(decl.Type)
	name := decl.Name()
	if name != nil && (name.Kind == ast.KindObjectBindingPattern || name.Kind == ast.KindArrayBindingPattern) {
		// Run-time evaluation order: the initializer produces the value
		// first, then the pattern binds element by element.
		builder.expr(decl.Initializer)
		builder.patternBind(name)
		return
	}
	if decl.Initializer == nil {
		// `let x;` stores no value.
		builder.patternReads(name)
		return
	}
	builder.patternReads(name)
	builder.expr(decl.Initializer)
	builder.patternWrites(name)
}

func (builder *Builder[E]) ifStatement(node *ast.Node) {
	stmt := node.AsIfStatement()
	after := builder.newBlock()
	thenEntry := builder.newBlock()
	elseEntry := after
	if stmt.ElseStatement != nil {
		elseEntry = builder.newBlock()
	}
	builder.condition(stmt.Expression, thenEntry, elseEntry)

	builder.enter(thenEntry)
	builder.statement(stmt.ThenStatement)
	builder.link(builder.cur, after)

	if stmt.ElseStatement != nil {
		builder.enter(elseEntry)
		builder.statement(stmt.ElseStatement)
		builder.link(builder.cur, after)
	}

	builder.enter(after)
}

func (builder *Builder[E]) whileStatement(node *ast.Node) {
	stmt := node.AsWhileStatement()
	test := builder.newBlock()
	builder.link(builder.cur, test)
	builder.enter(test)
	after := builder.newBlock()
	body := builder.newBlock()
	falseEntry := after
	if isAlwaysTruthyTest(stmt.Expression) {
		falseEntry = nil
	}
	builder.condition(stmt.Expression, body, falseEntry)

	builder.pushJump(node, after, test, node)
	builder.enter(body)
	builder.statement(stmt.Statement)
	builder.loop(node)
	builder.link(builder.cur, test)
	builder.popJump()

	builder.enter(after)
}

func (builder *Builder[E]) doStatement(node *ast.Node) {
	stmt := node.AsDoStatement()
	body := builder.newBlock()
	builder.link(builder.cur, body)
	test := builder.newBlock()
	after := builder.newBlock()

	// A `continue` restarts a `do`…`while` at its test, not at its body, so it
	// is not what starts another iteration: the test passing is.
	builder.pushJump(node, after, test, nil)
	builder.enter(body)
	builder.statement(stmt.Statement)
	builder.link(builder.cur, test)
	builder.popJump()

	builder.enter(test)
	builder.expr(stmt.Expression)
	builder.loop(node)
	builder.link(builder.cur, body)
	if !isAlwaysTruthyTest(stmt.Expression) {
		builder.link(builder.cur, after)
	}

	builder.enter(after)
}

func (builder *Builder[E]) forStatement(node *ast.Node) {
	stmt := node.AsForStatement()
	if stmt.Initializer != nil {
		if stmt.Initializer.Kind == ast.KindVariableDeclarationList {
			builder.variableDeclarationList(stmt.Initializer)
		} else {
			builder.expr(stmt.Initializer)
		}
	}
	if stmt.Condition == nil && stmt.Incrementor == nil {
		// ESLint keeps the body of `for (;;) { ... }` on the loop-header
		// segment itself. Model that shape as one self-looping block: events at
		// the top of the body stay on the repeated header, while a branch in the
		// body still creates later blocks that cycle analysis can identify.
		body := builder.newBlock()
		builder.link(builder.cur, body)
		after := builder.newBlock()
		builder.pushJump(node, after, body, node)
		builder.enter(body)
		builder.statement(stmt.Statement)
		builder.loop(node)
		builder.link(builder.cur, body)
		builder.popJump()
		builder.enter(after)
		return
	}

	test := builder.newBlock()
	builder.link(builder.cur, test)
	builder.enter(test)
	after := builder.newBlock()
	body := builder.newBlock()
	falseEntry := after
	if stmt.Condition == nil || isAlwaysTruthyTest(stmt.Condition) {
		falseEntry = nil
	}
	builder.condition(stmt.Condition, body, falseEntry)

	// The incrementor runs after the body and is laid out before it, so a body
	// that always leaves abruptly does not make it unreachable and lose the
	// fork a throwable node in it makes.
	update := test
	if stmt.Incrementor != nil {
		update = builder.newBlock()
		builder.enterDisconnected(update)
		builder.expr(stmt.Incrementor)
		builder.link(builder.cur, test)
	}

	builder.pushJump(node, after, update, node)
	builder.enter(body)
	builder.statement(stmt.Statement)
	builder.loop(node)
	builder.link(builder.cur, update)
	builder.popJump()

	builder.enter(after)
}

func (builder *Builder[E]) forInOfStatement(node *ast.Node) {
	stmt := node.AsForInOrOfStatement()
	builder.expr(stmt.Expression)

	head := builder.newBlock()
	builder.link(builder.cur, head)
	builder.enter(head)

	after := builder.newBlock()
	builder.link(head, after)
	body := builder.newBlock()
	builder.link(head, body)

	builder.pushJump(node, after, head, node)
	builder.enter(body)
	if stmt.Initializer != nil {
		// The loop variable takes its value from the iteration, so it binds
		// with no expression of its own; its computed keys and destructuring
		// defaults evaluate once per iteration.
		if stmt.Initializer.Kind == ast.KindVariableDeclarationList {
			declList := stmt.Initializer.AsVariableDeclarationList()
			if declList != nil && declList.Declarations != nil {
				for _, decl := range declList.Declarations.Nodes {
					builder.patternBind(decl.Name())
				}
			}
		} else {
			builder.patternBind(stmt.Initializer)
		}
	}
	builder.statement(stmt.Statement)
	builder.loop(node)
	builder.link(builder.cur, head)
	builder.popJump()

	builder.enter(after)
}

func (builder *Builder[E]) switchStatement(node *ast.Node) {
	stmt := node.AsSwitchStatement()
	builder.expr(stmt.Expression)

	after := builder.newBlock()
	var clauses []*ast.Node
	if stmt.CaseBlock != nil {
		if caseBlock := stmt.CaseBlock.AsCaseBlock(); caseBlock != nil && caseBlock.Clauses != nil {
			clauses = caseBlock.Clauses.Nodes
		}
	}
	if len(clauses) == 0 {
		builder.link(builder.cur, after)
		builder.enter(after)
		return
	}

	bodies := make([]*Block[E], len(clauses))
	dispatchFrom := make([]*Block[E], len(clauses))
	dispatchEdge := make([]int, len(clauses))
	for i := range dispatchEdge {
		dispatchEdge[i] = -1
	}
	for i := range clauses {
		bodies[i] = builder.newBlock()
	}

	defaultIndex := -1
	firstCaseIndex := -1
	testCur := builder.cur
	for i, clause := range clauses {
		if clause.Kind == ast.KindDefaultClause {
			defaultIndex = i
			continue
		}
		if firstCaseIndex == -1 {
			firstCaseIndex = i
		}
		builder.cur = testCur
		builder.expr(clause.AsCaseOrDefaultClause().Expression)
		dispatchFrom[i] = builder.cur
		dispatchEdge[i] = builder.linkWithCycleBarrier(builder.cur, bodies[i], false)
		nextTest := builder.newBlock()
		builder.link(builder.cur, nextTest)
		builder.enter(nextTest)
		testCur = builder.cur
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
	for i, clause := range clauses {
		beforeFirstCase := firstCaseIndex == -1 || clause.Kind == ast.KindDefaultClause && i < firstCaseIndex
		barrier := hadSwitchBreak || beforeFirstCase
		if from := dispatchFrom[i]; from != nil && dispatchEdge[i] >= 0 {
			builder.setCycleBarrier(from, dispatchEdge[i], barrier)
		}
		builder.linkWithCycleBarrier(fallthroughFrom, bodies[i], barrier)
		builder.enter(bodies[i])
		builder.statements(clause.AsCaseOrDefaultClause().Statements)
		fallthroughFrom = builder.cur
		if i >= firstCaseIndex && firstCaseIndex != -1 {
			hadSwitchBreak = builder.jumps[switchJump].broken
		}
	}
	builder.link(fallthroughFrom, after)
	builder.popJump()

	builder.enter(after)
}

func (builder *Builder[E]) tryStatement(node *ast.Node) {
	stmt := node.AsTryStatement()
	hasCatch := stmt.CatchClause != nil
	hasFinally := stmt.FinallyBlock != nil

	frame := &tryFrame[E]{position: posTry, hasFinally: hasFinally}
	if hasCatch {
		frame.catchEntry = builder.newBlock()
	}
	if hasFinally {
		frame.finallyEntry = builder.newBlock()
	}
	builder.tryStack = append(builder.tryStack, frame)

	builder.statement(stmt.TryBlock)

	// The normal completion of a `try` statement carries on from the end of its
	// `try` block and from the end of its `catch` block, whether or not either
	// of them is somewhere control arrives.
	var normalEnds []*Block[E]
	if hasCatch {
		// ESLint routes the end of a `try` block into the `catch` block as well
		// as past the statement.
		builder.link(builder.cur, frame.catchEntry)
		normalEnds = append(normalEnds, builder.cur)
		frame.position = posCatch
		frame.thrownForked = false
		builder.enter(frame.catchEntry)
		catchClause := stmt.CatchClause.AsCatchClause()
		if catchClause.VariableDeclaration != nil {
			builder.patternBind(catchClause.VariableDeclaration.Name())
		}
		builder.statement(catchClause.Block)
	}
	normalEnds = append(normalEnds, builder.cur)

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
	builder.statement(stmt.FinallyBlock)
	builder.link(builder.cur, after)

	if frame.finallyEntry.hasIncoming {
		// The `finally` block also runs on the path that leaves the statement
		// abruptly, so it is laid out a second time. Rewinding the enclosing
		// frames lets that copy fork at the same node the first copy did.
		builder.restoreForks(snapshot)
		builder.enter(frame.finallyEntry)
		builder.statement(stmt.FinallyBlock)
		if builder.cur.Reachable {
			// A `return` inside this `try`/`catch` still needs to run every
			// enclosing `finally` on its way out, not just this one. So this
			// second copy's own abrupt exit must keep propagating outward.
			if frame.returnedAny {
				if i := builder.returnFrame(); i >= 0 {
					outer := builder.tryStack[i]
					outer.returnedAny = true
					builder.link(builder.cur, outer.finallyEntry)
				} else {
					builder.markFinal(builder.cur)
				}
			}
			if frame.thrownAny {
				if i := builder.throwFrame(); i >= 0 {
					outer := builder.tryStack[i]
					outer.thrownAny = true
					builder.link(builder.cur, throwTarget(outer))
				} else {
					builder.markThrown(builder.cur)
				}
			}
		}
	}

	builder.enter(after)
}

func (builder *Builder[E]) labeledStatement(node *ast.Node) {
	stmt := node.AsLabeledStatement()
	if isBreakableStatement(stmt.Statement) {
		// The label belongs to the loop or switch itself; pushJump reads it
		// back off the parent chain.
		builder.statement(stmt.Statement)
		return
	}
	after := builder.newBlock()
	builder.jumps = append(builder.jumps, jumpTarget[E]{labels: []string{stmt.Label.Text()}, breakTo: after})
	builder.statement(stmt.Statement)
	builder.popJump()
	builder.link(builder.cur, after)
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
	if !builder.cur.Reachable {
		return
	}
	name := ""
	if label != nil {
		name = label.Text()
	}
	for i := len(builder.jumps) - 1; i >= 0; i-- {
		target := builder.jumps[i]
		if name == "" {
			if !target.breakable {
				continue
			}
		} else if !slices.Contains(target.labels, name) {
			continue
		}
		builder.jumps[i].broken = true
		builder.link(builder.cur, target.breakTo)
		break
	}
	builder.makeUnreachable()
}

func (builder *Builder[E]) makeContinue(label *ast.Node) {
	if !builder.cur.Reachable {
		return
	}
	name := ""
	if label != nil {
		name = label.Text()
	}
	for i := len(builder.jumps) - 1; i >= 0; i-- {
		target := builder.jumps[i]
		if target.continueTo == nil {
			continue
		}
		if name == "" || slices.Contains(target.labels, name) {
			builder.loop(target.loop)
			builder.link(builder.cur, target.continueTo)
			break
		}
	}
	builder.makeUnreachable()
}

func (builder *Builder[E]) makeReturn() {
	if !builder.cur.Reachable {
		return
	}
	if i := builder.returnFrame(); i >= 0 {
		frame := builder.tryStack[i]
		frame.returnedAny = true
		builder.link(builder.cur, frame.finallyEntry)
	} else {
		builder.markFinal(builder.cur)
	}
	builder.makeUnreachable()
}

func (builder *Builder[E]) makeThrow() {
	if !builder.cur.Reachable {
		return
	}
	if i := builder.throwFrame(); i >= 0 {
		frame := builder.tryStack[i]
		frame.thrownAny = true
		builder.link(builder.cur, throwTarget(frame))
	} else {
		builder.markThrown(builder.cur)
	}
	builder.makeUnreachable()
}

// makeYield mirrors ESLint's handling of a suspended generator: the caller may
// resume it with `.return()` or `.throw()`, so both leaving paths are recorded
// before the normal continuation resumes in a fresh block.
func (builder *Builder[E]) makeYield() {
	if !builder.cur.Reachable {
		return
	}
	if i := builder.returnFrame(); i >= 0 {
		frame := builder.tryStack[i]
		frame.returnedAny = true
		builder.link(builder.cur, frame.finallyEntry)
	} else {
		builder.markFinal(builder.cur)
	}
	if i := builder.throwFrame(); i >= 0 {
		frame := builder.tryStack[i]
		frame.thrownAny = true
		builder.link(builder.cur, throwTarget(frame))
	} else {
		builder.markThrown(builder.cur)
	}
	next := builder.newBlock()
	builder.link(builder.cur, next)
	builder.enter(next)
}
