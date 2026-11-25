package core

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// superScanner walks a constructor body in source order tracking whether `super()` has definitely
// been called yet.
//
// It is a struct rather than a pair of free functions because the walk is genuinely stateful: the
// "have we called super yet" bit is set by one statement and read by a later sibling, and threading
// it through returns would make every recursive call carry two answers instead of one.
type superScanner struct {
	// superCalled records that every path reaching the current point has called `super()`.
	//
	// Once true it never goes back to false. That is correct rather than a shortcut: statements are
	// visited in source order, so a later statement is reached only after the earlier ones ran, and
	// nothing can un-call a call.
	superCalled bool
}

// scanStatements walks a statement looking for the first violation, returning true when it finds
// one.
//
// It returns early on the first violation rather than collecting them, because the report is
// per-constructor. Collecting every offending node and then reporting once would do strictly more
// work for the same output.
func (scanner *superScanner) scanStatements(node *ast.Node) bool {
	if node == nil {
		return false
	}

	// A nested function, arrow, class, or accessor is a different evaluation context: its body does
	// not run at the point it is written, so a `this` inside it is not reached before `super()`.
	// Six pass cases rest on this, and the walk must stop here rather than merely decline to
	// report, since a `super()` written inside a nested function must not credit the outer
	// constructor either.
	if isSeparateEvaluationContext(node) {
		return false
	}

	// There is deliberately no case for a class field initializer here, and it is worth saying why
	// rather than leaving the absence to be read as an oversight.
	//
	// Upstream has four pass cases about field initializers, so the behaviour is required. It is
	// already correct without a check, because the only route from a constructor to any field
	// declaration runs through a class node, and `isSeparateEvaluationContext` stops the walk at
	// every one of those. This port did carry an explicit `KindPropertyDeclaration` guard, and a
	// mutation sweep scored it as a survivor. Instrumenting the walk on six inputs covering a field
	// in the constructor's own class, in a nested class declaration, and in a nested class
	// expression showed the scanner receives a field declaration zero times in all six. The branch
	// was unreachable, so the guard was deleted rather than given a fixture, which would have been
	// a test asserting nothing.

	// The expression carrying `super()` is where credit is granted, and it has to be checked before
	// the operands are walked so that `super(this.c)` reports the argument. Handled inside
	// scanExpression.
	if isExpressionNode(node) {
		return scanner.scanExpression(node)
	}

	switch node.Kind {
	case ast.KindIfStatement:
		statement := node.AsIfStatement()
		// The test runs unconditionally, so a violation or a `super()` in it counts in full.
		if scanner.scanStatements(statement.Expression) {
			return true
		}
		// Each arm starts from the state the test left, and neither arm's result is visible to the
		// other. `if (a) super(); else super();` is a pass case and `if (a) super();` is a fail
		// case, and the only difference between them is whether both arms answered yes.
		entryState := scanner.superCalled

		thenScanner := &superScanner{superCalled: entryState}
		if thenScanner.scanStatements(statement.ThenStatement) {
			return true
		}

		elseScanner := &superScanner{superCalled: entryState}
		if statement.ElseStatement != nil {
			if elseScanner.scanStatements(statement.ElseStatement) {
				return true
			}
		}

		// An absent else is an arm that runs nothing, so it keeps the entry state: `if (foo) {
		// super(); }` entered without a call leaves the state false and `this.a()` after it
		// reports, while an `if` entered after `super()` leaves it true. This read `then && else !=
		// nil && else` once, which reset the state to false after every else-less `if` whatever
		// held on entry, so a second such `if` after `super()` reported its `this`: 9 false findings
		// on api-phi-health (#7t7c7g7).
		scanner.superCalled = thenScanner.superCalled && elseScanner.superCalled
		return false

	case ast.KindTryStatement:
		return scanner.scanTry(node.AsTryStatement())

	case ast.KindBlock:
		for _, statement := range node.AsBlock().Statements.Nodes {
			if scanner.scanStatements(statement) {
				return true
			}
			// A statement after one that definitely leaves the function is unreachable, so a
			// `this` in it never evaluates and never throws. Upstream agrees by way of its CFG:
			// ESLint's `isCalled` answers true for any unreachable segment, and oxc's edge filter
			// maps `EdgeType::Unreachable` to `No`. Two pass cases pin it, one per class kind,
			// and they exist because of eslint/eslint#5894.
			if bodyDefinitelyExits(statement) {
				return false
			}
		}
		return false

	case ast.KindWhileStatement, ast.KindForStatement,
		ast.KindForInStatement, ast.KindForOfStatement:
		return scanner.scanLoop(node)

	case ast.KindDoStatement:
		// A do-while body runs at least once before its test, so a `super()` in it is guaranteed
		// and the credit is kept. Both upstreams agree, measured rather than reasoned: this port
		// first grouped it with the other loops and both `oxlint` and ESLint disagreed on
		// `do { super(); } while (foo); this.a();`.
		//
		// The `while` test is scanned after the body and with the body's credit, since it runs
		// after the first iteration.
		statement := node.AsDoStatement()
		if scanner.scanStatements(statement.Statement) {
			return true
		}
		return scanner.scanStatements(statement.Expression)

	case ast.KindSwitchStatement:
		return scanner.scanSwitch(node.AsSwitchStatement())

	case ast.KindLabeledStatement:
		return scanner.scanStatements(node.AsLabeledStatement().Statement)
	}

	// Everything else is walked child by child in source order with the state carried straight
	// through. That covers expression statements, variable statements, returns, throws, and
	// anything the corpus does not name, and it is the right default: a construct nobody thought
	// about contributes its children's verdicts and grants no credit of its own.
	violated := false
	node.ForEachChild(func(child *ast.Node) bool {
		if scanner.scanStatements(child) {
			violated = true
			return true
		}
		return false
	})
	return violated
}

