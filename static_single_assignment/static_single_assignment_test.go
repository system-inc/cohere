package static_single_assignment_test

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	ssa "github.com/system-inc/cohere/static_single_assignment"
)

// The passes are tested here on an IR of the tests' own, as small as Graph allows, so the module
// proves itself without either real IR. cohere's high-level IR and its React rules test the same
// passes through cohere's adapter, and Adamic's suite through its own.

// testPlace carries a tag beside its value, standing in for what a real IR's place carries (cohere's
// effect, reactivity and range), so a test can see it ride along through renaming.
type testPlace struct {
	id  ssa.IdentifierId
	tag string
}

type testInstruction struct {
	uses, defines []testPlace
	contextStore  bool
	// result is the definition a context store's own result is, the one the verifier counts.
	result ssa.IdentifierId
	order  ssa.EvaluationOrder
}

type testEdge struct {
	to   ssa.BlockId
	kind ssa.Edge
}

type testBlock struct {
	id            ssa.BlockId
	instructions  []*testInstruction
	edges         []testEdge
	returns       bool
	predecessors  []ssa.BlockId
	phis          []*ssa.Phi[testPlace]
	terminalOrder ssa.EvaluationOrder
	placeholder   bool
}

type testIdentifier struct {
	declaration ssa.DeclarationId
	name        string
}

type testFunction struct {
	entry       ssa.BlockId
	blocks      []*testBlock
	table       map[ssa.BlockId]*testBlock
	next        ssa.BlockId
	identifiers []testIdentifier
	params      []testPlace
	returns     *testPlace
	contextual  map[ssa.DeclarationId]bool
	// variables maps a name to its declaration, for building.
	variables map[string]ssa.DeclarationId
}

func newTestFunction() *testFunction {
	function := &testFunction{
		table:      map[ssa.BlockId]*testBlock{},
		next:       1,
		contextual: map[ssa.DeclarationId]bool{},
		variables:  map[string]ssa.DeclarationId{},
	}
	// Identifier 0 is never a variable's, as in both real IRs.
	function.identifiers = append(function.identifiers, testIdentifier{})
	return function
}

func (function *testFunction) block() *testBlock {
	block := &testBlock{id: function.next}
	function.next++
	function.blocks = append(function.blocks, block)
	function.table[block.id] = block
	return block
}

// value mints a new pre-assignment value of a variable, as lowering mints one per store and per read.
func (function *testFunction) value(name string) testPlace {
	declaration, ok := function.variables[name]
	if !ok {
		declaration = ssa.DeclarationId(len(function.variables) + 1)
		function.variables[name] = declaration
	}
	id := ssa.IdentifierId(len(function.identifiers))
	function.identifiers = append(function.identifiers, testIdentifier{declaration: declaration, name: name})
	return testPlace{id: id, tag: name}
}

// define appends an instruction that stores a new value of the variable name.
//
// Mutates: block a test builds its function by appending instructions to the blocks it made
func (function *testFunction) define(block *testBlock, name string) *testInstruction {
	instruction := &testInstruction{defines: []testPlace{function.value(name)}}
	block.instructions = append(block.instructions, instruction)
	return instruction
}

// use appends an instruction that reads the variable name.
//
// Mutates: block a test builds its function by appending instructions to the blocks it made
func (function *testFunction) use(block *testBlock, name string) *testInstruction {
	instruction := &testInstruction{uses: []testPlace{function.value(name)}}
	block.instructions = append(block.instructions, instruction)
	return instruction
}

type testGraph struct{}

func (testGraph) Entry(function *testFunction) ssa.BlockId { return function.entry }
func (testGraph) BlockBound(function *testFunction) int    { return int(function.next) }
func (testGraph) Block(function *testFunction, id ssa.BlockId) (*testBlock, bool) {
	block, ok := function.table[id]
	return block, ok
}
func (testGraph) Blocks(function *testFunction) []*testBlock { return function.blocks }

// Mutates: function the passes hand their new block order back through this setter, as through a real IR's
func (testGraph) SetBlocks(function *testFunction, blocks []*testBlock) { function.blocks = blocks }
func (testGraph) Retain(function *testFunction, keep func(ssa.BlockId) bool) {
	for id := range function.table {
		if !keep(id) {
			delete(function.table, id)
		}
	}
}
func (testGraph) Placeholder(function *testFunction, block *testBlock) *testBlock {
	placeholder := &testBlock{id: block.id, placeholder: true,
		predecessors: append([]ssa.BlockId(nil), block.predecessors...)}
	function.table[block.id] = placeholder
	return placeholder
}
func (testGraph) Id(block *testBlock) ssa.BlockId             { return block.id }
func (testGraph) Predecessors(block *testBlock) []ssa.BlockId { return block.predecessors }

