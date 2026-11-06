package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

const concurrencyNoLostUpdateId = "lostUpdate"

// concurrencyNoLostUpdateText is the rule's message, whose wording lives in
// `policy/messages/concurrency-no-lost-update.json`. Both findings share the id, and the phrase says
// which stale read the write builds on: a variable, or an entry a read call returned.
var (
	concurrencyNoLostUpdateText = policy.MessageOf(
		"nexus/concurrency-no-lost-update",
		concurrencyNoLostUpdateId,
	)
	concurrencyNoLostUpdateVariableRead   = concurrencyNoLostUpdateText.Option("suspendedRead", "variable")
	concurrencyNoLostUpdateStoreEntryRead = concurrencyNoLostUpdateText.Option("suspendedRead", "storeEntry")
)

// concurrencyNoLostUpdateMessage names the written target, so a reader of the finding does not have
// to work out which of several writes on a line is the one that loses an update.
func concurrencyNoLostUpdateMessage(target string) rule.Message {
	return rule.Message{
		Id: concurrencyNoLostUpdateId,
		Description: concurrencyNoLostUpdateText.Render(
			map[string]string{"target": target},
			concurrencyNoLostUpdateVariableRead,
		),
	}
}

// concurrencyNoLostUpdateStoreMessage names the read call whose stale answer the write builds on.
func concurrencyNoLostUpdateStoreMessage(read string) rule.Message {
	return rule.Message{
		Id: concurrencyNoLostUpdateId,
		Description: concurrencyNoLostUpdateText.Render(
			map[string]string{"read": read},
			concurrencyNoLostUpdateStoreEntryRead,
		),
	}
}