// scanLoop walks a loop, crediting nothing that happens inside the body.
//
// The body may run zero times, so a `super()` in it cannot be relied on, which is why
// `while (foo) { super(); } this.a();` is a fail case. Violations inside the body still count,
// because the body may also run once: `while (foo) { this.a(); super(); }` is also a fail case, and
// those two are upstream's matched pair proving the asymmetry is deliberate rather than a bug.
//
// The initializer and the test of a `for` are different: they run at least once and before the
// body, so they are scanned with the outer state and their credit is kept. Upstream's
// `for (let i = 0; i < 0; i++); this;` after a `super()` is the pass case that needs this to not
// discard the credit already held.
func (scanner *superScanner) scanLoop(node *ast.Node) bool {
	var head []*ast.Node
	var body *ast.Node

	switch node.Kind {
	case ast.KindWhileStatement:
		statement := node.AsWhileStatement()
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
		if scanner.scanStatements(part) {
			return true
		}
	}

	// The body runs against the state the head left, but whatever it decides is discarded.
	bodyScanner := &superScanner{superCalled: scanner.superCalled}
	return bodyScanner.scanStatements(body)
}

// scanTry walks a try statement, granting credit only where a throw cannot route around it.
//
// Three shapes and the corpus separates all three:
//
//	try { super(); } finally {}                 passes: nothing can skip the try's super()
//	try { super(); } catch (err) { } this.a;    fails:  the catch swallows a throwing super()
//	try { super(); } finally { this.a; }        fails:  the finally runs mid-throw, this unbound
//
// The finally block is scanned from the *entry* state rather than the try's exit state, which is
// what makes the third case report. A finally runs on the exception path too, and on that path the
// try body may have thrown partway through `super()` itself, so nothing the try achieved can be
// assumed. That single choice is the difference between the second fail case and the first pass
// case, and it is the sharpest thing in this rule.
func (scanner *superScanner) scanTry(statement *ast.TryStatement) bool {
	entryState := scanner.superCalled

	tryScanner := &superScanner{superCalled: entryState}
	if tryScanner.scanStatements(statement.TryBlock) {
		return true
	}

	if statement.CatchClause != nil {
		// The catch runs from the entry state: an exception can arrive from anywhere in the try
		// body, including from inside `super()` itself.
		catchScanner := &superScanner{superCalled: entryState}
		if catchScanner.scanStatements(statement.CatchClause) {
			return true
		}
	}

	if statement.FinallyBlock != nil {
		finallyScanner := &superScanner{superCalled: entryState}
		if finallyScanner.scanStatements(statement.FinallyBlock) {
			return true
		}
	}

	// Credit survives only when the try body called `super()` and no catch could have skipped it.
	// A catch clause means the normal path is not the only one out, so nothing is guaranteed.
	scanner.superCalled = tryScanner.superCalled && statement.CatchClause == nil
	return false
}