// Mutates: block the passes record its recomputed predecessors through this setter, as through a real IR's
func (testGraph) SetPredecessors(block *testBlock, predecessors []ssa.BlockId) {
	block.predecessors = predecessors
}
func (testGraph) Phis(block *testBlock) []*ssa.Phi[testPlace] { return block.phis }

// Mutates: block the passes record its phis through this setter, as through a real IR's
func (testGraph) SetPhis(block *testBlock, phis []*ssa.Phi[testPlace]) { block.phis = phis }
func (testGraph) EachEdge(block *testBlock, visit func(ssa.BlockId, ssa.Edge)) {
	for _, edge := range block.edges {
		visit(edge.to, edge.kind)
	}
}
func (testGraph) EndsInReturn(block *testBlock) bool { return block.returns }
func (testGraph) InstructionCount(function *testFunction, block *testBlock) int {
	return len(block.instructions)
}
func (testGraph) EachInstructionPlace(function *testFunction, block *testBlock, index int,
	visit func(*testPlace, ssa.Role)) {
	instruction := block.instructions[index]
	for index := range instruction.uses {
		visit(&instruction.uses[index], ssa.Use)
	}
	for index := range instruction.defines {
		visit(&instruction.defines[index], ssa.Define)
	}
}
func (testGraph) IsContextStore(function *testFunction, block *testBlock, index int) bool {
	return block.instructions[index].contextStore
}
func (testGraph) ContextStoreDefines(function *testFunction, block *testBlock, index int, place testPlace) bool {
	return place.id == block.instructions[index].result
}
func (testGraph) SetInstructionOrder(function *testFunction, block *testBlock, index int, order ssa.EvaluationOrder) {
	block.instructions[index].order = order
}
func (testGraph) EachTerminalPlace(block *testBlock, visit func(*testPlace, ssa.Role)) {
	// A test block's terminal is its edges and a return flag, which hold no place to visit.
}

// Mutates: block the passes record its terminal's evaluation order through this setter, as through a real IR's
func (testGraph) SetTerminalOrder(block *testBlock, order ssa.EvaluationOrder) {
	block.terminalOrder = order
}
func (testGraph) Params(function *testFunction) []testPlace { return function.params }
func (testGraph) Returns(function *testFunction) *testPlace { return function.returns }
func (testGraph) Declaration(function *testFunction, id ssa.IdentifierId) ssa.DeclarationId {
	return function.identifiers[id].declaration
}
func (testGraph) Contextual(function *testFunction, declaration ssa.DeclarationId) bool {
	return function.contextual[declaration]
}

// Mutates: function construction mints each renamed value through this, which records it in the test function's table
func (testGraph) Mint(function *testFunction, original ssa.IdentifierId) ssa.IdentifierId {
	source := function.identifiers[original]
	id := ssa.IdentifierId(len(function.identifiers))
	function.identifiers = append(function.identifiers, source)
	return id
}
func (testGraph) Named(function *testFunction, id ssa.IdentifierId) bool {
	return function.identifiers[id].name != ""
}
func (testGraph) PlaceString(function *testFunction, id ssa.IdentifierId) string {
	return fmt.Sprintf("%s$%d", function.identifiers[id].name, id)
}
func (testGraph) IdentifierOf(place testPlace) ssa.IdentifierId { return place.id }
func (testGraph) WithIdentifier(place testPlace, id ssa.IdentifierId) testPlace {
	place.id = id
	return place
}

// finalize and construct are the sequence both real IRs run.
func finalize(function *testFunction) {
	ssa.ReversePostorder(testGraph{}, function)
	ssa.MarkPredecessors(testGraph{}, function)
	ssa.MarkEvaluationOrder(testGraph{}, function)
}

func construct(test *testing.T, function *testFunction) {
	test.Helper()
	finalize(function)
	ssa.Construct(testGraph{}, function)
	if violations := ssa.VerifySSA(testGraph{}, function); len(violations) != 0 {
		test.Fatalf("not in single assignment form: %v", violations)
	}
}

// goTo adds a real edge from one block to each of the others.
//
// Mutates: from a test builds its function by adding edges to the blocks it made
func goTo(from *testBlock, to ...*testBlock) {
	for _, block := range to {
		from.edges = append(from.edges, testEdge{to: block.id, kind: ssa.Real})
	}
}

