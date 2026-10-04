package core

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/descriptor"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// isJavaScriptSourceFile answers whether the rule should run on this file at all.
//
// Upstream skips TypeScript outright: `should_run` reads `!ctx.source_type().is_typescript()`,
// because the compiler already reports a getter with a non-returning path, and a getter annotated
// `: boolean | undefined` is *correct* TypeScript that this rule would wrongly flag. That is
// upstream's entire second Tester block, one pass case pinning exactly this.
func isJavaScriptSourceFile(fileName string) bool {
	return strings.HasSuffix(fileName, ".js") ||
		strings.HasSuffix(fileName, ".jsx") ||
		strings.HasSuffix(fileName, ".mjs") ||
		strings.HasSuffix(fileName, ".cjs")
}

// GetterReturnOptions configures whether a bare `return;` satisfies the rule.
//
// This is the whole option surface. ESLint's `meta.schema` is one object with a single
// `allowImplicit` boolean and `additionalProperties: false`, so there is nothing else to read and
// a case configuring anything else configures nothing.
type GetterReturnOptions struct {
	// AllowImplicit lets a getter satisfy the rule with a `return` carrying no expression, which
	// yields `undefined`. Off by default, because a getter returning nothing is usually the bug
	// this rule exists to find rather than a deliberate choice.
	AllowImplicit bool `json:"allowImplicit"`
}

// GetterReturn requires a getter to return a value on every path out of it.
//
// A getter looks like a property read at the call site, so a path through it that returns nothing
// silently yields `undefined` where the reader expected a value. That is the defect: not a missing
// statement, but a property that lies about being a property.
//
// Three shapes count as a getter here, matching upstream: a `get` accessor in a class, a `get`
// accessor in an object literal, and a function sitting under the `get` key of a property
// descriptor passed to `Object.defineProperty`, `Object.defineProperties`, `Object.create`, or
// `Reflect.defineProperty`.
//
// Examples of incorrect code:
//
//	var foo = { get bar() {} };
//	class foo { get bar() { if (baz) { return true; } } }
//	Object.defineProperty(foo, "bar", { get: function () {} });
//
// Examples of correct code:
//
//	var foo = { get bar() { return true; } };
//	class foo { get bar() { if (baz) { return true; } else { return false; } } }
//	var foo = { get bar() { throw new Error("nope"); } };
//
// # Why there is no control flow graph here, and why there does not need to be
//
// Upstream reaches for oxc's CFG (`ctx.cfg()`) and walks it depth-first looking for a basic block
// that neither returns nor throws and has no outgoing edge. We have no CFG and are not building
// one, so the question is whether TypeScript's flow nodes are a substitute. Measured rather than
// assumed: they are not, and the reason is that they answer a different question.
//
// `Node.FlowNodeData()` is real and populated, but TypeScript's flow graph exists for *narrowing*
// and is keyed on the expression positions where a type could change. Probed on three getter bodies
// drawn from upstream's own corpus, counting statements that carry a flow node at all:
//
//	if (baz) { return true; }              4 statements, 2 with a flow node
//	try { return a(); } catch {}           4 statements, 1 with a flow node
//	for (let i=0;i<10;i++) { return i; }   3 statements, 1 with a flow node
//
// The middle line is the one that decides it. `try { return a(); } catch {}` is upstream's sharpest
// fail case, separated from the passing `try { return a(); } finally {}` by exactly the question
// this rule asks, and it carries one flow node in the whole body. There is no return instruction
// and no throw instruction to find, because a narrowing graph does not record them. Walking it
// would not be an approximation of the CFG answer; it would be reading an unrelated structure.
//
// So the analysis here is structural, over the statement tree, and it reproduces upstream's verdict
// on every discrimination its corpus draws. That is worth stating precisely, because "structural"
// invites the assumption that it is the weaker instrument. For this question it is not. "Does every
// path out of this body return" is a property of how statements nest, and the four constructs that
// make it interesting are all visible in the tree:
//
//	if        exits only when it has an else and both arms exit
//	try       exits when the finally exits, or when the try exits and the catch exits
//	switch    exits when it has a default and every non-empty clause exits
//	loops     never count, because the body may run zero times
//
// The loop rule is the one that looks like a limitation and is not. Upstream passes a loop whose
// body returns *followed by* a trailing return, and fails the identical loop without the trailing
// return, which is the same judgment reached here for the same reason: no static analysis can know
// the loop runs. A CFG gets this right by having a bypass edge around the loop, and the tree gets
// it right by declining to credit the body.
//
// The cost of this decision is that no checker is acquired, so this rule does not pay the per-file
// exclusive lock at all.
//
// # What this deliberately does not catch, matching upstream
//
// A getter whose only exit is inside a nested function does not count, because the nested function
// returns from itself. Upstream fails `get bar() { ~function () { return true; }() }` and so does
// this. Conversely `return` inside a nested function must not credit the outer getter, which is why
// the walk stops at every function boundary.
var GetterReturn = rule.Rule{
	Name:       "getter-return",
	NoListener: rule.NoListenerDeclinesIrrelevantFiles,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		allowImplicit := false
		if parsed, isParsed := rule.OptionsAs[GetterReturnOptions](options); isParsed {
			allowImplicit = parsed.AllowImplicit
		}

		// TypeScript checks this itself, so upstream skips typed files outright rather than
		// duplicating an error the compiler already reports. Its `should_run` reads
		// `!ctx.source_type().is_typescript()`, and its second Tester block exists only to pin
		// that: a getter typed `boolean | undefined` with a non-returning path is clean there.
		//
		// Matching that here means the rule declines the file, not the case. Declining by
		// returning nil listeners is also the cheapest possible decline, which matters more in
		// this tree than upstream because our walk is shared.
		if !isJavaScriptSourceFile(ctx.SourceFile.FileName()) {
			return nil
		}

		check := func(node *ast.Node, body *ast.Node) {
			if body == nil || body.Kind != ast.KindBlock {
				return
			}
			if !isGetterFunction(node) {
				return
			}
			if bodyDefinitelyExits(body, allowImplicit) {
				return
			}
			ctx.ReportRange(getterHeadRange(ctx.SourceFile, node, body), rule.Message{
				Id: "expected",
				Description: "This getter can finish without returning a value, so reading the " +
					"property yields undefined on that path. Return a value from every path.",
			})
		}

		return rule.Listeners{
			ast.KindGetAccessor: func(node *ast.Node) {
				check(node, bodyOf(node))
			},
			ast.KindFunctionExpression: func(node *ast.Node) {
				check(node, bodyOf(node))
			},
			ast.KindArrowFunction: func(node *ast.Node) {
				check(node, bodyOf(node))
			},
			ast.KindMethodDeclaration: func(node *ast.Node) {
				check(node, bodyOf(node))
			},
		}
	},
}