// scanSwitch walks a switch, crediting it only when every way out of it has called `super()`.
//
// No case in oxc's corpus exercises a switch, so the semantics were taken from both upstreams
// directly rather than inferred: ten invented inputs run through `oxlint` and through ESLint's
// Linter API, which agreed on all ten. This port's first version credited nothing here, and both
// upstreams disagreed, so a limit that was written down as deliberate turned out to be wrong. The
// measured rule is:
//
//	switch (x) { case 1: super(); break; default: super(); }   credits
//	switch (x) { case 1: super(); break; }                     does not: no default
//	switch (x) { case 1: super(); break; case 2: bar(); break;
//	             default: super(); }                           does not: case 2 escapes uncalled
//	switch (x) { case 1: case 2: super(); break;
//	             default: super(); }                           credits: case 1 falls through
//	switch (x) { case 1: super(); default: bar(); }            does not: falls into an uncalled default
//
// Two properties carry it. A `default` must exist, because without one the switch can be skipped
// entirely, which is the same reason an `if` with no else does not credit. And fallthrough is real:
// an empty clause contributes nothing of its own and inherits whatever the clause below decides,
// which is why the state is carried forward between clauses rather than reset at each one.
func (scanner *superScanner) scanSwitch(statement *ast.SwitchStatement) bool {
	if scanner.scanStatements(statement.Expression) {
		return true
	}

	entryState := scanner.superCalled
	if statement.CaseBlock == nil {
		return false
	}
	clauses := statement.CaseBlock.AsCaseBlock().Clauses.Nodes

	hasDefault := false
	// everyExitCalled tracks whether every clause that can leave the switch has called `super()`.
	// It starts true and is falsified by the first clause that escapes without one, which is the
	// standard shape for a must-analysis over a list.
	everyExitCalled := true
	// clauseState is what the previous clause hands down when it falls through. It starts true
	// meaning "no predecessor constrains this", so the first clause reduces to the entry state,
	// and a clause that exits resets it to true for the same reason.
	clauseState := true

	for _, clause := range clauses {
		if clause.Kind == ast.KindDefaultClause {
			hasDefault = true
		}

		// A clause is reachable two ways: by matching its own test, and by falling through from
		// the clause above. It can only be credited when *both* routes have called, so the two
		// states are combined pessimistically rather than the fallthrough state simply inherited.
		//
		// That distinction is the whole of `case 1: super(); default: bar();`, which both
		// upstreams report. Inheriting the fallthrough state alone credits the default with the
		// `super()` from `case 1`, but `default:` is also entered directly on any other value,
		// and on that route nothing has been called. An earlier version of this loop carried the
		// state straight through and got that case wrong while passing every other switch case.
		clauseScanner := &superScanner{superCalled: clauseState && entryState}

		// One accessor covers both clause kinds; a default clause simply has a nil Expression.
		details := clause.AsCaseOrDefaultClause()
		if details == nil {
			continue
		}
		// The case test runs against the switch's own state rather than the clause's, since it is
		// evaluated during dispatch and before any clause body.
		if scanner.scanStatements(details.Expression) {
			return true
		}

		var exits bool
		statements := details.Statements.Nodes

		for _, inner := range statements {
			if clauseScanner.scanStatements(inner) {
				return true
			}
			if clauseExits(inner) {
				exits = true
				break
			}
		}

		if exits {
			// This clause leaves the switch here, so its verdict is final and it cannot hand
			// anything to the clause below.
			everyExitCalled = everyExitCalled && clauseScanner.superCalled
			clauseState = true
			continue
		}

		// No exit: control falls into the next clause carrying this state. The last clause in the
		// list falls out of the switch instead, so its verdict counts.
		clauseState = clauseScanner.superCalled
		if clause == clauses[len(clauses)-1] {
			everyExitCalled = everyExitCalled && clauseScanner.superCalled
		}
	}

	scanner.superCalled = hasDefault && everyExitCalled
	return false
}