// TestDiamondPlacesOnePhiAtTheJoin: x is written before an if and on one arm, so the join merges two
// values, and the read after it names the phi.
func TestDiamondPlacesOnePhiAtTheJoin(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, left, right, join := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	function.define(entry, "x")
	function.define(left, "x")
	read := function.use(join, "x")
	goTo(entry, left, right)
	goTo(left, join)
	goTo(right, join)

	construct(test, function)

	if len(join.phis) != 1 {
		test.Fatalf("join has %d phis, want 1", len(join.phis))
	}
	phi := join.phis[0]
	if got := ssa.PhiOperandsInOrder(phi); !slices.Equal(got, []ssa.BlockId{left.id, right.id}) {
		test.Errorf("operands from %v, want [%d %d]", got, left.id, right.id)
	}
	if read.uses[0].id != phi.Place.id {
		test.Errorf("the read after the join names $%d, want the phi's $%d", read.uses[0].id, phi.Place.id)
	}
	if left.instructions[0].defines[0].id == entry.instructions[0].defines[0].id {
		test.Errorf("the two writes of x share a value")
	}
	// The place's other fields ride along on the phi and its operands.
	for _, operand := range phi.Operands {
		if operand.Place.tag != "x" {
			test.Errorf("operand from bb%d lost its tag: %+v", operand.Predecessor, operand.Place)
		}
	}
	if phi.Place.tag != "x" {
		test.Errorf("phi lost its tag: %+v", phi.Place)
	}
}

// TestLoopPhiTakesTheBackEdge: i is written before a loop and in its body, so the header merges the
// entry's value with the back edge's, an incomplete phi filled when the body is sealed.
func TestLoopPhiTakesTheBackEdge(t *testing.T) {
	t.Parallel()
	function := newTestFunction()
	entry, header, body, exit := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	function.define(entry, "i")
	test := function.use(header, "i")
	function.define(body, "i")
	after := function.use(exit, "i")
	goTo(entry, header)
	goTo(header, body, exit)
	goTo(body, header)

	construct(t, function)

	if len(header.phis) != 1 {
		t.Fatalf("header has %d phis, want 1", len(header.phis))
	}
	phi := header.phis[0]
	if got := phi.Operands.At(body.id).id; got != body.instructions[0].defines[0].id {
		t.Errorf("the back edge's operand is $%d, want the body's write $%d", got, body.instructions[0].defines[0].id)
	}
	if test.uses[0].id != phi.Place.id || after.uses[0].id != phi.Place.id {
		t.Errorf("reads in and after the loop don't name the header's phi")
	}
}

// TestLoopThatNeverWritesLeavesNoPhi: the header's incomplete phi takes the entry's value and its own
// result, which is redundant, and elimination removes it and rewrites its reads.
func TestLoopThatNeverWritesLeavesNoPhi(t *testing.T) {
	t.Parallel()
	function := newTestFunction()
	entry, header, body, exit := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	function.define(entry, "i")
	test := function.use(header, "i")
	function.use(body, "i")
	goTo(entry, header)
	goTo(header, body, exit)
	goTo(body, header)

	construct(t, function)

	if len(header.phis) != 0 {
		t.Fatalf("header kept %d phis, want none", len(header.phis))
	}
	if want := entry.instructions[0].defines[0].id; test.uses[0].id != want {
		t.Errorf("the loop's read names $%d, want the entry's write $%d", test.uses[0].id, want)
	}
}

// TestUnreachableBlockIsDropped: a block no edge reaches leaves the slice and the table.
func TestUnreachableBlockIsDropped(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, exit, orphan := function.block(), function.block(), function.block()
	function.entry = entry.id
	goTo(entry, exit)
	goTo(orphan, exit)

	finalize(function)

	for _, block := range function.blocks {
		if block.id == orphan.id {
			test.Fatalf("the unreachable block is still in the slice")
		}
	}
	if _, ok := (testGraph{}).Block(function, orphan.id); ok {
		test.Errorf("the unreachable block is still in the table")
	}
	if !slices.Equal(exit.predecessors, []ssa.BlockId{entry.id}) {
		test.Errorf("exit's predecessors are %v, want only the entry", exit.predecessors)
	}
}