// ConcurrencyNoLostUpdate reports a read-modify-write that spans a suspension: an assignment whose
// new value is computed from the same variable or property it writes, where that value was read
// before an await or a yield that runs before the write.
//
//	invalid: count = count + await next();
//	invalid: count += await next();
//	invalid: const old = state.count; await save(); state.count = old + 1;
//	invalid: state.total = state.total + (await price());
//	valid:   state.lastRunAt = Date.now();                      not computed from its prior value
//	valid:   const original = console.log; ... finally { console.log = original; }   a restore
//	valid:   await save(); state.count = state.count + 1;       read after the suspension
//	valid:   async function f() { let total = 0; total += await next(); }   nothing else can write it
//
// # Where it came from
//
// Kirk's ruling of 2026-10-01, replacing `require-atomic-updates`, which is off in both engines.
// Upstream reports any write after an await to something read before it, and on ahra none of its 39
// findings was a read-modify-write: they were timestamps, flags, cached tokens and restores in
// `finally`. This rule asks the narrower question that names an actual lost update.
//
// # The conditions, all of which hold for a finding
//
//  1. The function can suspend: an async function or a generator. A suspension is an `await`, a
//     `yield`, or a `for await` loop's step, and only one in this function counts, never one inside a
//     nested function.
//  2. The write is a plain `=` or an arithmetic or bitwise compound assignment (`+=`, `-=`, `|=` and
//     the rest). `??=`, `||=` and `&&=` are check-then-act rather than read-modify-write and are
//     never judged. Destructuring assignments are not judged.
//     A write through a store's write call (`updateAccount(key, value)`, `cache.set(key, value)`) is
//     judged exactly as a plain `=` of its last argument to that store entry; storeAccessOf says what
//     makes a read call and a write call one store, and the read call is then the only way that
//     entry is read.
//  3. The target is a variable, or a static member path (`a.b`, `a['b']`, `this.c.d`) rooted at a
//     variable or at `this`. A computed key whose value the syntax does not settle (`a[k]`) is judged
//     only in a compound assignment, which reads and writes through one evaluated reference; under a
//     plain `=` two reads of `a[k]` are the same property only if `k` held still, which nothing here
//     can show.
//  4. Someone else can write the target during the suspension:
//     - a variable declared outside this function, or a local of it that a nested function writes;
//     - a member path rooted at `this`, at a variable declared outside this function, at one of this
//     function's own parameters (the caller holds the object), or at a local a nested function
//     references.
//     A local nobody else can see is never judged: `let total = 0; total += await next()` loses
//     nothing, because there is no one to lose it to.
//  5. The new value is computed from the target's prior value, read before a suspension that runs
//     before the write, on some path. The read is either in the assigned expression itself
//     (`x = x + await y`, with the compound form's implicit read counting as first), or reached
//     through a `const` of this function whose initializer read it (`const old = s.count; await f();
//     s.count = old + 1`), through any chain of such consts.
//  6. The value is modified rather than copied, and computed here. `console.log = original` writes back
//     exactly the value it saved, which is a restore, and restoring a saved handler in `finally` is the
//     pattern the upstream rule reported most on this tree. Any operation on the value (arithmetic, a
//     call, a member read, a spread into a new literal) makes it a modification. Two things are not:
//     - an awaited call's result, which is the callee's answer rather than this function's
//     computation (`token = (await refresh(token)).next` sends the old value out and writes back
//     what came back);
//     - a write after this function itself overwrote the target, between the read and the
//     write, which is the save-and-restore protocol (`const original = console.log; console.log =
//     filter; try { await run(); } finally { console.log = original.bind(console); }`).
//
// # How "before a suspension" is decided
//
// Inside the assigned expression, by evaluating it in order along each path: `&&`, `||`, `??` and
// `?:` fork, a branch's value carries only what that branch read, and a suspension marks stale every
// read already taken on its path. That is what keeps `x = c ? x + 1 : await f()` silent: the read and
// the suspension are on different paths. And `x = x || await f()` is silent too, because on the path
// that suspends the value written is `await f()`, not `x`.
//
// Across statements, through the function's control-flow graph: from the end of the const's
// declaration to the start of the write, is there a path through a suspension that does not pass the
// declaration again? Again is what keeps a const declared inside a loop honest, since each iteration
// binds a fresh one. A suspension whose expression contains the write (`await save(s.count = old + 1)`)
// runs after it and is not counted.
//
// # What it deliberately does not see
//
// A value laundered through a `let`, an alias of the object (`const snapshot = state; ...
// snapshot.count`), and a computed key under a plain `=` are all silent. So are a store read
// destructured straight into a pattern, a store whose key is a member read or a call, a store whose
// read and write functions are declared in different files, and a store object mutated in place and
// written back verbatim (`state.cursor = c; writeState(state)`). Each is a missed finding rather than
// a false one, which is the direction this rule errs in.
//
// No fix and no suggestion: the repair is a restructuring (re-read after the suspension, a lock, or
// an atomic update on the store), and which one is right is the author's call.
var ConcurrencyNoLostUpdate = rule.Rule[rule.NoOptions]{
	Name: "nexus/concurrency-no-lost-update",

	// Symbol identity is what makes a read and a write the same variable, and what tells an outer
	// variable from a shadowing local of the same spelling.
	NeedsTypeChecker: true,

	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		if ruleContext.TypeChecker == nil {
			return nil
		}
		return rule.Listeners{
			ast.KindSourceFile: func(sourceFile *ast.Node) {
				var visit func(node *ast.Node) bool
				visit = func(node *ast.Node) bool {
					if control_flow_graph.IsRoot(node) && concurrencyNoLostUpdateCanSuspend(node) {
						analyzeConcurrencyNoLostUpdateRoot(ruleContext, node)
					}
					node.ForEachChild(visit)
					return false
				}
				sourceFile.ForEachChild(visit)
			},
		}
	},
}

// concurrencyNoLostUpdateCanSuspend reports whether a root is an async function or a generator.
func concurrencyNoLostUpdateCanSuspend(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
	default:
		return false
	}
	if node.ModifierFlags()&ast.ModifierFlagsAsync != 0 {
		return true
	}
	switch node.Kind {
	case ast.KindFunctionDeclaration:
		return node.AsFunctionDeclaration().AsteriskToken != nil
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().AsteriskToken != nil
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().AsteriskToken != nil
	}
	return false
}

const (
	concurrencyNoLostUpdateFreshVerbatim concurrencyNoLostUpdateDependencies = 1 << iota
	concurrencyNoLostUpdateFreshModified
	concurrencyNoLostUpdateStaleVerbatim
	concurrencyNoLostUpdateStaleModified
)

// concurrencyNoLostUpdateOutcome is one evaluation path through an expression.
type concurrencyNoLostUpdateOutcome struct {
	dependencies concurrencyNoLostUpdateDependencies
	// suspended is whether a suspension has happened on this path, counting one before the expression.
	suspended bool
	// during is whether a suspension happened inside this expression, which is what stales the reads
	// its earlier siblings took.
	during bool
}

