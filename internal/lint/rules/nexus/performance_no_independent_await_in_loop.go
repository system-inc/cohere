package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// performanceNoIndependentAwaitInLoopText is the rule's message, whose wording lives in
// `policy/messages/performance-no-independent-await-in-loop.json`.
var performanceNoIndependentAwaitInLoopText = policy.MessageOf(
	"nexus/performance-no-independent-await-in-loop",
	"independentAwaitInLoop",
)

func messagePerformanceNoIndependentAwaitInLoop() rule.Message {
	return rule.Message{
		Id:          performanceNoIndependentAwaitInLoopText.Id,
		Description: performanceNoIndependentAwaitInLoopText.Render(nil),
	}
}

// PerformanceNoIndependentAwaitInLoop reports a loop over a collection whose iterations wait on
// each other for no reason the code can show.
//
//	invalid: for (const tree of trees) { results.push(await checkTree(tree)); }
//	invalid: for (const adapter of this.adapters) { try { results.push(await adapter.fetch()); }
//	         catch (error) { results.push(failed(error)); } }
//	invalid: for (let index = 0; index < files.length; index++) { await upload(files[index]); }
//	valid:   while (hasMore) { const page = await list(cursor); cursor = page.next; }
//	valid:   for (const page of pages) { cursor = await fetchPage(page, cursor); }
//	valid:   for (const section of sections) { console.log(section.name); await printTasks(section); }
//	valid:   for (const host of hosts) { if (await isUp(host)) return host; }
//	valid:   for (const item of items) { await sleep(250); await send(item); }
//
// # Why this exists rather than `no-await-in-loop`
//
// Upstream's rule reports every await inside every loop: 379 findings on ahra, and Kirk's sample put
// three quarters of them as deliberately sequential (pagination, polling, retries, ordered output).
// A rule that is wrong three times in four teaches its reader to suppress it unread. This one is
// the same question asked with the program in hand: it reports only when the loop's iterations can
// be shown not to depend on each other, so the finding is nearly always a real `Promise.all`.
//
// # When it fires, and every condition must hold
//
// The loop iterates a collection. A `for...of` that is not `for await`, or the indexed form
// `for (let i = 0; i < xs.length; i++)` whose body never writes `i`. `while`, `do`, `for...in`, a
// `for await`, and any other `for` header (an attempt counter, a cursor, `;;`) never fire, because
// those are the shapes of retry, polling and pagination.
//
// The loop itself holds an await. An await inside a nested loop belongs to that loop and is judged
// there, so a finding names the innermost loop that waits, once, however many awaits it holds. An
// await inside a nested function does not count, because the function is not called by being
// written. An await on the for-of iterable runs once and does not count either.
//
// No loop-carried state. Every identifier in the body is resolved through the checker, and a
// binding is outer when none of its declarations lies inside the loop. Then:
//
//   - a write to an outer binding anywhere in the body (assignment, compound assignment, `++`,
//     destructuring) is silence: that is a cursor, an attempt count or an accumulator;
//   - a `var` declared in the body is silence, because it is function-scoped and so outlives the
//     iteration that wrote it;
//   - in the indexed form, a write to the index in the body is silence;
//   - a compound assignment or `++` on an outer property (`summary.total += n`) is silence;
//   - a call that reads and consumes outer state (`pop`, `shift`, `splice`, `next`, `read`, `sort`,
//     `reverse` on an outer receiver) is silence.
//
// Collecting results is allowed, because it is what `Promise.all` returns: `push`, `unshift`, `set`,
// `add`, `append`, `delete` on an outer receiver, `Object.assign(outer, ...)`, a plain `=` into an
// outer property or element, and `delete outer[key]` are sinks. A sink is allowed only while the
// collection it fills is not read anywhere else in the body or by the loop's own iterable: once
// `results.length` names the archive, or `seen.has(key)` decides what runs, the iterations depend on
// each other. Paths are compared as property chains, so filling `this.cache` does not conflict with
// calling `this.fetch`, while reading `cursor` whole does conflict with writing `cursor.after`.
//
// `Object.assign(accumulator, await load())` is a sink on the same terms. Its result depends on
// order only where two loads return the same key, and `Promise.all` keeps that order too, so
// assigning the settled results in sequence reproduces it exactly. That is the
// `AhraOsCommandLineInterface` boot loader, and it reports.
//
// No exit on an awaited result. A `break`, a `return`, a `throw` not caught inside the body, or a
// labeled `continue` to an outer loop leaves the loop, and it is silence when its guard reads a
// value derived from an await: the `if` or `switch` it sits under, the test of an inner loop it sits
// in, a `catch` whose `try` awaits, a `try` that awaits before the exit and has a `catch`, an earlier
// statement that jumps on such a value, or the exit's own expression. "Derived" is a fixpoint over
// the body's own declarations and assignments, and a catch parameter whose `try` awaits counts. An
// exit whose guard reads no awaited value is not loop-carried (a validation `throw` before the first
// await), so it does not silence the rule. A plain `continue`, and a `try/catch` whose catch records
// the failure and moves on, are fine: that is the FinanceConnections shape, and it reports.
//
// No ordered side effect. Matched by name, anywhere in the body including callbacks, because these
// are about what the reader sees and a callback runs inside the iteration:
//
//   - `console.*`, `process.stdout.*`, `process.stderr.*`;
//   - `setTimeout`, `setInterval`, `setImmediate`, and any call whose name starts with `sleep`,
//     `delay`, `wait`, `pause`, `throttle`, `backoff` or `rateLimit`, which reads as pacing or
//     polling;
//   - any call whose name, or whose receiver's last segment, contains `progress` or `spinner`;
//   - `write`, `writeSync`, `appendFile`, `appendFileSync`, which write into one stream or file in
//     order;
//   - `log`, `info`, `warn`, `error`, `debug` or `trace` on a receiver whose last segment contains
//     `log` (`logger.info`, `this.log.warn`), and a bare `log(...)`;
//   - a `yield`, which hands results out in order.
//
// Inside a `catch` in the body, and one level into every function the body calls (resolved through
// the checker, imports followed), the list narrows to progress output and explicit pacing:
// `console.error`/`warn`/`debug`/`trace`, `process.stderr`, a `log`-receiver's `error`/`warn`, and
// `setTimeout` do not count there, and a callee's own `catch` blocks are not read. A failure report
// is not output whose order the reader follows, and a callee's `setTimeout` is a request timeout.
//
// These are names, not types, and the list is the whole of it: an ordered effect spelled some
// other way (a shared browser page, a migration runner executing statements in turn) is not seen
// and reports. The rule doc carries the false positives that measured on ahra.
//
// # Why syntax rather than the control-flow graph
//
// The flow graph answers "does this block reach that one", and the questions here are narrower:
// which bindings outlive an iteration (a declaration-position test on resolved symbols), and which
// conditions guard an exit (the statements between the exit and the loop body, plus earlier
// siblings that jump). Both are answered by walking up from the node, conservatively: an earlier
// sibling that holds any tainted jump counts, even when that jump targets something inside the
// sibling. Over-approximating there costs a finding, never a false one.
//
// # Where the finding points
//
// At the loop header, from `for` to the closing parenthesis, so the line a suppression goes above is
// the loop rather than one of its awaits.
//
// # No fixer
//
// The rewrite is `Promise.all`, a bounded pool, or a deliberate suppression, and which one depends
// on how large the collection is and what the far side tolerates, which the syntax cannot say.
var PerformanceNoIndependentAwaitInLoop = rule.Rule[rule.NoOptions]{
	Name: "nexus/performance-no-independent-await-in-loop",

	// Every identifier in the loop body is resolved to its declaration to decide whether it outlives
	// an iteration, and the binder that answers that runs only with the program.
	NeedsTypeChecker: true,

	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		if ruleContext.TypeChecker == nil {
			return nil
		}

		check := func(node *ast.Node) {
			analysis := newIndependentAwaitLoopAnalysis(ruleContext, node)
			if analysis == nil || !analysis.isIndependent() {
				return
			}
			ruleContext.ReportRange(
				independentAwaitLoopHeaderRange(ruleContext.SourceFile, node, analysis.body),
				messagePerformanceNoIndependentAwaitInLoop(),
			)
		}

		return rule.Listeners{
			ast.KindForOfStatement: check,
			ast.KindForStatement:   check,
		}
	},
}

