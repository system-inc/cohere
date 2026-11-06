package core

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// duplicateSuperFinder walks a constructor body in source order tracking whether a `super()` may
// already have run.
//
// A struct rather than threaded returns because the walk is genuinely stateful in one direction and
// branching in the other: a statement sets the bit for its successors, while two arms of a
// conditional each read the entry bit and neither sees the other's.
type duplicateSuperFinder struct {
	// mayHaveCalled records that at least one execution reaching this point has called `super()`.
	mayHaveCalled bool
	// duplicates is every call found while mayHaveCalled was already true, in source order.
	duplicates []*ast.Node
}

// walkStatement walks one statement, returning nothing and mutating mayHaveCalled in place.
//
// `insideLoop` is carried down so a `super()` in a loop body can be recognised without being
// credited as a duplicate of itself. It is the one place where the same call is both the first and
// the second, and upstream declines to report it; see the note on findDuplicateSuperCalls.
func (finder *duplicateSuperFinder) walkStatement(node *ast.Node, insideLoop bool) {
	if node == nil || isSeparateEvaluationContext(node) {
		return
	}

	switch node.Kind {
	case ast.KindBlock:
		finder.walkStatementList(node.AsBlock().Statements.Nodes, insideLoop)
		return

	case ast.KindIfStatement:
		statement := node.AsIfStatement()
		finder.walkStatement(statement.Expression, insideLoop)
		// Each arm reads the entry state and neither sees the other's result, which is what keeps
		// `if (a) super(); else super();` clean. The merge is a logical or because either arm having
		// called means a call may have happened.
		entry := finder.mayHaveCalled

		finder.mayHaveCalled = entry
		finder.walkStatement(statement.ThenStatement, insideLoop)
		thenCalled := finder.mayHaveCalled && !statementExits(statement.ThenStatement)

		finder.mayHaveCalled = entry
		finder.walkStatement(statement.ElseStatement, insideLoop)
		elseCalled := finder.mayHaveCalled && !statementExits(statement.ElseStatement)

		// An arm that leaves the constructor contributes nothing to what follows, because no
		// execution continues from it into the code after the `if`. That is what keeps
		// `for (const a of list) { if (a.foo) { super(a); return; } } super();` clean: the loop
		// body's call is on a path that returns, so it can never be the first of two.
		finder.mayHaveCalled = thenCalled || elseCalled
		return

	case ast.KindConditionalExpression:
		conditional := node.AsConditionalExpression()
		finder.walkStatement(conditional.Condition, insideLoop)
		entry := finder.mayHaveCalled

		finder.mayHaveCalled = entry
		finder.walkStatement(conditional.WhenTrue, insideLoop)
		whenTrueCalled := finder.mayHaveCalled

		finder.mayHaveCalled = entry
		finder.walkStatement(conditional.WhenFalse, insideLoop)
		whenFalseCalled := finder.mayHaveCalled

		finder.mayHaveCalled = whenTrueCalled || whenFalseCalled
		return

	case ast.KindSwitchStatement:
		statement := node.AsSwitchStatement()
		finder.walkStatement(statement.Expression, insideLoop)
		// Clauses are walked in order with the state carried through, because fallthrough is real
		// and `case 0: super(); default: super();` reaches both. That is upstream's fourth
		// duplicate case and it is the reason the clauses are not treated as alternatives the way
		// an if's arms are.
		// Clauses are walked in order, but only a clause that falls through hands its state to
		// the next one. A `break`, `return` or `throw` ends the clause, so the following one
		// starts from the switch's entry state instead.
		//
		// That is the whole difference between two corpus cases one word apart:
		// `switch (a) { case 0: super(); break; default: super(); }` is clean, because the `break`
		// means no execution reaches both calls, while
		// `switch (a) { case 0: super(); default: super(); }` reports a `duplicate`, because the
		// first clause falls into the second and one execution runs both.
		entry := finder.mayHaveCalled
		anyClauseCalled := entry
		for _, clause := range statement.CaseBlock.AsCaseBlock().Clauses.Nodes {
			statements := clause.AsCaseOrDefaultClause().Statements.Nodes
			finder.walkStatementList(statements, insideLoop)
			anyClauseCalled = anyClauseCalled || finder.mayHaveCalled
			if clauseBreaksOut(statements) {
				finder.mayHaveCalled = entry
			}
		}
		finder.mayHaveCalled = anyClauseCalled
		return

	case ast.KindWhileStatement, ast.KindDoStatement, ast.KindForStatement,
		ast.KindForInStatement, ast.KindForOfStatement:
		finder.walkLoop(node)
		return

	case ast.KindTryStatement:
		statement := node.AsTryStatement()
		entry := finder.mayHaveCalled

		finder.walkStatement(statement.TryBlock.AsNode(), insideLoop)
		tryCalled := finder.mayHaveCalled

		catchCalled := false
		if statement.CatchClause != nil {
			// The catch runs from the try's entry state, since an exception can arrive before
			// anything in the try body completed.
			finder.mayHaveCalled = entry
			finder.walkStatement(statement.CatchClause.AsCatchClause().Block.AsNode(), insideLoop)
			catchCalled = finder.mayHaveCalled
		}

		finder.mayHaveCalled = tryCalled || catchCalled
		if statement.FinallyBlock != nil {
			// The finally runs after either, so it sees whatever they left.
			finder.walkStatement(statement.FinallyBlock.AsNode(), insideLoop)
		}
		return

	case ast.KindCallExpression:
		if isSuperCall(node) {
			// Arguments evaluate before the call, so a `super()` nested in them is the earlier one.
			// `super(super())` is not legal, but `super(f(() => 0))` walks the same way and the
			// ordering costs nothing to get right.
			if arguments := node.AsCallExpression().Arguments; arguments != nil {
				for _, argument := range arguments.Nodes {
					finder.walkStatement(argument, insideLoop)
				}
			}
			if finder.mayHaveCalled && !insideLoop {
				finder.duplicates = append(finder.duplicates, node)
			}
			finder.mayHaveCalled = true
			return
		}
	}

	// Everything else contributes its children in source order with the state carried straight
	// through. That covers expression statements, returns, variable statements, and the logical
	// operators: `super() || super()` walks its left operand then its right, so the right is
	// reported, which is upstream's second duplicate case. Short circuiting does not save it,
	// because this is a *may* analysis and the non-short-circuiting execution exists.
	node.ForEachChild(func(child *ast.Node) bool {
		finder.walkStatement(child, insideLoop)
		return false
	})
}