// TestFallthroughNothingReachesBecomesAPlaceholder: a structured terminal names a continuation no
// real edge reaches, so it stays, empty, and is nobody's successor.
func TestFallthroughNothingReachesBecomesAPlaceholder(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, body, continuation := function.block(), function.block(), function.block()
	function.entry = entry.id
	entry.edges = []testEdge{{to: continuation.id, kind: ssa.Fallthrough}, {to: body.id, kind: ssa.Real}}
	body.returns = true

	finalize(function)

	var kept *testBlock
	for _, block := range function.blocks {
		if block.id == continuation.id {
			kept = block
		}
	}
	if kept == nil || !kept.placeholder {
		test.Fatalf("the fallthrough is %+v, want a placeholder in the slice", kept)
	}
	if len(kept.predecessors) != 0 {
		test.Errorf("a fallthrough made a predecessor: %v", kept.predecessors)
	}
	if !slices.Equal(body.predecessors, []ssa.BlockId{entry.id}) {
		test.Errorf("body's predecessors are %v", body.predecessors)
	}
}

// TestContextStoreReusesItsDefinition: a binding a closure captures is defined once, and a second
// store to it in the same block names the same value, which the verifier accepts on the store's own
// result alone.
func TestContextStoreReusesItsDefinition(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry := function.block()
	function.entry = entry.id
	first := function.define(entry, "n")
	first.contextStore = true
	function.contextual[function.variables["n"]] = true
	second := function.define(entry, "n")
	second.contextStore = true

	construct(test, function)

	if first.defines[0].id != second.defines[0].id {
		test.Errorf("the context stores define $%d and $%d, want one value", first.defines[0].id, second.defines[0].id)
	}
}

// TestExceptionalEdgeIsAnEdge: MayThrow's handler is reached by an exceptional edge, which is a real
// predecessor today, so the handler's read resolves through it.
//
// It resolves to the block's END: x$2, written by the throwing instruction itself, which never
// completed when the handler runs. That is #2yz9ra9. Its fix belongs in valueFromPredecessor, where
// every lookup across an edge now goes, and flips the want below to the write before the throw.
func TestExceptionalEdgeIsAnEdge(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, next, handler := function.block(), function.block(), function.block()
	function.entry = entry.id
	before := function.define(entry, "x")
	throwing := function.define(entry, "x")
	read := function.use(handler, "x")
	entry.edges = []testEdge{{to: next.id, kind: ssa.Real}, {to: handler.id, kind: ssa.Exceptional}}

	construct(test, function)

	if !slices.Equal(handler.predecessors, []ssa.BlockId{entry.id}) {
		test.Fatalf("the handler's predecessors are %v, want the throwing block", handler.predecessors)
	}
	if got, want := read.uses[0].id, throwing.defines[0].id; got != want {
		test.Errorf("the handler reads $%d, want $%d (the end of the block, #2yz9ra9; $%d once fixed)",
			got, want, before.defines[0].id)
	}
}

// TestEvaluationOrderFollowsTheSlice: instructions and terminals are numbered from 1 in reverse
// postorder, with nothing left at zero.
func TestEvaluationOrderFollowsTheSlice(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, exit := function.block(), function.block()
	function.entry = entry.id
	function.define(entry, "a")
	function.use(exit, "a")
	goTo(entry, exit)

	finalize(function)

	var orders []ssa.EvaluationOrder
	for _, block := range function.blocks {
		for _, instruction := range block.instructions {
			orders = append(orders, instruction.order)
		}
		orders = append(orders, block.terminalOrder)
	}
	if !slices.Equal(orders, []ssa.EvaluationOrder{1, 2, 3, 4}) {
		test.Errorf("orders are %v, want [1 2 3 4]", orders)
	}
}

// TestVerifierCatchesAnUndominatedUse: a hand-broken function, a read on one arm of a diamond naming
// a value written on the other, is a violation, so a clean result elsewhere is a real one.
func TestVerifierCatchesAnUndominatedUse(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, left, right := function.block(), function.block(), function.block()
	function.entry = entry.id
	write := function.define(left, "x")
	read := function.use(right, "x")
	read.uses[0].id = write.defines[0].id
	goTo(entry, left, right)
	finalize(function)

	violations := ssa.VerifySSA(testGraph{}, function)
	if len(violations) != 1 || violations[0].Kind != ssa.SSAViolationUseNotDominated {
		test.Fatalf("violations are %v, want one undominated use", violations)
	}
	if stats := ssa.CollectSSAStats(testGraph{}, function); stats.Uses != 1 || stats.NamedValues != 1 {
		test.Errorf("stats are %+v, want one use and one named value", stats)
	}
}