// independentAwaitLoopPath is a property chain rooted at an outer binding or at `this`.
type independentAwaitLoopPath struct {
	segments []string
	root     *ast.Node
}

func newIndependentAwaitLoopAnalysis(ruleContext rule.Context, loop *ast.Node) *independentAwaitLoopAnalysis {
	analysis := &independentAwaitLoopAnalysis{
		ruleContext:    ruleContext,
		loop:           loop,
		taintedSymbols: map[*ast.Symbol]bool{},
		orderedBodies:  map[*ast.Node]bool{},
	}

	switch loop.Kind {
	case ast.KindForOfStatement:
		statement := loop.AsForInOrOfStatement()
		// `for await` is asynchronous iteration: the source hands out the next item when it is ready,
		// which is the deliberate sequential case rather than the mistake.
		if statement.AwaitModifier != nil {
			return nil
		}
		analysis.body = statement.Statement
		analysis.collection = statement.Expression

	case ast.KindForStatement:
		statement := loop.AsForStatement()
		indexName, collection := independentAwaitLoopIndexedShape(statement)
		if indexName == nil {
			return nil
		}
		analysis.indexSymbol = ruleContext.TypeChecker.GetSymbolAtLocation(indexName)
		if analysis.indexSymbol == nil {
			return nil
		}
		analysis.body = statement.Statement
		analysis.collection = collection

	default:
		return nil
	}

	if analysis.body == nil {
		return nil
	}
	return analysis
}

