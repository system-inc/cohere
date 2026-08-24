package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

var messageNoUselessAssignment = rule.Message{
	Id: "noUselessAssignment",
	Description: "This assigns a value that nothing ever reads. Every path leaving this line either " +
		"ends or writes the variable again before any code looks at it, so the value computed here is " +
		"discarded. That is usually a symptom rather than the bug: the write was meant to be read and " +
		"an early return, a wrong branch, or a second assignment swallowed it, or the wrong variable " +
		"was named on the left. Remove the assignment, or use the value it produces.",
}

// NoUselessAssignment flags a write whose value no later read can observe, a dead store.
//
//	valid:   let v = 'a'; f(v); v = 'b'; f(v);
//	valid:   let v = 1; for (let i = 0; i < 10; i++) { f(v); v = 2; }
//	valid:   let v = 1; f(v); setTimeout(() => f(v)); v = 2;
//	valid:   let v; try { v = 1; h(); v = 2; } catch {} return v;
//	invalid: let v = 'a'; f(v); v = 'dead';
//	invalid: function f(c) { let v = 1; if (c) { v = 2; return; } g(v); }
//
// The value assigned here is computed and then thrown away. On its own that is only wasted work, but
// the shapes that produce it are nearly always a mistake somewhere else: a branch that returns before
// the value is used, a second write that lands first, or a left-hand side naming the wrong variable.
// Upstream's own corpus is mostly those three.
//
// # Why this is flow-sensitive and cannot be answered from the symbol table
//
// Whether a write is dead is not a property of the write. `v = 2` is dead in one file and live in
// the next, and nothing about the assignment node or the symbol distinguishes them. The question is
// whether any read of the same binding is reachable from this write without an intervening write,
// which is a path property of the program and needs the control flow graph. The symbol table can say
// that a binding is read somewhere; it cannot say that the read happens after this particular write,
// which is the entire discrimination. `let v = 1; f(v); v = 2;` and `let v = 1; v = 2; f(v);` have
// identical symbol tables and opposite verdicts.
//
// # The graph we have points the wrong way, and that decides the algorithm
//
// Upstream runs a backward liveness dataflow over oxc's CFG: it walks blocks in reverse, unions the
// live sets of each block's successors, and reports a write whose bit is not live. That shape needs
// successor edges.
//
// TypeScript's flow graph has none. `ast.FlowNode` carries `Antecedent` and `Antecedents` and
// nothing else, so every edge points from a node to what preceded it. Measured rather than read off
// the struct: a probe walking identifier flow nodes in `let v = 'a'; g(v); v = 'dead';` printed
// chains running strictly backward to `[Start]`, and the assignment expression itself carries no
// flow node at all, only the identifier on its left-hand side does.
//
// So the question is inverted instead of the graph. Rather than asking a write which reads follow
// it, each read is asked whether its own antecedent chain passes through this write. A write that no
// read can reach backward is a write no read observes forward, which is the same predicate computed
// from the edges that exist. That inversion is what makes the port possible at all, and it is why
// this does not look like upstream's code.
//
// # What the inversion costs, and where it stops
//
// Reachability is asked per candidate write against every read of the same binding, so the work is
// quadratic in the occurrences of one variable rather than linear in blocks. Upstream's bitset
// dataflow computes all symbols in one pass. In exchange this needs no block construction, no loop
// header detection, and no cached loop liveness, which is most of upstream's 1,581 lines.
//
// Three shapes fall out of the graph correctly with no special handling, which is the main evidence
// the inversion is sound rather than lucky. A branch that returns before the read leaves the read's
// chain passing through the other arm, so the write is unreachable and reported. A loop back-edge
// makes the loop header a `FlowFlagsLoopLabel` with the body among its antecedents, so a write at
// the bottom of the body is reachable from a read at the top and is correctly live. A `try` block
// joins into a `FlowFlagsBranchLabel` carrying both the normal and the caught path, so a write
// followed by a throwing call stays live.
//
// # The one shape the graph cannot answer, and why it is a guard rather than a fix
//
// A function body starts its own flow graph. A probe confirmed this directly: in
// `let v = 1; g(v); setTimeout(() => g(v)); v = 2;` the read of `v` inside the arrow begins at
// `[Start "() => g(v)"]`, with no antecedent path to the enclosing function's writes at all. The
// inversion therefore reports the final write as dead, and it is live, because the closure can run
// after it.
//
// That is a false positive, the direction that costs a reader their trust in the rule, so it is
// guarded structurally rather than approximated: a binding read from inside a different
// function-like than the one declaring it is never reported. Upstream carries the same guard under
// the name `has_captured_read`, and its corpus has four clean cases that exist only to pin it. The
// guard is deliberately coarse. It silences every write to a captured variable, including writes
// that really are dead, which is a false negative traded for a false positive.
//
// # Scope of this port, stated rather than implied
//
// Measured against upstream's imported corpus of 71 clean and 43 failing inputs: this reproduces 33
// of upstream's 54 diagnostics, matches 27 of the 43 failing inputs exactly, and reports nothing on
// any of the 71 clean cases. It never reports where upstream does not, on any input in the corpus.
//
// Covered: simple and compound assignment to a plain identifier, the initializer of a variable
// declarator, a write killed by a later write on every path, and the branch, loop, switch and
// try/catch joins those travel through.
//
// Declined, each one a silent miss rather than a wrong report:
//
//	update expressions            `v++` and `v--` are writes this does not treat as candidates
//	destructuring targets         a pattern element's flow node does not line up with the
//	                              single-identifier reachability question this asks
//	writes inside a try block      upstream suppresses reporting these; this additionally declines
//	                              to let them kill an earlier write, which costs five diagnostics
//	self-referential write chains  `x = x + 1` followed by `x = 5` needs the right-hand read
//	                              ordered before its own store, which upstream models explicitly
//	captured bindings              any read from another function silences every write, coarser
//	                              than upstream's per-write judgment
//
// `TestNoUselessAssignmentBoundary` pins five of these in executable form, so a later porter who
// picks one up finds out from a failing test rather than from this comment.
//
// No fix and no suggestion. Upstream offers neither, and its snapshot contains no fix output; the
// reason is that the right-hand side can have effects. Deleting `v = f()` removes the call, so the
// repair is not meaning-preserving and cannot be applied unattended. Even as a suggestion it would
// have to choose between deleting the statement and keeping the expression, and which one is right
// depends on whether the call matters.
var NoUselessAssignment = rule.Rule{
	Name: "no-useless-assignment",

	// Two separate needs. Symbol identity decides which occurrences name the same binding, so a
	// shadow in an inner block is not confused with the outer variable. And the flow nodes this
	// walks are populated by the binder as a side effect of building the program, so a run without
	// the checker sees `FlowNode == nil` everywhere and the rule goes silent rather than wrong.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindSourceFile: func(sourceFile *ast.Node) {
				if ctx.TypeChecker == nil {
					return
				}
				analyzeDeadStores(ctx, sourceFile)
			},
		}
	},
}