// walkStatementList walks statements in order, stopping at the first unconditional exit.
//
// Stopping is what keeps an unreachable call from counting. `constructor() { return; super(); }`
// holds one `super()` and reports `missingAll`; a body holding two after a `return` reports the
// same rather than gaining a duplicate, because neither call is reachable and a duplicate is a
// second *reachable* call.
func (finder *duplicateSuperFinder) walkStatementList(statements []*ast.Node, insideLoop bool) {
	for _, statement := range statements {
		finder.walkStatement(statement, insideLoop)
		if superFlowOfStatement(statement).exits {
			return
		}
	}
}

// walkLoop walks a loop without letting its body count as a duplicate of itself.
//
// The head runs before the body and its state carries out, so it is walked normally. The body is
// walked with `insideLoop` set, which suppresses the duplicate report while still letting a call in
// it set the state for whatever follows the loop. That last part is what makes
// `while (a) super(); super();` report its trailing call, and it is also why the body cannot simply
// be skipped.
func (finder *duplicateSuperFinder) walkLoop(node *ast.Node) {
	// The body is not exempt from duplicate reporting; see the note below on what `insideLoopBody`
	// actually suppresses.
	const insideLoopBody = false

	var head []*ast.Node
	var body *ast.Node

	switch node.Kind {
	case ast.KindWhileStatement:
		statement := node.AsWhileStatement()
		head = []*ast.Node{statement.Expression}
		body = statement.Statement
	case ast.KindDoStatement:
		statement := node.AsDoStatement()
		head = []*ast.Node{statement.Expression}
		body = statement.Statement
	case ast.KindForStatement:
		statement := node.AsForStatement()
		head = []*ast.Node{statement.Initializer, statement.Condition, statement.Incrementor}
		body = statement.Statement
	case ast.KindForInStatement, ast.KindForOfStatement:
		statement := node.AsForInOrOfStatement()
		head = []*ast.Node{statement.Initializer, statement.Expression}
		body = statement.Statement
	}

	for _, part := range head {
		finder.walkStatement(part, false)
	}

	// The body is walked exactly once, from the state the head left, and a call inside it is
	// reported normally. `while (a) { super(); super(); }` runs both on one iteration, so the
	// second is a duplicate by the same reading as anywhere else, and oxc agrees: its duplicate
	// branch is gated on `super_call_spans.len() > 1` and a body with two calls holds two spans.
	//
	// Walking once is the whole of the divergence from ESLint. ESLint models the back edge, so a
	// *single* call in a loop body is a duplicate of itself on the next iteration and it reports
	// `while (a) super();` as `missingSome` and `duplicate` both. Oxc's span gate does not, because
	// one call is one span, and its four loop cases each record exactly one diagnostic. Not walking
	// the body a second time is what reproduces that, and ESLint's is the more useful finding.
	//
	// There is deliberately no merge back of the entry state here. It read as the load-bearing line
	// and it was subsumed: `mayHaveCalled` is monotonic, set to true and never back to false, so
	// after the body walk it already carries whatever the entry state held. A mutation sweep scored
	// `mayHaveCalled = entry` against `mayHaveCalled = mayHaveCalled || entry` as a survivor and no
	// fixture could have caught it, because the two expressions cannot differ. Deleted rather than
	// tested, so the next reader does not spend the same half hour proving the same thing.
	finder.walkStatement(body, insideLoopBody)
}