// independentAwaitLoopIndexedShape recognizes `for (let i = <start>; i < xs.length; i++)` and
// answers the counter's name node and `xs`, or nil when the header is anything else.
//
// The `.length` bound is the whole point. An attempt counter (`attempt <= maximumAttempts`), a poll
// budget (`attempt < 60 && state === 'PROCESSING'`) and an infinite `for (;;)` are the same syntax
// with a different meaning, and the bound is what tells a walk over a collection from a retry.
func independentAwaitLoopIndexedShape(statement *ast.ForStatement) (*ast.Node, *ast.Node) {
	if statement.Initializer == nil || statement.Initializer.Kind != ast.KindVariableDeclarationList ||
		statement.Condition == nil || statement.Incrementor == nil {
		return nil, nil
	}
	declarations := statement.Initializer.AsVariableDeclarationList().Declarations
	if declarations == nil || len(declarations.Nodes) != 1 {
		return nil, nil
	}
	declaration := declarations.Nodes[0].AsVariableDeclaration()
	indexName := declaration.Name()
	if indexName == nil || indexName.Kind != ast.KindIdentifier || declaration.Initializer == nil {
		return nil, nil
	}

	condition := ast.SkipParentheses(statement.Condition)
	if condition.Kind != ast.KindBinaryExpression {
		return nil, nil
	}
	comparison := condition.AsBinaryExpression()
	if comparison.OperatorToken.Kind != ast.KindLessThanToken {
		return nil, nil
	}
	left := ast.SkipParentheses(comparison.Left)
	right := ast.SkipParentheses(comparison.Right)
	if left.Kind != ast.KindIdentifier || left.Text() != indexName.Text() ||
		right.Kind != ast.KindPropertyAccessExpression ||
		right.AsPropertyAccessExpression().Name().Text() != "length" {
		return nil, nil
	}

	if !independentAwaitLoopIncrementsByOne(ast.SkipParentheses(statement.Incrementor), indexName.Text()) {
		return nil, nil
	}
	return indexName, right.AsPropertyAccessExpression().Expression
}