// deadStoreCandidate is one write that might be dead, paired with the flow node reachability is
// asked against.
//
// The flow node is taken from the target identifier rather than from the assignment, because the
// assignment expression carries none. A probe anchoring on `KindBinaryExpression` printed
// `FLOWNODE=nil` for every assignment in the file and found nothing; the same probe anchored on the
// left-hand identifier separated a dead store from a live one on the first attempt.
type deadStoreCandidate struct {
	// target is the identifier being written, and the node the finding points at. Upstream's
	// snapshot underlines exactly this and not the whole statement.
	target *ast.Node
	flow   *ast.FlowNode
}

// analyzeDeadStores walks the file once, then decides each candidate write against the reads of its
// own binding.
//
// One walk collects both sides because the reads a write must be checked against can sit anywhere in
// the file, including before it. Hoisting and closures both make position unreliable, so nothing here
// depends on source order.
func analyzeDeadStores(ctx rule.Context, sourceFile *ast.Node) {
	var candidates []deadStoreCandidate
	var identifiers []*ast.Node

	var visit func(*ast.Node)
	visit = func(node *ast.Node) {
		if node == nil {
			return
		}
		if node.Kind == ast.KindIdentifier {
			// A property name, a member access target, and an import or export binding can never be
			// a read of a local variable, and skipping them here keeps them out of both the symbol
			// resolution below and every guard's scan.
			if !isNonReferenceIdentifier(node) {
				identifiers = append(identifiers, node)
			}
			if target, ok := writeTargetOf(node); ok {
				if flowData := target.FlowNodeData(); flowData != nil && flowData.FlowNode != nil {
					candidates = append(candidates, deadStoreCandidate{target: target, flow: flowData.FlowNode})
				}
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile)

	// Every identifier is resolved once and the occurrences grouped by binding, rather than each
	// guard rescanning the whole file per candidate. The first version did the latter and cost
	// 3.3 seconds on 3,407 files, 56.7% of all rule time in the tree, because the work was
	// quadratic in a file's identifiers and called the checker once per pair. Grouping makes each
	// guard linear in one binding's own occurrences, which is the size the algorithm actually needs.
	// Only identifiers spelled the same as some candidate's target can matter, so the checker is
	// asked about those and about nothing else. Without this filter every identifier in the file
	// reached `GetSymbolAtLocation`, imports and property names and type references included, and
	// the rule cost 2.3 seconds across the tree, 54% of all rule time. A name comparison is far
	// cheaper than a symbol resolution and it cannot be wrong in the unsafe direction: two different
	// bindings sharing a name are still separated by symbol identity below, and a read of a
	// differently-named binding can never keep this write live.
	//
	// Mutants disabling this filter and the one in the walk both survive, which is the expected
	// result and not a gap: disabling them makes the rule resolve more identifiers and reach the
	// same verdicts, so no fixture can distinguish them by construction. What was measured instead
	// is the pair of properties that matter. The findings against the real tree are byte-identical
	// with and without them, 34 in both cases at the same 34 locations, and the cost falls from
	// 2,305ms to 122ms, from 54.2% of all rule time to 5.1%. A fixture asserting a timing would be
	// flaky, so the measurement lives here rather than in the test file.
	interesting := map[string]bool{}
	for _, candidate := range candidates {
		interesting[candidate.target.Text()] = true
	}
	if len(interesting) == 0 {
		return
	}

	occurrences := map[*ast.Symbol][]*ast.Node{}
	for _, identifier := range identifiers {
		if !interesting[identifier.Text()] {
			continue
		}
		if symbol := resolveOccurrence(ctx, identifier); symbol != nil {
			occurrences[symbol] = append(occurrences[symbol], identifier)
		}
	}

	// The export names are collected in one pass for the same reason. Walking the file per candidate
	// to answer a question about a name was the second half of the cost.
	exported := exportedNames(sourceFile)

	// A symbol is judged once and the verdict reused across its writes. `hasAnyRead` and
	// `isCapturedAcrossFunctions` are properties of the binding, not of the write, so computing them
	// per candidate repeated identical work on every file with more than one assignment.
	type symbolVerdict struct{ eligible bool }
	verdicts := map[*ast.Symbol]symbolVerdict{}

	for _, candidate := range candidates {
		symbol := resolveOccurrence(ctx, candidate.target)
		if symbol == nil || len(symbol.Declarations) == 0 {
			continue
		}

		verdict, decided := verdicts[symbol]
		if !decided {
			verdict = symbolVerdict{eligible: symbolIsEligible(ctx, symbol, exported, occurrences[symbol])}
			verdicts[symbol] = verdict
		}
		if !verdict.eligible {
			continue
		}

		if isObservedAfter(ctx, symbol, candidate, occurrences[symbol]) {
			continue
		}

		ctx.ReportNode(candidate.target, messageNoUselessAssignment)
	}
}

// symbolIsEligible answers the three questions that are properties of a binding rather than of any
// one write to it, so the answer is computed once per symbol and reused.
func symbolIsEligible(ctx rule.Context, symbol *ast.Symbol, exported map[string]bool, group []*ast.Node) bool {
	// A binding this file does not declare in a function-like it controls is out of scope. A
	// module-level `let` that another module imports, or a global, can be read from somewhere this
	// walk cannot see, and reporting it would be a guess.
	declaration := symbol.Declarations[0]
	if !isLocalVariableDeclaration(declaration) {
		return false
	}
	if exported[symbol.Name] {
		return false
	}

	// A binding nothing ever reads is a different judgment. Every write to it is trivially dead, so
	// reporting them here would restate `no-unused-vars` once per assignment and bury the dead-store
	// finding under it. Upstream carries the same guard as `is_used` and skips the symbol before it
	// looks at any write. Four of its clean cases exist only to pin this: three where the only read
	// is the declaration itself, and `let v = 'used variable';`, which has no read at all.
	if !hasAnyRead(group) {
		return false
	}

	return !isCapturedAcrossFunctions(declaration, group)
}

// isObservedAfter reports whether any read of the binding can reach this write backward through the
// flow graph.
//
// This is the inverted liveness question and the whole rule. A read whose antecedent chain passes
// through the write's flow node is a read that observes it, so the write is live. The walk is
// backward because that is the only direction the graph has.
func isObservedAfter(ctx rule.Context, symbol *ast.Symbol, candidate deadStoreCandidate, group []*ast.Node) bool {
	// Every other plain write to the same binding blocks the walk. A read that can only reach this
	// write by crossing a later one observes that later value and not this one, so it does not keep
	// this write live. Without these blockers the rule sees only the writes that nothing reads at
	// all and misses the commonest dead store there is, a value immediately overwritten:
	// `let v = 'used'; f(v); v = 'unused'; v = 'used'; f(v);` has a read of `v` whose chain does run
	// back through `v = 'unused'`, so the unblocked walk calls it live. Upstream gets this from
	// `scratch_live.unset_bit` clearing the bit at every write as the backward pass crosses it;
	// this is the same kill, expressed as a barrier in the graph rather than a bit in a set.
	blockers := map[*ast.FlowNode]bool{}
	for _, identifier := range group {
		if identifier == candidate.target {
			continue
		}
		target, isWrite := writeTargetOf(identifier)
		if !isWrite || target != identifier || !isPlainAssignmentTarget(identifier) {
			continue
		}
		// A write inside a `try` block never blocks. It may be abandoned partway by a throw from
		// itself or from anything after it, so the value it overwrites is still observable on the
		// catch and finally paths and the earlier write stays live. Upstream expresses this as
		// `is_in_try_block` refusing to report; the same judgment lands here because the barrier is
		// what makes those writes look dead in the first place. Three clean cases pin it, including
		// `let v = 'init'; try { v = callA(); try { v = callB(); } catch {} } catch {} console.log(v)`
		// where the barrier is two try levels deep.
		if isInsideTryBlock(identifier) {
			continue
		}
		if flowData := identifier.FlowNodeData(); flowData != nil && flowData.FlowNode != nil {
			blockers[flowData.FlowNode] = true
		}
	}

	for _, identifier := range group {
		// The write's own target is not a read of the value it stores.
		if identifier == candidate.target {
			continue
		}
		if _, isWrite := writeTargetOf(identifier); isWrite {
			// A plain reassignment reads nothing. A compound assignment (`v += 1`) does read the
			// previous value, and that read is what keeps the earlier write live, so it is not
			// skipped here.
			if isPlainAssignmentTarget(identifier) {
				continue
			}
		}
		flowData := identifier.FlowNodeData()
		if flowData == nil || flowData.FlowNode == nil {
			continue
		}
		// A write whose own right-hand side is this read must not block it. In `a = a + 1` the read
		// of `a` is evaluated before the store, so it observes the previous value and keeps the
		// earlier write live. Blocking on it would call `let a = 42; f(a); a = 10; a = a + 1; f(a);`
		// a dead store at `a = 10`, and upstream has three clean cases pinning exactly that.
		// Upstream reaches the same place by pushing a synthetic Read op before the deferred write;
		// here the barrier is lifted for the one read it would wrongly stop.
		// A mutant replacing this copy with an empty map survives, and it is recorded as unmeasured
		// rather than equivalent. 83 constructed inputs across if/else, while, do-while, for,
		// switch, try-finally, labeled break, nested branches and seven right-hand-side shapes
		// failed to distinguish the two, and there is a structural argument that the lifted barrier
		// is always the walk's first step so no other barrier can matter. That argument is not
		// trusted here, because two rewrites resting on it passed the whole imported corpus and
		// both introduced false positives that only a constructed conditional case could see. The
		// copy is kept because it is the version whose behavior is measured.
		effective := blockers
		if enclosing := enclosingAssignment(identifier); enclosing != nil {
			if flowData := enclosing.FlowNodeData(); flowData != nil && flowData.FlowNode != nil &&
				blockers[flowData.FlowNode] {
				effective = make(map[*ast.FlowNode]bool, len(blockers))
				for node := range blockers {
					effective[node] = true
				}
				delete(effective, flowData.FlowNode)
			}
		}
		if flowReaches(flowData.FlowNode, candidate.flow, effective, map[*ast.FlowNode]bool{}) {
			return true
		}
	}
	return false
}

// flowReaches walks antecedents from a read back toward a write.
//
// The visited set is required rather than defensive. A loop label is its own transitive antecedent
// through the back-edge, so an unguarded walk does not terminate on any file containing a loop; the
// first probe that omitted this hung.
func flowReaches(from *ast.FlowNode, target *ast.FlowNode, blockers map[*ast.FlowNode]bool, visited map[*ast.FlowNode]bool) bool {
	if from == nil || visited[from] {
		return false
	}
	if from == target {
		return true
	}
	visited[from] = true

	// A later write to the same binding ends this path. Checked after the target comparison so a
	// write is never treated as blocking itself, and before the antecedents are followed so the walk
	// stops at the barrier rather than stepping over it.
	if blockers[from] {
		return false
	}

	// A label carries a list of antecedents rather than one, and every arm has to be tried: a read
	// after an if/else is reachable from a write in either branch, and missing one arm would call a
	// live write dead.
	if from.Antecedents != nil {
		for entry := from.Antecedents; entry != nil; entry = entry.Next {
			if flowReaches(entry.Flow, target, blockers, visited) {
				return true
			}
		}
		return false
	}
	return flowReaches(from.Antecedent, target, blockers, visited)
}

// writeTargetOf reports the identifier a write assigns to, when this identifier is that target.
//
// Only the shapes the flow graph annotates are accepted. A destructuring target and an update
// expression are both real writes that upstream reports, and both are declined here rather than
// guessed at, because the flow node they carry does not line up with the single-identifier
// reachability question this rule asks.
func writeTargetOf(identifier *ast.Node) (*ast.Node, bool) {
	parent := identifier.Parent
	if parent == nil {
		return nil, false
	}

	switch parent.Kind {
	case ast.KindBinaryExpression:
		binary := parent.AsBinaryExpression()
		if binary.OperatorToken == nil || !ast.IsAssignmentOperator(binary.OperatorToken.Kind) {
			return nil, false
		}
		if ast.SkipParentheses(binary.Left) != identifier {
			return nil, false
		}
		return identifier, true

	case ast.KindVariableDeclaration:
		// The initializer is a write like any other, and upstream reports it: one of its snapshot
		// diagnostics underlines `let v = 'unused';` itself. A declarator with no initializer stores
		// nothing and is not a candidate.
		declaration := parent.AsVariableDeclaration()
		if declaration.Name() != identifier || declaration.Initializer == nil {
			return nil, false
		}
		return identifier, true
	}

	return nil, false
}

// isPlainAssignmentTarget reports whether this identifier is written by `=` rather than by a
// compound operator.
//
// The distinction is which occurrences count as reads. `v = 1` does not read `v`, so it cannot keep
// an earlier write live. `v += 1` does read it, and treating the two alike would call the earlier
// write dead in `let v = 1; v += 1; f(v);`.
func isPlainAssignmentTarget(identifier *ast.Node) bool {
	parent := identifier.Parent
	if parent == nil || parent.Kind != ast.KindBinaryExpression {
		// A declarator's name is not a read of a previous value; there is none.
		return parent != nil && parent.Kind == ast.KindVariableDeclaration
	}
	binary := parent.AsBinaryExpression()
	return binary.OperatorToken != nil && binary.OperatorToken.Kind == ast.KindEqualsToken
}

// isLocalVariableDeclaration reports whether a declaration is a `let` or `var` this rule may judge.
//
// A `const` cannot be reassigned, so its initializer is the only write and reporting it would
// duplicate the unused-variable judgment rather than the dead-store one. A parameter, a function, a
// class, and an import are all declined for the same reason: the write this rule reasons about is a
// store into a mutable local.
func isLocalVariableDeclaration(declaration *ast.Node) bool {
	if declaration.Kind != ast.KindVariableDeclaration {
		return false
	}
	// The declarator sits inside a declaration list that carries the let/const/var flag.
	list := declaration.Parent
	if list == nil || list.Kind != ast.KindVariableDeclarationList {
		return false
	}
	if list.Flags&ast.NodeFlagsConst != 0 {
		return false
	}

	// An exported binding can be read by another module, so no write to it is provably dead. The
	// statement wrapping the list carries the modifier.
	statement := list.Parent
	if statement != nil && statement.Kind == ast.KindVariableStatement {
		if statement.ModifierFlags()&ast.ModifierFlagsExport != 0 {
			return false
		}
	}
	return true
}

// exportedNames collects every binding name the file hands out through any export form.
//
// The modifier check above sees `export let foo` and nothing else, which is the smaller half of the
// surface. A binding is equally exported by a later `export { foo }`, by `export { foo as bar }`,
// and by `export default foo`, and in every one of those the declaration carries no modifier at all.
// Upstream reads its module record's `exported_bindings` map, which is populated by all of them; we
// have no module record here, so the export clauses are walked directly.
//
// Collected in one pass and consulted by name, rather than walked per candidate. The per-candidate
// form was half of a 3.3 second cost on the real tree.
//
// Measured rather than reasoned: before this existed the rule reported
// `let foo = 'used'; export { foo }; console.log(foo); foo = 'unused like but exported';`, which is
// upstream clean case 15 and exists precisely to pin this. That was the last false positive on the
// imported corpus.
func exportedNames(sourceFile *ast.Node) map[string]bool {
	exported := map[string]bool{}
	var visit func(*ast.Node)
	visit = func(node *ast.Node) {
		if node == nil {
			return
		}
		switch node.Kind {
		case ast.KindExportSpecifier:
			specifier := node.AsExportSpecifier()
			// `export { foo }` names the local in `Name`; `export { foo as bar }` puts the local in
			// `PropertyName` and the public name in `Name`. The local is the one that matters,
			// because that is the binding writes here target.
			local := specifier.Name()
			if specifier.PropertyName != nil {
				local = specifier.PropertyName
			}
			if local != nil {
				exported[local.Text()] = true
			}
		case ast.KindExportAssignment:
			// `export default foo` and `export = foo` both hand the binding out whole.
			if expression := node.AsExportAssignment().Expression; expression != nil &&
				expression.Kind == ast.KindIdentifier {
				exported[expression.Text()] = true
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile)
	return exported
}

// isCapturedAcrossFunctions reports whether the binding is read from inside a different function
// than the one declaring it.
//
// This is the guard the flow graph forces. A function body begins its own graph, so a read inside a
// closure has no antecedent path to the enclosing writes and the inversion cannot see it. Without
// this every write preceding a closure that captures the variable would be reported, and those
// reports would be wrong.
//
// Deliberately coarse: any cross-function read silences every write to that binding, whether or not
// the closure could actually run after a given one. That trades false positives, which cost trust,
// for false negatives, which cost coverage.
func isCapturedAcrossFunctions(declaration *ast.Node, group []*ast.Node) bool {
	declaringFunction := enclosingFunctionLike(declaration)
	for _, identifier := range group {
		if enclosingFunctionLike(identifier) != declaringFunction {
			return true
		}
	}
	return false
}

// enclosingFunctionLike reports the nearest construct that starts its own flow graph.
//
// The source file counts, because top-level code has a graph of its own. The list is the set of
// bodies the binder gives a fresh `FlowFlagsStart`, so it is the boundary the reachability walk
// cannot cross rather than a general notion of scope.
func enclosingFunctionLike(node *ast.Node) *ast.Node {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		switch parent.Kind {
		case ast.KindFunctionDeclaration,
			ast.KindFunctionExpression,
			ast.KindArrowFunction,
			ast.KindMethodDeclaration,
			ast.KindGetAccessor,
			ast.KindSetAccessor,
			ast.KindConstructor,
			ast.KindClassStaticBlockDeclaration,
			ast.KindSourceFile:
			return parent
		}
	}
	return nil
}

// hasAnyRead reports whether the binding is read anywhere in the file.
//
// This is upstream's `is_used`, and it is checked before any write is examined rather than after.
// The distinction it draws is between two different judgments that look alike from the report site.
// A binding nothing reads has every write dead by definition, so without this guard the rule emits
// one finding per assignment to a variable whose real problem is that it is unused, which is
// `no-unused-vars`' judgment restated N times and stated worse. Upstream pins it with clean cases
// including `let v = 'used variable';`, which has no read at all and which this rule reported before
// the guard existed.
//
// A read is any occurrence of the symbol that is not the target of a plain assignment. A compound
// assignment target reads the previous value and so counts, matching how `isObservedAfter` treats
// the same node, because two places disagreeing about what a read is would make the guard and the
// liveness walk answer different questions.
func hasAnyRead(group []*ast.Node) bool {
	for _, identifier := range group {
		if target, isWrite := writeTargetOf(identifier); isWrite && target == identifier {
			if isPlainAssignmentTarget(identifier) {
				continue
			}
		}
		return true
	}
	return false
}

// enclosingAssignment reports the target identifier of the assignment whose right-hand side contains
// this read, when there is one.
//
// It answers a single question: is this read part of the value being stored by some write? If it is,
// that write cannot be a barrier for it, because the read happens first. Walking up to the nearest
// assignment is enough, since a read nested more deeply (`a = f(a + 1)`) still evaluates before the
// same store.
func enclosingAssignment(read *ast.Node) *ast.Node {
	for node := read; node != nil && node.Parent != nil; node = node.Parent {
		parent := node.Parent
		switch parent.Kind {
		case ast.KindBinaryExpression:
			binary := parent.AsBinaryExpression()
			// Deliberately not also requiring that `node` be the right operand. That condition was
			// written first and then removed as subsumed: a caller only reaches here with a read,
			// and a left-operand identifier is a write target that `writeTargetOf` has already
			// filtered out, while an identifier nested inside a left-hand member expression
			// (`w[v] = 2`) belongs to a write that never enters the blocker set, so the exemption
			// it would receive changes nothing. A mutant forcing the condition true survived, and
			// eight constructed inputs covering parenthesized, computed-member, self-assigning and
			// call-target left sides could not distinguish the two. Keeping an unreachable
			// discrimination would be a line asserting nothing.
			if binary.OperatorToken != nil && ast.IsAssignmentOperator(binary.OperatorToken.Kind) {
				if left := ast.SkipParentheses(binary.Left); left != nil && left.Kind == ast.KindIdentifier {
					return left
				}
			}
		case ast.KindVariableDeclaration:
			declaration := parent.AsVariableDeclaration()
			if declaration.Initializer == node {
				if name := declaration.Name(); name != nil && name.Kind == ast.KindIdentifier {
					return name
				}
			}
		}
		// A function boundary ends the search: a read inside a callback is not the stored value.
		switch parent.Kind {
		case ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindFunctionDeclaration:
			return nil
		}
	}
	return nil
}

// isInsideTryBlock reports whether a node sits in the `try` block of a try statement.
//
// Upstream refuses to report any write inside a try, under the name `is_in_try_block`, and the
// reason is that a try block has an edge out of every operation rather than only its end. A write
// there may be abandoned by a throw from the next line, so the value it overwrote is still
// observable on the catch and finally paths, and calling it dead is wrong. Three of upstream's clean
// cases are exactly this shape, including one where the try nests two deep.
//
// The catch and finally clauses are not covered: code there runs after the throw has been handled,
// so it has ordinary flow and an ordinary dead store is a real one.
func isInsideTryBlock(node *ast.Node) bool {
	for child, parent := node, node.Parent; parent != nil; child, parent = parent, parent.Parent {
		if parent.Kind == ast.KindTryStatement && parent.AsTryStatement().TryBlock == child {
			return true
		}
	}
	return false
}

// resolveOccurrence reports the binding an identifier occurrence refers to.
//
// Plain `GetSymbolAtLocation` is wrong for one shape and only one: a shorthand property. In
// `return { v }` the identifier resolves to the *property's* symbol rather than to the variable it
// reads, so the read is filed under a symbol nothing else touches and the variable looks unread.
// The effect is not a missed finding but a false positive, because a write whose only read is a
// shorthand property then looks dead.
//
// Found on real code rather than on the corpus, which contains no shorthand at all. The dry run
// reported 50 findings in one file, and every one of them traced to
// `let newStartTime: Date; switch (...) { ... newStartTime = ...; } return { newStartTime, ... }`.
// Reading those findings is what surfaced it; the imported corpus was green throughout.
func resolveOccurrence(ctx rule.Context, identifier *ast.Node) *ast.Symbol {
	if parent := identifier.Parent; parent != nil && parent.Kind == ast.KindShorthandPropertyAssignment {
		if symbol := ctx.TypeChecker.GetShorthandAssignmentValueSymbol(parent); symbol != nil {
			return symbol
		}
	}
	return ctx.TypeChecker.GetSymbolAtLocation(identifier)
}

// isNonReferenceIdentifier reports whether an identifier occurrence cannot be a reference to a local
// variable, so it need never be resolved or scanned.
//
// A property name in `o.v` or `{ v: 1 }` shares its spelling with a variable and resolves to a
// different symbol, so keeping it was never wrong, only wasteful. A shorthand property is
// deliberately absent from this list: it looks like a property name and is a real read, which is the
// distinction `resolveOccurrence` exists to draw.
func isNonReferenceIdentifier(identifier *ast.Node) bool {
	parent := identifier.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindPropertyAccessExpression:
		// `o.v` reads `o` and names `v`; only the name half is skipped.
		return parent.AsPropertyAccessExpression().Name() == identifier
	case ast.KindPropertyAssignment:
		return parent.AsPropertyAssignment().Name() == identifier
	case ast.KindQualifiedName, ast.KindMethodDeclaration, ast.KindPropertyDeclaration,
		ast.KindPropertySignature, ast.KindMethodSignature, ast.KindGetAccessor,
		ast.KindSetAccessor, ast.KindEnumMember, ast.KindImportSpecifier,
		ast.KindImportClause, ast.KindNamespaceImport, ast.KindTypeParameter,
		ast.KindTypeReference, ast.KindJsxAttribute:
		return parent.Name() == identifier
	}
	return false
}