// scanExpression walks an expression, handling the two shapes that are about `super` itself.
//
// Everything else falls through to the generic child walk, which is what makes `super(a(b(this.c)))`
// report: the arguments are ordinary children and the `this` inside them is found the ordinary way.
func (scanner *superScanner) scanExpression(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindCallExpression:
		call := node.AsCallExpression()
		if call.Expression != nil && call.Expression.Kind == ast.KindSuperKeyword {
			// The arguments evaluate before the call, so a `this` in them is reached while `this`
			// is still unbound. Six fail cases rest on this and they are the reason credit is
			// granted after the arguments are walked rather than before.
			violated := false
			if call.Arguments != nil {
				for _, argument := range call.Arguments.Nodes {
					if scanner.scanStatements(argument) {
						violated = true
						break
					}
				}
			}
			if violated {
				return true
			}
			scanner.superCalled = true
			return false
		}

	case ast.KindConditionalExpression:
		// Neither arm is guaranteed, so neither can grant credit, exactly as with an `if` that has
		// only one arm. The condition itself runs unconditionally and keeps its credit.
		conditional := node.AsConditionalExpression()
		if scanner.scanStatements(conditional.Condition) {
			return true
		}
		entryState := scanner.superCalled
		whenTrueScanner := &superScanner{superCalled: entryState}
		if whenTrueScanner.scanStatements(conditional.WhenTrue) {
			return true
		}
		whenFalseScanner := &superScanner{superCalled: entryState}
		if whenFalseScanner.scanStatements(conditional.WhenFalse) {
			return true
		}
		// Both arms calling would be sound to credit, but `super()` twice on one construction is
		// itself a ReferenceError, so no correct program reaches that state and crediting it would
		// buy nothing. Left uncredited to match the treatment of every other conditional here.
		scanner.superCalled = entryState
		return false

	case ast.KindBinaryExpression:
		binary := node.AsBinaryExpression()
		if binary.OperatorToken == nil {
			break
		}
		if isShortCircuitingOperator(binary.OperatorToken.Kind) {
			// `foo && super()` may never evaluate its right side. This is the plain-operator twin
			// of the logical assignments below and it reaches a different branch, so it needs its
			// own case: upstream reports `foo && super(); this.a();` and so does this.
			if scanner.scanStatements(binary.Left) {
				return true
			}
			rightScanner := &superScanner{superCalled: scanner.superCalled}
			return rightScanner.scanStatements(binary.Right)
		}
		if isShortCircuitingAssignment(binary.OperatorToken.Kind) {
			// `foo &&= super().a` may never evaluate its right side, so nothing in it can be
			// credited. Three fail cases, matched against four pass cases using `=`, `+=`, `|=`
			// and `&=`, where the right side always evaluates. That matched set is why the
			// operator has to be inspected rather than every assignment treated alike.
			if scanner.scanStatements(binary.Left) {
				return true
			}
			rightScanner := &superScanner{superCalled: scanner.superCalled}
			return rightScanner.scanStatements(binary.Right)
		}

	case ast.KindThisKeyword:
		return !scanner.superCalled

	case ast.KindSuperKeyword:
		// A bare `super` reaching here is not in callee position, since the call case above
		// consumes that one without descending. So this is `super.x` or `super[x]`, both of which
		// need the `this` binding for their home-object lookup and are violations for the same
		// reason `this` is.
		return !scanner.superCalled
	}

	violated := false
	node.ForEachChild(func(child *ast.Node) bool {
		if scanner.scanStatements(child) {
			violated = true
			return true
		}
		return false
	})
	return violated
}