// independentAwaitLoopIncrementsByOne accepts `i++`, `++i` and `i += 1`, the three spellings of
// stepping through a collection one element at a time.
func independentAwaitLoopIncrementsByOne(incrementor *ast.Node, indexName string) bool {
	isIndex := func(node *ast.Node) bool {
		unwrapped := ast.SkipParentheses(node)
		return unwrapped.Kind == ast.KindIdentifier && unwrapped.Text() == indexName
	}
	switch incrementor.Kind {
	case ast.KindPostfixUnaryExpression:
		unary := incrementor.AsPostfixUnaryExpression()
		return unary.Operator == ast.KindPlusPlusToken && isIndex(unary.Operand)
	case ast.KindPrefixUnaryExpression:
		unary := incrementor.AsPrefixUnaryExpression()
		return unary.Operator == ast.KindPlusPlusToken && isIndex(unary.Operand)
	case ast.KindBinaryExpression:
		binary := incrementor.AsBinaryExpression()
		right := ast.SkipParentheses(binary.Right)
		return binary.OperatorToken.Kind == ast.KindPlusEqualsToken && isIndex(binary.Left) &&
			right.Kind == ast.KindNumericLiteral && right.Text() == "1"
	}
	return false
}

// independentAwaitLoopIsBoundary answers whether a walk stops here because the code below is not
// run by being written: a function of any form, or a class static block.
func independentAwaitLoopIsBoundary(node *ast.Node) bool {
	return ast.IsFunctionLike(node) || node.Kind == ast.KindClassStaticBlockDeclaration
}

// independentAwaitLoopFunctionBody answers the body a declaration runs when called, or nil: a
// function or method directly, or a variable or class field initialized with a function.
func independentAwaitLoopFunctionBody(declaration *ast.Node) *ast.Node {
	if sourceFile := ast.GetSourceFileOfNode(declaration); sourceFile == nil || sourceFile.IsDeclarationFile {
		return nil
	}
	switch declaration.Kind {
	case ast.KindFunctionDeclaration, ast.KindMethodDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
		return declaration.Body()
	case ast.KindVariableDeclaration, ast.KindPropertyDeclaration:
		var initializer *ast.Node
		if declaration.Kind == ast.KindVariableDeclaration {
			initializer = declaration.AsVariableDeclaration().Initializer
		} else {
			initializer = declaration.AsPropertyDeclaration().Initializer
		}
		if initializer == nil {
			return nil
		}
		initializer = independentAwaitLoopSkipWrappers(initializer)
		if initializer.Kind == ast.KindFunctionExpression || initializer.Kind == ast.KindArrowFunction {
			return initializer.Body()
		}
	}
	return nil
}

// independentAwaitLoopCalleeQuietConsoleMethods are the console methods a callee uses to report a
// problem rather than to show progress, so they do not make its caller's loop ordered.
var independentAwaitLoopCalleeQuietConsoleMethods = map[string]bool{
	"error": true, "warn": true, "debug": true, "trace": true, "assert": true,
}

// independentAwaitLoopIsCalleeOrderedCall is the narrower question asked inside a called function:
// progress output and explicit pacing only.
//
// Two exclusions from the loop-body list. A callee's `console.error` and `console.warn` report a
// failure (`sendOnMeta`'s advisory safety warning is the measured case), and a callee's `setTimeout`
// is almost always a request timeout rather than a sleep (`FrameTvApi.sendRequest` arms one per
// request), so neither says the caller's loop must run in order.
func independentAwaitLoopIsCalleeOrderedCall(callee *ast.Node) bool {
	unwrapped := independentAwaitLoopSkipWrappers(callee)
	switch unwrapped.Kind {
	case ast.KindIdentifier:
		if name := unwrapped.Text(); name == "setTimeout" || name == "setInterval" || name == "setImmediate" {
			return false
		}
	case ast.KindPropertyAccessExpression:
		access := unwrapped.AsPropertyAccessExpression()
		receiver := independentAwaitLoopSegments(access.Expression)
		method := access.Name().Text()
		if len(receiver) > 0 && receiver[0] == "console" && independentAwaitLoopCalleeQuietConsoleMethods[method] {
			return false
		}
		if len(receiver) >= 2 && receiver[0] == "process" && receiver[1] == "stderr" {
			return false
		}
		if len(receiver) > 0 && strings.Contains(strings.ToLower(receiver[len(receiver)-1]), "log") &&
			(method == "error" || method == "warn" || method == "debug" || method == "trace") {
			return false
		}
		if method == "setTimeout" || method == "setInterval" || method == "setImmediate" {
			return false
		}
	}
	return independentAwaitLoopIsOrderedCall(unwrapped)
}