// TestDominanceIsTheEntrysAndTheJoins: the entry dominates every block, a branch arm dominates
// neither the other arm nor the join.
func TestDominanceIsTheEntrysAndTheJoins(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, left, right, join := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	goTo(entry, left, right)
	goTo(left, join)
	goTo(right, join)
	finalize(function)

	dominance := ssa.ComputeDominance(testGraph{}, function)
	for _, block := range []*testBlock{entry, left, right, join} {
		if !dominance.Dominates(entry.id, block.id) {
			test.Errorf("the entry does not dominate bb%d", block.id)
		}
	}
	if dominance.Dominates(left.id, join.id) || dominance.Dominates(left.id, right.id) {
		test.Errorf("an arm dominates the join or the other arm")
	}
}

// TestDominanceFollowsRealEdgesPastAFallthrough: a structural fallthrough reaches d before the back edge
// does, so reverse postorder puts d ahead of every real predecessor it has. Every path to d runs a, b
// and c, so all three dominate it. Over the block slice, dominance said only the entry and a did
// (#6v4a54x, Adamic's generated-631).
func TestDominanceFollowsRealEdgesPastAFallthrough(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, a, b, c, d := function.block(), function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	goTo(entry, a)
	a.edges = []testEdge{{to: d.id, kind: ssa.Fallthrough}, {to: b.id, kind: ssa.Real}}
	goTo(b, a, c)
	goTo(c, d)
	goTo(d, d, b)
	finalize(function)

	var order []ssa.BlockId
	for _, block := range function.blocks {
		order = append(order, block.id)
	}
	if !slices.Equal(order, []ssa.BlockId{entry.id, a.id, d.id, b.id, c.id}) {
		test.Fatalf("the block order is %v; the test needs d ahead of its real predecessors", order)
	}
	dominance := ssa.ComputeDominance(testGraph{}, function)
	for _, dominator := range []*testBlock{entry, a, b, c, d} {
		if !dominance.Dominates(dominator.id, d.id) {
			test.Errorf("bb%d doesn't dominate bb%d, which every path reaches through it", dominator.id, d.id)
		}
	}
}

// TestDominanceAgreesWithSetIntersection: over thousands of generated graphs, with fallthroughs,
// exceptional edges, self loops and edges to blocks that aren't there, ComputeDominance answers every
// pair of blocks as dominance computed the plain way does: each block's dominators are itself and the
// intersection of its reachable predecessors', to a fixed point. The graphs have to reach the shape
// #6v4a54x was, a block reached by real edges with none of its real predecessors ahead of it in the
// block slice, or the comparison couldn't see that bug.
func TestDominanceAgreesWithSetIntersection(test *testing.T) {
	test.Parallel()
	random := rand.New(rand.NewSource(20261007))
	const graphs = 4000
	behind := 0
	for graph := 0; graph < graphs; graph++ {
		function := newTestFunction()
		count := 1 + random.Intn(12)
		blocks := make([]*testBlock, 0, count)
		for index := 0; index < count; index++ {
			blocks = append(blocks, function.block())
		}
		function.entry = blocks[0].id
		target := func() ssa.BlockId {
			if random.Intn(25) == 0 {
				return ssa.BlockId(int(function.next) + 3)
			}
			return blocks[random.Intn(len(blocks))].id
		}
		for _, block := range blocks {
			if random.Intn(4) == 0 {
				block.edges = append(block.edges, testEdge{to: target(), kind: ssa.Fallthrough})
			}
			for edges := random.Intn(3); edges > 0; edges-- {
				block.edges = append(block.edges, testEdge{to: target(), kind: ssa.Real})
			}
			if random.Intn(10) == 0 {
				block.edges = append(block.edges, testEdge{to: target(), kind: ssa.Exceptional})
			}
		}
		finalize(function)

		want := setIntersectionDominators(function)
		dominance := ssa.ComputeDominance(testGraph{}, function)
		for _, block := range function.blocks {
			for _, dominator := range function.blocks {
				if got := dominance.Dominates(dominator.id, block.id); got != want[block.id][dominator.id] {
					test.Fatalf("graph %d: Dominates(bb%d, bb%d) is %t, set intersection says %t\n%s",
						graph, dominator.id, block.id, got, !got, describeTestGraph(function))
				}
			}
		}
		if reachedFromBehind(function) {
			behind++
		}
	}
	if behind == 0 {
		test.Fatalf("none of the %d graphs puts a reached block ahead of all its real predecessors", graphs)
	}
	test.Logf("%d graphs, %d of them with a reached block ahead of all its real predecessors", graphs, behind)
}