// bodyOf returns the body a function-like node executes, or nil when it has none.
//
// An arrow function with an expression body returns that expression unconditionally, so it can
// never violate this rule. Upstream reaches the same conclusion in its `arrow_expr.is_expression()`
// early break. That filtering happens at the call site rather than here; see the note below.
func bodyOf(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindGetAccessor:
		if body := node.AsGetAccessorDeclaration().Body; body != nil {
			return body.AsNode()
		}
	case ast.KindFunctionExpression:
		if body := node.AsFunctionExpression().Body; body != nil {
			return body.AsNode()
		}
	case ast.KindArrowFunction:
		// No block-kind filter here, deliberately. An arrow with an expression body returns that
		// expression unconditionally and must not be reported, but the `check` closure already
		// declines any body that is not a block, so filtering in both places is one check doing
		// the work of two. A mutation sweep scored each gate as a survivor on its own and caught
		// them only when both were removed together, which is the signature of a redundant pair.
		// The gate is kept at `check` rather than here because that one also guards the three
		// non-arrow shapes.
		if body := node.AsArrowFunction().Body; body != nil {
			return body.AsNode()
		}
	case ast.KindMethodDeclaration:
		if body := node.AsMethodDeclaration().Body; body != nil {
			return body.AsNode()
		}
	}
	return nil
}

// getterHeadRange is where the finding points: the getter's head, not its whole body.
//
// Anchoring on the head rather than the node keeps the caret on the declaration a reader has to
// change, and keeps a multi-hundred-line getter from underlining itself entirely. `headEnd` is the
// body's start, so the range runs from the declaration's first token to just before the brace.
func getterHeadRange(file *ast.SourceFile, node *ast.Node, body *ast.Node) core.TextRange {
	// TokenRange is what ReportNode uses, and it is what skips the leading trivia. Building the
	// range from node.Pos() directly is the defect that helper exists to prevent: a getter preceded
	// by a comment would report at the comment, and a `-next-line` suppression written above it
	// could never match.
	head := rule.TokenRange(file, node)
	start := head.Pos()
	end := body.Pos()
	if end <= start {
		end = head.End()
	}
	return core.NewTextRange(start, end)
}

// isGetterFunction answers whether this function-like node is in getter position: a `get` accessor in
// a class or object literal, or the `get` of a property descriptor, which the `descriptor` shelf
// recognizes and `no-setter-return` shares for `set`.
//
// The descriptor's `Object` is matched by spelling here, since this rule runs without the checker and
// only on JavaScript files.
func isGetterFunction(node *ast.Node) bool {
	if node.Kind == ast.KindGetAccessor {
		// A `get` accessor is a getter in both a class and an object literal, and there is no
		// other thing it can be.
		return true
	}
	return descriptor.IsFunctionUnder(node, "get", func(*ast.Node) bool { return true }, descriptor.AnyArgument)
}