var independentAwaitLoopPacingPrefixes = []string{
	"sleep",
	"delay",
	"wait",
	"pause",
	"throttle",
	"backoff",
	"ratelimit",
	"print",
}

var independentAwaitLoopStreamWrites = map[string]bool{
	"write": true, "writeSync": true, "appendFile": true, "appendFileSync": true,
}

var independentAwaitLoopLogMethods = map[string]bool{
	"log": true, "info": true, "warn": true, "error": true, "debug": true, "trace": true,
}

// independentAwaitLoopIsOrderedCall answers whether a call's callee names an effect whose order the
// reader can see. Names only; the doc comment on the rule carries the whole list.
func independentAwaitLoopIsOrderedCall(callee *ast.Node) bool {
	unwrapped := independentAwaitLoopSkipWrappers(callee)
	name := ""
	var receiver []string
	switch unwrapped.Kind {
	case ast.KindIdentifier:
		name = unwrapped.Text()
	case ast.KindPropertyAccessExpression:
		access := unwrapped.AsPropertyAccessExpression()
		name = access.Name().Text()
		receiver = independentAwaitLoopSegments(access.Expression)
	default:
		return false
	}

	if len(receiver) > 0 && receiver[0] == "console" {
		return true
	}
	if len(receiver) >= 2 && receiver[0] == "process" && (receiver[1] == "stdout" || receiver[1] == "stderr") {
		return true
	}
	if name == "setTimeout" || name == "setInterval" || name == "setImmediate" {
		return true
	}

	lowerName := strings.ToLower(name)
	for _, prefix := range independentAwaitLoopPacingPrefixes {
		if strings.HasPrefix(lowerName, prefix) {
			return true
		}
	}
	if strings.Contains(lowerName, "progress") || strings.Contains(lowerName, "spinner") {
		return true
	}
	if independentAwaitLoopStreamWrites[name] {
		return true
	}

	lastReceiver := ""
	if len(receiver) > 0 {
		lastReceiver = strings.ToLower(receiver[len(receiver)-1])
	}
	if strings.Contains(lastReceiver, "progress") || strings.Contains(lastReceiver, "spinner") {
		return true
	}
	if independentAwaitLoopLogMethods[name] && strings.Contains(lastReceiver, "log") {
		return true
	}
	return unwrapped.Kind == ast.KindIdentifier && name == "log"
}

// independentAwaitLoopSkipWrappers removes parentheses and non-null assertions, which change
// nothing about which value an expression names.
func independentAwaitLoopSkipWrappers(node *ast.Node) *ast.Node {
	current := node
	for current != nil {
		switch current.Kind {
		case ast.KindParenthesizedExpression:
			current = current.AsParenthesizedExpression().Expression
		case ast.KindNonNullExpression:
			current = current.AsNonNullExpression().Expression
		default:
			return current
		}
	}
	return current
}

// independentAwaitLoopSegments spells a property chain as its names, `this.cache.entries` as
// `this`, `cache`, `entries`, or nil when the expression is not a plain chain.
func independentAwaitLoopSegments(node *ast.Node) []string {
	unwrapped := independentAwaitLoopSkipWrappers(node)
	switch unwrapped.Kind {
	case ast.KindIdentifier:
		return []string{unwrapped.Text()}
	case ast.KindThisKeyword:
		return []string{"this"}
	case ast.KindPropertyAccessExpression:
		access := unwrapped.AsPropertyAccessExpression()
		base := independentAwaitLoopSegments(access.Expression)
		if base == nil {
			return nil
		}
		return append(append([]string{}, base...), access.Name().Text())
	}
	return nil
}

// independentAwaitLoopRoot answers the identifier or `this` a property chain starts from.
func independentAwaitLoopRoot(node *ast.Node) *ast.Node {
	unwrapped := independentAwaitLoopSkipWrappers(node)
	switch unwrapped.Kind {
	case ast.KindIdentifier, ast.KindThisKeyword:
		return unwrapped
	case ast.KindPropertyAccessExpression:
		return independentAwaitLoopRoot(unwrapped.AsPropertyAccessExpression().Expression)
	}
	return nil
}