// concurrencyNoLostUpdateLocal is a const of the root that may carry the target's prior value.
type concurrencyNoLostUpdateLocal struct {
	name        *ast.Node
	initializer *ast.Node
	// elementPath is set for a binding element of an object pattern over a static path, which reads
	// that path plus the element's property at the declaration.
	elementPath *concurrencyNoLostUpdatePath
}

type concurrencyNoLostUpdateEventKind uint8

const (
	concurrencyNoLostUpdateSuspend concurrencyNoLostUpdateEventKind = iota
	concurrencyNoLostUpdateDeclarationStart
	concurrencyNoLostUpdateDeclarationEnd
	concurrencyNoLostUpdateAnchor
)

type concurrencyNoLostUpdateEvent struct {
	kind   concurrencyNoLostUpdateEventKind
	node   *ast.Node
	symbol *ast.Symbol
}

// concurrencyNoLostUpdateLocalKey memoizes what one const carries of one target.
type concurrencyNoLostUpdateLocalKey struct {
	local      *ast.Symbol
	targetRoot *ast.Symbol
	targetPath string
}

type concurrencyNoLostUpdateNestedUsage struct {
	referenced bool
	written    bool
}

func analyzeConcurrencyNoLostUpdateRoot(ruleContext rule.Context, root *ast.Node) {
	analysis := &concurrencyNoLostUpdateRoot{
		ruleContext:      ruleContext,
		root:             root,
		locals:           map[*ast.Symbol]*concurrencyNoLostUpdateLocal{},
		between:          map[concurrencyNoLostUpdateBetweenKey]bool{},
		writtenPaths:     map[*ast.Node]*concurrencyNoLostUpdatePath{},
		localMemo:        map[concurrencyNoLostUpdateLocalKey]concurrencyNoLostUpdateDependencies{},
		localInProgress:  map[concurrencyNoLostUpdateLocalKey]bool{},
		nestedUsageCache: map[*ast.Symbol]concurrencyNoLostUpdateNestedUsage{},
		storeAccesses:    map[*ast.Node]*concurrencyNoLostUpdateStoreAccess{},
		stableBindings:   map[*ast.Symbol]bool{},
	}

	var assignments []*ast.Node
	var calls []*ast.Node
	suspends := false
	analysis.forEachOwnNode(func(node *ast.Node) {
		switch node.Kind {
		case ast.KindAwaitExpression, ast.KindYieldExpression:
			suspends = true
		case ast.KindForOfStatement:
			if node.AsForInOrOfStatement().AwaitModifier != nil {
				suspends = true
			}
		case ast.KindBinaryExpression:
			operator := node.AsBinaryExpression().OperatorToken.Kind
			if ast.IsAssignmentOperator(operator) && !ast.IsLogicalOrCoalescingAssignmentOperator(operator) {
				assignments = append(assignments, node)
			}
		case ast.KindCallExpression:
			calls = append(calls, node)
		case ast.KindVariableDeclaration:
			analysis.collectLocal(node)
		}
	})
	// A function that never suspends cannot hold a stale read, so it costs nothing beyond this walk.
	//
	// A cost filter rather than a judgment, and a mutant removing it survived for that reason: with no
	// suspension, no assigned expression can suspend and no path through the graph carries one.
	if !suspends {
		return
	}

	for _, assignment := range assignments {
		binary := assignment.AsBinaryExpression()
		target, isPath := analysis.pathOf(binary.Left)
		if !isPath {
			// Reached for a plain `=` too, and harmless there: no read in an assigned expression can
			// match a computed name, so such a write can never be shown computed from its target.
			target, isPath = analysis.compoundTargetOf(binary.Left)
		}
		if !isPath || !analysis.targetIsShared(target) {
			continue
		}
		if analysis.losesUpdate(assignment, target) {
			targetRange := rule.TokenRange(ruleContext.SourceFile, binary.Left)
			targetText := ruleContext.SourceFile.Text()[targetRange.Pos():targetRange.End()]
			ruleContext.ReportNode(assignment, concurrencyNoLostUpdateMessage(targetText))
		}
	}

	// Writes through a store's write call, judged exactly as a plain `=` is: the written value must be
	// computed from a stale read of the same entry, through that store's read call.
	for _, call := range calls {
		access := analysis.storeAccessOf(call)
		if access == nil || !access.isWrite || !analysis.targetIsShared(access.target) {
			continue
		}
		evaluator := concurrencyNoLostUpdateEvaluator{analysis: analysis, target: access.target, anchor: call}
		for _, outcome := range evaluator.evaluate(access.value, false) {
			if outcome.dependencies&concurrencyNoLostUpdateStaleModified != 0 {
				ruleContext.ReportNode(
					call,
					concurrencyNoLostUpdateStoreMessage(analysis.storeReadText(access.target, call)),
				)
				break
			}
		}
	}
}