// bodyDefinitelyExits answers whether every path through a statement leaves the enclosing function.
//
// "Exits" rather than "returns" because a throw counts: upstream passes
// `get willThrowSoValid() { throw MyException() }`, and it is right to, since that path never
// yields a value at all rather than yielding the wrong one.
//
// Everything not named below answers false, which is the safe direction: an unrecognized statement
// is assumed not to exit, so the rule reports rather than staying silent. That is the correct bias
// for a correctness rule but it is also the one that produces false positives, which is why the
// recognized set covers every construct upstream's corpus exercises.
func bodyDefinitelyExits(node *ast.Node, allowImplicit bool) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.KindReturnStatement:
		// A bare `return;` yields undefined, which is the thing the rule is looking for unless
		// the reader has said they meant it.
		if allowImplicit {
			return true
		}
		return node.AsReturnStatement().Expression != nil

	case ast.KindThrowStatement:
		return true

	case ast.KindBlock:
		// A block exits when any statement in it does. Statements after an exiting one are
		// unreachable, so scanning past the first is harmless and stopping early is not required
		// for correctness.
		for _, statement := range node.AsBlock().Statements.Nodes {
			if bodyDefinitelyExits(statement, allowImplicit) {
				return true
			}
		}
		return false

	case ast.KindIfStatement:
		// Both arms have to exit, and an absent else is one that does not. Without an else there is
		// a path that falls through, which is upstream's `get bar(){if(baz) {return true;}}` fail
		// case and the most common real defect this rule catches.
		//
		// There is deliberately no `ElseStatement != nil` conjunct here. It read as the load-bearing
		// part of this branch and it was subsumed: an absent else is a nil node, and the recursion
		// rejects nil on its first line, so the explicit check and the recursion agreed on every
		// input. A mutation sweep scored it as a survivor and no fixture could have caught it,
		// because none can exist. Deleted rather than tested, so the next reader does not spend the
		// same hour proving the same thing.
		statement := node.AsIfStatement()
		return bodyDefinitelyExits(statement.ThenStatement, allowImplicit) &&
			bodyDefinitelyExits(statement.ElseStatement, allowImplicit)

	case ast.KindTryStatement:
		return tryStatementExits(node.AsTryStatement(), allowImplicit)

	case ast.KindSwitchStatement:
		return switchStatementExits(node.AsSwitchStatement(), allowImplicit)

	case ast.KindLabeledStatement:
		// A label wraps a statement without changing whether it exits. `break label` would, but a
		// break out of a getter body reaches the end of the body, which is already not an exit.
		return bodyDefinitelyExits(node.AsLabeledStatement().Statement, allowImplicit)

	case ast.KindWithStatement:
		return bodyDefinitelyExits(node.AsWithStatement().Statement, allowImplicit)
	}

	// Loops land here and answer false on purpose. A `for`, `while`, or `for-in` body may run zero
	// times, so a return inside one is not a return on every path. Upstream agrees: it fails a
	// getter whose only return is inside a loop, and passes the identical loop followed by a
	// trailing return. `do-while` runs at least once and could in principle be credited, but
	// upstream does not credit it and this reproduces that rather than silently improving on it.
	return false
}

// tryStatementExits answers whether a try statement exits on every path.
//
// This is the subtlest of the four and the pair upstream uses to pin it is
// `try { return a(); } finally {}` passing against `try { return a(); } catch {}` failing. The
// difference is that a catch introduces a path: the try may throw partway through, and then only
// the catch runs. A finally introduces no path of its own.
func tryStatementExits(statement *ast.TryStatement, allowImplicit bool) bool {
	// A finally that exits wins outright, since it runs on every path out of the try and the
	// catch both, and its own exit overrides theirs.
	if statement.FinallyBlock != nil &&
		bodyDefinitelyExits(statement.FinallyBlock.AsNode(), allowImplicit) {
		return true
	}
	if !bodyDefinitelyExits(statement.TryBlock.AsNode(), allowImplicit) {
		return false
	}
	if statement.CatchClause == nil {
		return true
	}
	return bodyDefinitelyExits(statement.CatchClause.AsCatchClause().Block.AsNode(), allowImplicit)
}

// switchStatementExits answers whether a switch exits on every path.
//
// Two conditions, and both are load-bearing. Without a default clause the scrutinee may match
// nothing and fall out of the switch entirely. And every clause has to exit, since one that does
// not is a path through.
//
// An empty clause is not a failure: `case A: case B: return x;` is a deliberate fallthrough and
// the empty `case A` exits by way of `case B`. Only a trailing empty clause genuinely falls out.
func switchStatementExits(statement *ast.SwitchStatement, allowImplicit bool) bool {
	clauses := statement.CaseBlock.AsCaseBlock().Clauses.Nodes

	hasDefault := false
	for _, clause := range clauses {
		if clause.Kind == ast.KindDefaultClause {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		return false
	}

	for index, clause := range clauses {
		statements := clause.AsCaseOrDefaultClause().Statements.Nodes
		if len(statements) == 0 {
			// Falls through to the next clause, unless there is no next clause.
			if index == len(clauses)-1 {
				return false
			}
			continue
		}
		exits := false
		for _, inner := range statements {
			if bodyDefinitelyExits(inner, allowImplicit) {
				exits = true
				break
			}
		}
		if !exits {
			return false
		}
	}
	return true
}