// independentAwaitLoopSinkMethods fill a collection without reading it back, which is what
// `Promise.all` returns anyway.
var independentAwaitLoopSinkMethods = map[string]bool{
	"push": true, "unshift": true, "set": true, "add": true, "append": true, "delete": true,
}

// independentAwaitLoopIsSinkMethod answers whether a method fills its receiver: one of the exact
// names above, or a collection-building verb followed by a capitalized noun (`addPage`,
// `appendChild`, `insertRow`). The prefix form exists because a shared document built page by page
// is the same shape as a pushed array, and the one ahra instance (`document.addPage` per slide,
// with `document` also handed to the await) read as independent until it was a sink whose receiver
// the body reads back. `address` and `inserted` stay out because the letter after the verb must be
// upper case.
func independentAwaitLoopIsSinkMethod(method string) bool {
	if independentAwaitLoopSinkMethods[method] {
		return true
	}
	for _, verb := range []string{"add", "append", "insert"} {
		if len(method) > len(verb) &&
			strings.HasPrefix(method, verb) &&
			method[len(verb)] >= 'A' &&
			method[len(verb)] <= 'Z' {
			return true
		}
	}
	return false
}

// independentAwaitLoopConsumingMethods read outer state and change it in one motion, so the next
// iteration sees what this one left.
var independentAwaitLoopConsumingMethods = map[string]bool{
	"pop": true, "shift": true, "splice": true, "next": true, "read": true, "sort": true, "reverse": true,
}

// isNameOnly answers whether an identifier occurrence names something other than a binding read or
// write: a declaration's own name, a member name, a label, or a type position.
func independentAwaitLoopIsNameOnly(identifier *ast.Node) bool {
	parent := identifier.Parent
	if parent == nil {
		return true
	}
	switch parent.Kind {
	case ast.KindPropertyAccessExpression:
		return parent.AsPropertyAccessExpression().Name() == identifier
	case ast.KindBreakStatement, ast.KindContinueStatement, ast.KindLabeledStatement,
		ast.KindJsxAttribute, ast.KindQualifiedName:
		return true
	case ast.KindShorthandPropertyAssignment:
		return false
	}
	return ast.IsDeclarationName(identifier) || ast.IsPartOfTypeNode(identifier)
}

// independentAwaitLoopReadSegments spells the longest property chain a read starts, so that
// `results.length` is one read of `results.length` rather than a read of `results`.
func independentAwaitLoopReadSegments(root *ast.Node) []string {
	segments := independentAwaitLoopSegments(root)
	current := root
	for {
		parent := current.Parent
		for parent != nil &&
			(parent.Kind == ast.KindParenthesizedExpression || parent.Kind == ast.KindNonNullExpression) {
			current = parent
			parent = current.Parent
		}
		if parent == nil || parent.Kind != ast.KindPropertyAccessExpression ||
			parent.AsPropertyAccessExpression().Expression != current {
			return segments
		}
		segments = append(segments, parent.AsPropertyAccessExpression().Name().Text())
		current = parent
	}
}

// independentAwaitLoopPathsOverlap answers whether one chain is a prefix of the other: reading
// `cursor` whole sees a write to `cursor.after`, and reading `results.length` sees a push to
// `results`, while `this.cache` and `this.fetch` are unrelated.
func independentAwaitLoopPathsOverlap(first []string, second []string) bool {
	if len(first) == 0 || len(second) == 0 {
		return false
	}
	shorter := min(len(first), len(second))
	for index := range shorter {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

// independentAwaitLoopHeaderRange spans `for (...)`, from the keyword to the closing parenthesis.
func independentAwaitLoopHeaderRange(sourceFile *ast.SourceFile, loop *ast.Node, body *ast.Node) core.TextRange {
	start := rule.TokenRange(sourceFile, loop).Pos()
	bodyStart := rule.TokenRange(sourceFile, body).Pos()
	header := strings.TrimRight(sourceFile.Text()[start:bodyStart], " \t\r\n")
	return core.NewTextRange(start, start+len(header))
}