// concurrencyNoLostUpdateComputedName stands for a member whose key the syntax does not settle. It
// can never equal a real property name, so no read in an assigned expression matches it.
const concurrencyNoLostUpdateComputedName = "\x00computed"

// concurrencyNoLostUpdateSkipTransparent sees through the wrappers that change neither a value nor
// which reference it is.
func concurrencyNoLostUpdateSkipTransparent(node *ast.Node) *ast.Node {
	current := node
	for current != nil {
		switch current.Kind {
		case ast.KindParenthesizedExpression:
			current = current.AsParenthesizedExpression().Expression
		case ast.KindNonNullExpression:
			current = current.AsNonNullExpression().Expression
		case ast.KindAsExpression:
			current = current.AsAsExpression().Expression
		case ast.KindSatisfiesExpression:
			current = current.AsSatisfiesExpression().Expression
		case ast.KindTypeAssertionExpression:
			current = current.AsTypeAssertion().Expression
		default:
			return current
		}
	}
	return nil
}

func concurrencyNoLostUpdateWithin(node *ast.Node, ancestor *ast.Node) bool {
	for current := node; current != nil; current = current.Parent {
		if current == ancestor {
			return true
		}
	}
	return false
}

func concurrencyNoLostUpdateDeduplicate(outcomes []concurrencyNoLostUpdateOutcome) []concurrencyNoLostUpdateOutcome {
	seen := map[concurrencyNoLostUpdateOutcome]bool{}
	unique := outcomes[:0:0]
	for _, outcome := range outcomes {
		if !seen[outcome] {
			seen[outcome] = true
			unique = append(unique, outcome)
		}
	}
	return unique
}

func concurrencyNoLostUpdateModifiedAll(outcomes []concurrencyNoLostUpdateOutcome) []concurrencyNoLostUpdateOutcome {
	for index := range outcomes {
		outcomes[index].dependencies = outcomes[index].dependencies.modified()
	}
	return concurrencyNoLostUpdateDeduplicate(outcomes)
}

// concurrencyNoLostUpdateBetweenKey memoizes one control-flow question.
type concurrencyNoLostUpdateBetweenKey struct {
	symbol     *ast.Symbol
	anchor     *ast.Node
	targetRoot *ast.Symbol
	targetPath string
}

func concurrencyNoLostUpdateContains(outer *ast.Node, inner *ast.Node) bool {
	return inner.Pos() >= outer.Pos() && inner.End() <= outer.End()
}

// concurrencyNoLostUpdateStoreAccess is one call into a store: a read of an entry, or a write of a
// new value to it.
type concurrencyNoLostUpdateStoreAccess struct {
	target  concurrencyNoLostUpdatePath
	isWrite bool
	// value is the written value, a write call's last argument.
	value *ast.Node
}

var (
	concurrencyNoLostUpdateReadVerbs  = []string{"get", "read", "load"}
	concurrencyNoLostUpdateWriteVerbs = []string{"set", "update", "write", "save", "put", "store"}
)

// concurrencyNoLostUpdateSplitVerb splits `getAccountCredentials` into its verb and its noun,
// `AccountCredentials`, when it starts with one of verbs at a word boundary.
func concurrencyNoLostUpdateSplitVerb(name string, verbs []string) (string, bool) {
	for _, verb := range verbs {
		if !strings.HasPrefix(name, verb) {
			continue
		}
		noun := name[len(verb):]
		if noun == "" || (noun[0] >= 'A' && noun[0] <= 'Z') {
			return noun, true
		}
	}
	return "", false
}