// reachedBlocks is the blocks the entry reaches by real edges.
func reachedBlocks(function *testFunction) map[ssa.BlockId]bool {
	reached := map[ssa.BlockId]bool{}
	var walk func(id ssa.BlockId)
	walk = func(id ssa.BlockId) {
		block, ok := function.table[id]
		if !ok || reached[id] {
			return
		}
		reached[id] = true
		for _, edge := range block.edges {
			if edge.kind != ssa.Fallthrough {
				walk(edge.to)
			}
		}
	}
	walk(function.entry)
	return reached
}

// setIntersectionDominators is dominance computed the plain way, over the real predecessors
// MarkPredecessors set: the entry dominates itself, a block the entry reaches is dominated by itself and
// every block that dominates all its reached predecessors, and a block it doesn't reach only by itself.
func setIntersectionDominators(function *testFunction) map[ssa.BlockId]map[ssa.BlockId]bool {
	reached := reachedBlocks(function)
	dominators := map[ssa.BlockId]map[ssa.BlockId]bool{}
	for _, block := range function.blocks {
		set := map[ssa.BlockId]bool{block.id: true}
		if reached[block.id] && block.id != function.entry {
			for _, other := range function.blocks {
				set[other.id] = true
			}
		}
		dominators[block.id] = set
	}
	for changed := true; changed; {
		changed = false
		for _, block := range function.blocks {
			if !reached[block.id] || block.id == function.entry {
				continue
			}
			var next map[ssa.BlockId]bool
			for _, predecessor := range block.predecessors {
				if !reached[predecessor] {
					continue
				}
				if next == nil {
					next = map[ssa.BlockId]bool{}
					for id := range dominators[predecessor] {
						next[id] = true
					}
					continue
				}
				for id := range next {
					if !dominators[predecessor][id] {
						delete(next, id)
					}
				}
			}
			if next == nil {
				next = map[ssa.BlockId]bool{}
			}
			next[block.id] = true
			if len(next) != len(dominators[block.id]) {
				dominators[block.id] = next
				changed = true
			}
		}
	}
	return dominators
}

// reachedFromBehind reports whether a block the entry reaches, other than the entry, sits in the block
// slice ahead of every real predecessor it has.
func reachedFromBehind(function *testFunction) bool {
	reached := reachedBlocks(function)
	position := map[ssa.BlockId]int{}
	for index, block := range function.blocks {
		position[block.id] = index
	}
	for index, block := range function.blocks {
		if block.id == function.entry || !reached[block.id] {
			continue
		}
		ahead := false
		for _, predecessor := range block.predecessors {
			if at, ok := position[predecessor]; ok && at < index {
				ahead = true
			}
		}
		if !ahead {
			return true
		}
	}
	return false
}

// describeTestGraph renders a function's blocks, edges and predecessors for a failure message.
func describeTestGraph(function *testFunction) string {
	var out strings.Builder
	fmt.Fprintf(&out, "entry bb%d\n", function.entry)
	for _, block := range function.blocks {
		fmt.Fprintf(&out, "bb%d predecessors %v edges", block.id, block.predecessors)
		for _, edge := range block.edges {
			fmt.Fprintf(&out, " %d:%d", edge.to, edge.kind)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// TestAnEntryWithPredecessorsIsReportedAndRefused: an edge into the entry breaks Graph.Entry's rule.
// VerifySSA says so, and Construct refuses with a panic that names the block. Without the refusal, the
// read of x in c, which nothing defines, looks back through b, a and the entry, each with one sealed
// predecessor, and around again until the stack runs out.
func TestAnEntryWithPredecessorsIsReportedAndRefused(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, a, b, c := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	function.use(c, "x")
	goTo(entry, a)
	goTo(a, b)
	goTo(b, entry, c)
	finalize(function)

	violations := ssa.VerifySSA(testGraph{}, function)
	if len(violations) == 0 ||
		violations[0].Kind != ssa.SSAViolationEntryHasPredecessors ||
		violations[0].Block != entry.id {
		test.Fatalf("violations are %v, want the entry's predecessors first", violations)
	}

	refusal := func() (message string) {
		defer func() {
			message = fmt.Sprint(recover())
		}()
		ssa.Construct(testGraph{}, function)
		return "no refusal"
	}()
	if want := fmt.Sprintf(
		"the entry block bb%d has predecessors [%d]",
		entry.id,
		b.id,
	); !strings.Contains(refusal, want) {
		test.Fatalf("Construct gives %q, want a refusal naming %q", refusal, want)
	}
}
