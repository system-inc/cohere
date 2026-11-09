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

type testGraph struct{}

func (testGraph) Entry(function *testFunction) ssa.BlockId { return function.entry }
func (testGraph) BlockBound(function *testFunction) int {
	if function.boundUnderReported {
		return 0
	}
	return int(function.next)
}
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
func (testGraph) Parameters(function *testFunction) []testPlace { return function.parameters }
func (testGraph) Returns(function *testFunction) *testPlace     { return function.returns }
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
	if violations := ssa.VerifyStaticSingleAssignment(testGraph{}, function); len(violations) != 0 {
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
// TestConstructionReachesBlocksPastTheBound builds the diamond with BlockBound answering zero: the builder's
// per-block state, dense by BlockId, grows to each block it meets rather than reading past its end.
func TestConstructionReachesBlocksPastTheBound(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	function.boundUnderReported = true
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
	if read.uses[0].id != join.phis[0].Place.id {
		test.Errorf("the read after the join names $%d, want the phi's $%d", read.uses[0].id,
			join.phis[0].Place.id)
	}
}

func TestLoopPhiTakesTheBackEdge(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, header, body, exit := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	function.define(entry, "i")
	headerRead := function.use(header, "i")
	function.define(body, "i")
	after := function.use(exit, "i")
	goTo(entry, header)
	goTo(header, body, exit)
	goTo(body, header)

	construct(test, function)

	if len(header.phis) != 1 {
		test.Fatalf("header has %d phis, want 1", len(header.phis))
	}
	phi := header.phis[0]
	if got := phi.Operands.At(body.id).id; got != body.instructions[0].defines[0].id {
		test.Errorf(
			"the back edge's operand is $%d, want the body's write $%d",
			got,
			body.instructions[0].defines[0].id,
		)
	}
	if headerRead.uses[0].id != phi.Place.id || after.uses[0].id != phi.Place.id {
		test.Errorf("reads in and after the loop don't name the header's phi")
	}
}

// TestLoopThatNeverWritesLeavesNoPhi: the header's incomplete phi takes the entry's value and its own
// result, which is redundant, and elimination removes it and rewrites its reads.
func TestLoopThatNeverWritesLeavesNoPhi(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, header, body, exit := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	function.define(entry, "i")
	headerRead := function.use(header, "i")
	function.use(body, "i")
	goTo(entry, header)
	goTo(header, body, exit)
	goTo(body, header)

	construct(test, function)

	if len(header.phis) != 0 {
		test.Fatalf("header kept %d phis, want none", len(header.phis))
	}
	if want := entry.instructions[0].defines[0].id; headerRead.uses[0].id != want {
		test.Errorf("the loop's read names $%d, want the entry's write $%d", headerRead.uses[0].id, want)
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

// TestExceptionalEdgeIsAnEdge: MayThrow's handler is reached by an exceptional edge, which is a
// predecessor like any other, but the edge leaves before the throwing instruction's own definition. So the
// handler reads x as it was written before the throw, x$1, never x$2, which the throwing instruction makes
// only if it doesn't throw (#2yz9ra9, #js7tdfg). The continuation, reached by the Real edge, reads x$2.
func TestExceptionalEdgeIsAnEdge(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, next, handler := function.block(), function.block(), function.block()
	function.entry = entry.id
	before := function.define(entry, "x")
	throwing := function.define(entry, "x")
	read := function.use(handler, "x")
	after := function.use(next, "x")
	entry.edges = []testEdge{{to: next.id, kind: ssa.Real}, {to: handler.id, kind: ssa.Exceptional}}

	construct(test, function)

	if !slices.Equal(handler.predecessors, []ssa.BlockId{entry.id}) {
		test.Fatalf("the handler's predecessors are %v, want the throwing block", handler.predecessors)
	}
	if got, want := read.uses[0].id, before.defines[0].id; got != want {
		test.Errorf("the handler reads $%d, want the write before the throw $%d, not the throw's own $%d",
			got, want, throwing.defines[0].id)
	}
	if got, want := after.uses[0].id, throwing.defines[0].id; got != want {
		test.Errorf("the continuation reads $%d, want the throwing instruction's write $%d", got, want)
	}
}

// TestHandlerReadsWhatTheThrowingBlockWasHanded: the throwing instruction is the block's only write of x,
// so what the handler is owed is x on entry to the block. That is a phi at the join the block sits after,
// looked up only when the handler reads it, and the throwing block's own read after its write names the
// write, not the phi.
func TestHandlerReadsWhatTheThrowingBlockWasHanded(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, left, right, join := function.block(), function.block(), function.block(), function.block()
	next, handler := function.block(), function.block()
	function.entry = entry.id
	function.define(left, "x")
	function.define(right, "x")
	throwing := function.define(join, "x")
	read := function.use(handler, "x")
	after := function.use(next, "x")
	goTo(entry, left, right)
	goTo(left, join)
	goTo(right, join)
	join.edges = []testEdge{{to: next.id, kind: ssa.Real}, {to: handler.id, kind: ssa.Exceptional}}

	construct(test, function)

	if len(join.phis) != 1 {
		test.Fatalf("the join has %d phis, want the one the handler's read needs", len(join.phis))
	}
	if got, want := read.uses[0].id, join.phis[0].Place.id; got != want {
		test.Errorf("the handler reads $%d, want x on entry to the throwing block, its phi $%d", got, want)
	}
	if got, want := after.uses[0].id, throwing.defines[0].id; got != want {
		test.Errorf("the continuation reads $%d, want the throwing instruction's write $%d", got, want)
	}
}

// TestAThrowsWriteReachesAJoinAfterIt: the throwing block's continuation is a join, also reached straight
// from the entry, so its phi takes the throw's write on the edge from the throwing block. The verifier
// holds that write from the top of the continuation, and the phi's operand on that one edge is the edge
// that carries it, which is clean, as construct's own verification requires.
func TestAThrowsWriteReachesAJoinAfterIt(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, throwingBlock, join, handler := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	written := function.define(entry, "x")
	throwing := function.define(throwingBlock, "x")
	read := function.use(join, "x")
	goTo(entry, throwingBlock, join)
	throwingBlock.edges = []testEdge{{to: join.id, kind: ssa.Real}, {to: handler.id, kind: ssa.Exceptional}}

	construct(test, function)

	if len(join.phis) != 1 || read.uses[0].id != join.phis[0].Place.id {
		test.Fatalf("the join's read names $%d and it has %d phis, want its one phi", read.uses[0].id, len(join.phis))
	}
	operands := join.phis[0].Operands
	if operands.At(throwingBlock.id).id != throwing.defines[0].id || operands.At(entry.id).id != written.defines[0].id {
		test.Errorf("the join's phi takes $%d from the throwing block and $%d from the entry, want $%d and $%d",
			operands.At(throwingBlock.id).id, operands.At(entry.id).id, throwing.defines[0].id, written.defines[0].id)
	}
}

// TestVerifierCatchesAHandlerReadingTheThrowsOwnWrite: the throwing instruction's definition holds from the
// top of its continuation, so a handler naming it reads a value the throw never made, which the verifier
// reports, as it reports any use its definition doesn't reach. The continuation's read of it is clean.
func TestVerifierCatchesAHandlerReadingTheThrowsOwnWrite(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, next, handler := function.block(), function.block(), function.block()
	function.entry = entry.id
	throwing := function.define(entry, "x")
	read := function.use(handler, "x")
	after := function.use(next, "x")
	entry.edges = []testEdge{{to: next.id, kind: ssa.Real}, {to: handler.id, kind: ssa.Exceptional}}
	finalize(function)
	after.uses[0] = throwing.defines[0]
	if violations := ssa.VerifyStaticSingleAssignment(testGraph{}, function); len(violations) != 0 {
		test.Fatalf("the continuation reading the throw's write read as %v, want no violation", violations)
	}

	read.uses[0] = throwing.defines[0]

	violations := ssa.VerifyStaticSingleAssignment(testGraph{}, function)
	if len(violations) != 1 || violations[0].Kind != ssa.StaticSingleAssignmentViolationUseNotDominated ||
		violations[0].Block != handler.id {
		test.Errorf("the handler reading the throw's own write read as %v, want one undominated use in bb%d",
			violations, handler.id)
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

	violations := ssa.VerifyStaticSingleAssignment(testGraph{}, function)
	if len(violations) != 1 || violations[0].Kind != ssa.StaticSingleAssignmentViolationUseNotDominated {
		test.Fatalf("violations are %v, want one undominated use", violations)
	}
	statistics := ssa.CollectStaticSingleAssignmentStatistics(testGraph{}, function)
	if statistics.Uses != 1 || statistics.NamedValues != 1 {
		test.Errorf("statistics are %+v, want one use and one named value", statistics)
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

// TestDominanceFollowsRealEdgesPastAFallthrough: a structural fallthrough reaches blockD before the back
// edge does, so reverse postorder puts blockD ahead of every real predecessor it has. Every path to blockD
// runs blockA, blockB and blockC, so all three dominate it. Over the block slice, dominance said only the
// entry and blockA did (#6v4a54x, Adamic's generated-631).
func TestDominanceFollowsRealEdgesPastAFallthrough(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, blockA, blockB := function.block(), function.block(), function.block()
	blockC, blockD := function.block(), function.block()
	function.entry = entry.id
	goTo(entry, blockA)
	blockA.edges = []testEdge{{to: blockD.id, kind: ssa.Fallthrough}, {to: blockB.id, kind: ssa.Real}}
	goTo(blockB, blockA, blockC)
	goTo(blockC, blockD)
	goTo(blockD, blockD, blockB)
	finalize(function)

	var order []ssa.BlockId
	for _, block := range function.blocks {
		order = append(order, block.id)
	}
	if !slices.Equal(order, []ssa.BlockId{entry.id, blockA.id, blockD.id, blockB.id, blockC.id}) {
		test.Fatalf("the block order is %v; the test needs blockD ahead of its real predecessors", order)
	}
	dominance := ssa.ComputeDominance(testGraph{}, function)
	for _, dominator := range []*testBlock{entry, blockA, blockB, blockC, blockD} {
		if !dominance.Dominates(dominator.id, blockD.id) {
			test.Errorf("bb%d doesn't dominate bb%d, which every path reaches through it", dominator.id, blockD.id)
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
		function := generatedFunction(random)
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

// generatedFunction is a random function of one to twelve blocks, with fallthroughs, exceptional edges,
// self loops and edges to blocks that aren't there, not yet finalized.
func generatedFunction(random *rand.Rand) *testFunction {
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
	return function
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
// VerifyStaticSingleAssignment says so, and Construct refuses with a panic that names the block. Without the
// refusal, the read of x in blockC, which nothing defines, looks back through blockB, blockA and the entry, each
// with one sealed predecessor, and around again until the stack runs out.
func TestAnEntryWithPredecessorsIsReportedAndRefused(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, blockA, blockB, blockC := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	function.use(blockC, "x")
	goTo(entry, blockA)
	goTo(blockA, blockB)
	goTo(blockB, entry, blockC)
	finalize(function)

	violations := ssa.VerifyStaticSingleAssignment(testGraph{}, function)
	if len(violations) == 0 ||
		violations[0].Kind != ssa.StaticSingleAssignmentViolationEntryHasPredecessors ||
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
		blockB.id,
	); !strings.Contains(refusal, want) {
		test.Fatalf("Construct gives %q, want a refusal naming %q", refusal, want)
	}
}

// blockIds is the blocks' ids, in order, for comparing with what Dominance answers.
func blockIds(blocks ...*testBlock) []ssa.BlockId {
	var ids []ssa.BlockId
	for _, block := range blocks {
		ids = append(ids, block.id)
	}
	return ids
}

// expectTree checks the tree ComputeDominance makes: each listed parent's children, every block's
// immediate dominator against those lists, and the preorder walk. A block no list names as a child has
// no immediate dominator.
func expectTree(
	test *testing.T,
	function *testFunction,
	children map[*testBlock][]*testBlock,
	preorder []*testBlock,
) *ssa.Dominance {
	test.Helper()
	dominance := ssa.ComputeDominance(testGraph{}, function)
	parent := map[ssa.BlockId]ssa.BlockId{}
	for block, want := range children {
		if got := dominance.Children(block.id); !slices.Equal(got, blockIds(want...)) {
			test.Errorf("bb%d's children are %v, want %v", block.id, got, blockIds(want...))
		}
		for _, child := range want {
			parent[child.id] = block.id
		}
	}
	for _, block := range function.blocks {
		if _, listed := children[block]; !listed && len(dominance.Children(block.id)) != 0 {
			test.Errorf("bb%d has children %v, want none", block.id, dominance.Children(block.id))
		}
		want, hasParent := parent[block.id]
		if got, ok := dominance.ImmediateDominator(block.id); ok != hasParent || got != want {
			test.Errorf("bb%d's immediate dominator is bb%d (%t), want bb%d (%t)", block.id, got, ok, want, hasParent)
		}
	}
	if got := dominance.Preorder(); !slices.Equal(got, blockIds(preorder...)) {
		test.Errorf("the preorder is %v, want %v", got, blockIds(preorder...))
	}
	return dominance
}

// TestTreeOfADiamondOrdersTheJoinBeforeTheSecondArm: the entry dominates both arms and the join, and
// its children are in the order go/ir's search first reaches them: the left arm, the join through it,
// then the right arm. Neither creation order (left, right, join) nor reverse postorder (right, left,
// join) is that order.
func TestTreeOfADiamondOrdersTheJoinBeforeTheSecondArm(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, left, right, join := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	goTo(entry, left, right)
	goTo(left, join)
	goTo(right, join)
	finalize(function)

	expectTree(test, function, map[*testBlock][]*testBlock{entry: {left, join, right}},
		[]*testBlock{entry, left, join, right})
}

// TestTreeOfALoopHangsTheBodyAndExitFromTheHeader: the back edge from the body changes no immediate
// dominator, so the header is the entry's one child, and the body and exit are the header's, in the
// order its terminal names them.
func TestTreeOfALoopHangsTheBodyAndExitFromTheHeader(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, header, body, exit := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	goTo(entry, header)
	goTo(header, body, exit)
	goTo(body, header)
	finalize(function)

	expectTree(test, function, map[*testBlock][]*testBlock{entry: {header}, header: {body, exit}},
		[]*testBlock{entry, header, body, exit})
}

// TestTreeOfANestedLoopFollowsTheOuterHeadersEdges: the outer header names its exit before the inner
// loop, so go/ir's search reaches the exit first and it is the outer header's first child, ahead of the
// inner header made before it. The inner loop's body and latch hang from the inner header.
func TestTreeOfANestedLoopFollowsTheOuterHeadersEdges(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, outer, inner := function.block(), function.block(), function.block()
	body, latch, exit := function.block(), function.block(), function.block()
	function.entry = entry.id
	goTo(entry, outer)
	goTo(outer, exit, inner)
	goTo(inner, body, latch)
	goTo(body, inner)
	goTo(latch, outer)
	finalize(function)

	expectTree(test, function,
		map[*testBlock][]*testBlock{entry: {outer}, outer: {exit, inner}, inner: {body, latch}},
		[]*testBlock{entry, outer, exit, inner, body, latch})
}

// TestTreeLeavesOutABlockTheRealEdgesDontReach: a fallthrough's placeholder stays in the slice with no
// real edge into it, so it has no immediate dominator, no children, and no place in the walk, and the
// entry's one child is the body. A block the function doesn't hold answers the same.
func TestTreeLeavesOutABlockTheRealEdgesDontReach(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, body, continuation := function.block(), function.block(), function.block()
	function.entry = entry.id
	entry.edges = []testEdge{{to: continuation.id, kind: ssa.Fallthrough}, {to: body.id, kind: ssa.Real}}
	body.returns = true
	finalize(function)
	if !slices.Contains(blockIds(function.blocks...), continuation.id) {
		test.Fatalf("the block order is %v; the test needs the placeholder in it", blockIds(function.blocks...))
	}

	dominance := expectTree(test, function, map[*testBlock][]*testBlock{entry: {body}}, []*testBlock{entry, body})
	missing := ssa.BlockId(function.next + 7)
	if _, ok := dominance.ImmediateDominator(missing); ok || dominance.Children(missing) != nil {
		test.Errorf("a block the function doesn't hold has a place in the tree")
	}
}

// TestTreePreorderIsNotTheSearchsPreorder: blocks are made in an order go/ir's isn't, and the walk of
// the tree differs from the search's own preorder. The entry reaches blockB first, then blockX through
// it, so the entry's children are blockB then blockX (creation made blockX first). blockX has the
// entry's edge too, so it hangs from the entry, and the walk visits blockB's child blockY before it,
// where the search visited blockX before blockY.
func TestTreePreorderIsNotTheSearchsPreorder(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, blockX, blockY, blockB := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	goTo(entry, blockB, blockX)
	goTo(blockB, blockX, blockY)
	finalize(function)

	expectTree(test, function, map[*testBlock][]*testBlock{entry: {blockB, blockX}, blockB: {blockY}},
		[]*testBlock{entry, blockB, blockY, blockX})
}

// TestTreeAgreesWithGoIrsConstruction: over thousands of generated graphs, the tree is the one go/ir's
// buildDomTree makes, built here the plain way: immediate dominators from set intersection, each block
// appended to its parent's children in the recursive depth-first preorder of the real edges, and the
// walk a recursive one over those children.
func TestTreeAgreesWithGoIrsConstruction(test *testing.T) {
	test.Parallel()
	random := rand.New(rand.NewSource(20261108))
	const graphs = 4000
	branching := 0
	for graph := 0; graph < graphs; graph++ {
		function := generatedFunction(random)
		finalize(function)

		dominators := setIntersectionDominators(function)
		var searchPreorder []ssa.BlockId
		searched := map[ssa.BlockId]bool{}
		var search func(id ssa.BlockId)
		search = func(id ssa.BlockId) {
			block, ok := function.table[id]
			if !ok || searched[id] {
				return
			}
			searched[id] = true
			searchPreorder = append(searchPreorder, id)
			for _, edge := range block.edges {
				if edge.kind != ssa.Fallthrough {
					search(edge.to)
				}
			}
		}
		search(function.entry)

		// A block's immediate dominator is the strict dominator every other strict one dominates, so the one
		// with the most dominators of its own.
		wantParent := map[ssa.BlockId]ssa.BlockId{}
		wantChildren := map[ssa.BlockId][]ssa.BlockId{}
		for _, id := range searchPreorder[1:] {
			best, bestCount := ssa.BlockId(0), -1
			for dominator := range dominators[id] {
				if dominator != id && len(dominators[dominator]) > bestCount {
					best, bestCount = dominator, len(dominators[dominator])
				}
			}
			wantParent[id] = best
			wantChildren[best] = append(wantChildren[best], id)
		}
		var wantPreorder []ssa.BlockId
		var walk func(id ssa.BlockId)
		walk = func(id ssa.BlockId) {
			wantPreorder = append(wantPreorder, id)
			for _, child := range wantChildren[id] {
				walk(child)
			}
		}
		walk(function.entry)

		dominance := ssa.ComputeDominance(testGraph{}, function)
		for _, block := range function.blocks {
			if got := dominance.Children(block.id); !slices.Equal(got, wantChildren[block.id]) {
				test.Fatalf("graph %d: bb%d's children are %v, go/ir's are %v\n%s",
					graph, block.id, got, wantChildren[block.id], describeTestGraph(function))
			}
			if len(wantChildren[block.id]) > 1 {
				branching++
			}
			want, hasParent := wantParent[block.id]
			if got, ok := dominance.ImmediateDominator(block.id); ok != hasParent || got != want {
				test.Fatalf("graph %d: bb%d's immediate dominator is bb%d (%t), want bb%d (%t)\n%s",
					graph, block.id, got, ok, want, hasParent, describeTestGraph(function))
			}
		}
		if got := dominance.Preorder(); !slices.Equal(got, wantPreorder) {
			test.Fatalf("graph %d: the preorder is %v, go/ir's is %v\n%s",
				graph, got, wantPreorder, describeTestGraph(function))
		}
	}
	if branching == 0 {
		test.Fatalf("none of the %d graphs has a block with two children, so the order was never tested", graphs)
	}
	test.Logf("%d graphs, %d blocks with two children or more", graphs, branching)
}

// phiIds is every phi's result in the function, block by block in the slice's order.
func phiIds(function *testFunction) []ssa.IdentifierId {
	var ids []ssa.IdentifierId
	for _, block := range function.blocks {
		for _, phi := range block.phis {
			ids = append(ids, phi.Place.id)
		}
	}
	return ids
}

// handPhi is a phi built without construction, its operands from predecessors 1, 2 and on, for a test
// that hands EliminateRedundantPhis a web of phis no small graph makes.
func handPhi(place testPlace, operands ...testPlace) *ssa.Phi[testPlace] {
	phi := &ssa.Phi[testPlace]{Place: place}
	for index, operand := range operands {
		phi.Operands.Set(ssa.BlockId(index+1), operand)
	}
	return phi
}

// TestIrreducibleLoopWithOneValueLeavesNoPhi: the entry branches into a cycle at two blocks, blockA and
// blockB, and x is written only before it. Construction places a phi in each, each naming the entry's x
// and the other's phi, so neither is trivial. Together they hold only the entry's x, and the strongly
// connected step collapses them to it (Braun et al., Figure 5).
func TestIrreducibleLoopWithOneValueLeavesNoPhi(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, blockA, blockB, exit := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	initial := function.define(entry, "x")
	reads := []*testInstruction{function.use(blockA, "x"), function.use(blockB, "x"), function.use(exit, "x")}
	goTo(entry, blockA, blockB)
	goTo(blockA, blockB, exit)
	goTo(blockB, blockA)

	construct(test, function)

	if phis := phiIds(function); len(phis) != 0 {
		test.Errorf("the cycle kept phis %v, want none", phis)
	}
	for _, read := range reads {
		if read.uses[0].id != initial.defines[0].id {
			test.Errorf("a read names $%d, want the entry's write $%d", read.uses[0].id, initial.defines[0].id)
		}
	}
}

// TestIrreducibleLoopWithTwoValuesKeepsBothPhis: the same cycle, entered with x written differently on
// each way in, so each entry's phi is a real merge and both stay.
func TestIrreducibleLoopWithTwoValuesKeepsBothPhis(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, left, right := function.block(), function.block(), function.block()
	blockA, blockB, exit := function.block(), function.block(), function.block()
	function.entry = entry.id
	fromLeft := function.define(left, "x")
	fromRight := function.define(right, "x")
	readA, readB := function.use(blockA, "x"), function.use(blockB, "x")
	function.use(exit, "x")
	goTo(entry, left, right)
	goTo(left, blockA)
	goTo(right, blockB)
	goTo(blockA, blockB, exit)
	goTo(blockB, blockA)

	construct(test, function)

	if len(blockA.phis) != 1 || len(blockB.phis) != 1 {
		test.Fatalf("blockA has %d phis and blockB %d, want one each", len(blockA.phis), len(blockB.phis))
	}
	if got := blockA.phis[0].Operands.At(left.id).id; got != fromLeft.defines[0].id {
		test.Errorf("blockA's phi takes $%d from the left, want $%d", got, fromLeft.defines[0].id)
	}
	if got := blockB.phis[0].Operands.At(right.id).id; got != fromRight.defines[0].id {
		test.Errorf("blockB's phi takes $%d from the right, want $%d", got, fromRight.defines[0].id)
	}
	if readA.uses[0].id != blockA.phis[0].Place.id || readB.uses[0].id != blockB.phis[0].Place.id {
		test.Errorf("the reads in the cycle don't name their block's phi")
	}
}

// TestNestedLoopWithTwoValuesKeepsItsCycleOfPhis: reducible flow can make a cycle of phis too. The outer
// header's phi takes the inner header's from the latch, and the inner header's takes the outer's and the
// body's write, so the two form a component whose outside values are the entry's i and the body's. Both
// stay, as they did before the strongly connected step.
func TestNestedLoopWithTwoValuesKeepsItsCycleOfPhis(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, outer, inner := function.block(), function.block(), function.block()
	body, latch, exit := function.block(), function.block(), function.block()
	function.entry = entry.id
	initial := function.define(entry, "i")
	function.use(outer, "i")
	function.use(inner, "i")
	written := function.define(body, "i")
	function.use(exit, "i")
	goTo(entry, outer)
	goTo(outer, inner, exit)
	goTo(inner, body, latch)
	goTo(body, inner)
	goTo(latch, outer)

	construct(test, function)

	if len(outer.phis) != 1 || len(inner.phis) != 1 {
		test.Fatalf("the outer header has %d phis and the inner %d, want one each", len(outer.phis), len(inner.phis))
	}
	outerPhi, innerPhi := outer.phis[0], inner.phis[0]
	if outerPhi.Operands.At(entry.id).id != initial.defines[0].id ||
		outerPhi.Operands.At(latch.id).id != innerPhi.Place.id {
		test.Errorf("the outer header's phi is %+v, want the entry's i and the inner header's phi", outerPhi.Operands)
	}
	if innerPhi.Operands.At(outer.id).id != outerPhi.Place.id ||
		innerPhi.Operands.At(body.id).id != written.defines[0].id {
		test.Errorf("the inner header's phi is %+v, want the outer header's phi and the body's i", innerPhi.Operands)
	}
}

// TestComponentRecursesIntoItsInnerPhis is Braun et al.'s Figure 6. phi1 to phi4 form one component with
// two values from outside, phi0 and plus, so phi1 and phi2, which take them, are real merges. phi3 and
// phi4 take only each other and phi2: the inner component, which the recursion collapses to phi2.
// phi0's component is phi0 alone, and stays. The phis kept stay in the order they were in.
func TestComponentRecursesIntoItsInnerPhis(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, head, loop, exit := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	x, y, plus := function.define(entry, "x"), function.define(entry, "y"), function.define(loop, "plus")
	phi0, phi1, phi2 := function.value("v"), function.value("v"), function.value("v")
	phi3, phi4 := function.value("v"), function.value("v")
	head.phis = []*ssa.Phi[testPlace]{handPhi(phi0, x.defines[0], y.defines[0])}
	loop.phis = []*ssa.Phi[testPlace]{
		handPhi(phi1, phi0, phi4),
		handPhi(phi2, phi1, plus.defines[0]),
		handPhi(phi3, phi2, phi4),
		handPhi(phi4, phi3, phi2),
	}
	read := &testInstruction{uses: []testPlace{phi3, phi4}}
	exit.instructions = append(exit.instructions, read)

	ssa.EliminateRedundantPhis(testGraph{}, function)

	if got, want := phiIds(function), []ssa.IdentifierId{phi0.id, phi1.id, phi2.id}; !slices.Equal(got, want) {
		test.Fatalf("the phis kept are %v, want %v", got, want)
	}
	if got := loop.phis[0].Operands.At(2).id; got != phi2.id {
		test.Errorf("phi1's operand from phi4 is $%d, want phi2's $%d", got, phi2.id)
	}
	if read.uses[0].id != phi2.id || read.uses[1].id != phi2.id {
		test.Errorf("the reads of phi3 and phi4 name $%d and $%d, want phi2's $%d",
			read.uses[0].id, read.uses[1].id, phi2.id)
	}
}

// TestCollapsedComponentLeavesATrivialPhiForTheTrivialRule: phiA and phiB form a component holding only
// x, and phiC merges x with phiA. phiC isn't trivial until the component collapses, and then it is, so
// the trivial rule has to run again after the strongly connected step.
func TestCollapsedComponentLeavesATrivialPhiForTheTrivialRule(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, loop, exit := function.block(), function.block(), function.block()
	function.entry = entry.id
	x := function.define(entry, "x")
	phiA, phiB, phiC := function.value("x"), function.value("x"), function.value("x")
	loop.phis = []*ssa.Phi[testPlace]{handPhi(phiA, x.defines[0], phiB), handPhi(phiB, x.defines[0], phiA)}
	exit.phis = []*ssa.Phi[testPlace]{handPhi(phiC, x.defines[0], phiA)}
	read := &testInstruction{uses: []testPlace{phiC}}
	exit.instructions = append(exit.instructions, read)

	ssa.EliminateRedundantPhis(testGraph{}, function)

	if phis := phiIds(function); len(phis) != 0 {
		test.Errorf("the phis kept are %v, want none", phis)
	}
	if read.uses[0].id != x.defines[0].id {
		test.Errorf("the read of phiC names $%d, want x's $%d", read.uses[0].id, x.defines[0].id)
	}
}

// TestATerminalNamingABlockTwiceSealsItOnce: the entry's terminal names the loop header twice, as a
// MaybeThrow whose continuation is its handler or a switch with two cases on one block would. The entry is
// one predecessor of the header, so the header stays unsealed until its latch is walked, and the header's
// phi takes the latch's write. Counting the entry twice sealed the header before the latch, and the phi
// read the latch's value before the latch had one (#mv39hpt).
func TestATerminalNamingABlockTwiceSealsItOnce(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, header, latch, exit := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	function.define(entry, "i")
	headerRead := function.use(header, "i")
	write := function.define(latch, "i")
	function.use(exit, "i")
	goTo(entry, header, header)
	goTo(header, latch, exit)
	goTo(latch, header)

	construct(test, function)

	if got, want := header.predecessors, blockIds(entry, latch); !slices.Equal(got, want) {
		test.Fatalf("the header's predecessors are %v, want %v, each once", got, want)
	}
	if len(header.phis) != 1 {
		test.Fatalf("the header has %d phis, want 1", len(header.phis))
	}
	phi := header.phis[0]
	if got := phi.Operands.At(latch.id).id; got != write.defines[0].id {
		test.Errorf("the latch's operand is $%d, want the latch's write $%d", got, write.defines[0].id)
	}
	if headerRead.uses[0].id != phi.Place.id {
		test.Errorf("the header's read names $%d, want its phi $%d", headerRead.uses[0].id, phi.Place.id)
	}
}

// TestVerifierCatchesPredecessorsTheEdgesDontGive: a block listing a predecessor twice, or one no edge comes
// from, is reported, since construction counts the list to seal a block and a phi takes an operand per entry.
func TestVerifierCatchesPredecessorsTheEdgesDontGive(test *testing.T) {
	test.Parallel()
	for _, extra := range []string{"twice", "without an edge"} {
		function := newTestFunction()
		entry, middle, join := function.block(), function.block(), function.block()
		function.entry = entry.id
		goTo(entry, middle, join)
		goTo(middle, join)
		finalize(function)
		if violations := ssa.VerifyStaticSingleAssignment(testGraph{}, function); len(violations) != 0 {
			test.Fatalf("before the change, %v", violations)
		}
		switch extra {
		case "twice":
			join.predecessors = append(join.predecessors, entry.id)
		case "without an edge":
			middle.predecessors = append(middle.predecessors, join.id)
		}
		violations := ssa.VerifyStaticSingleAssignment(testGraph{}, function)
		if len(violations) != 1 || violations[0].Kind != ssa.StaticSingleAssignmentViolationPredecessorsDisagree {
			test.Errorf("a predecessor listed %s read as %v, want one disagreement", extra, violations)
		}
	}
}

// TestAppendingToChildrenLeavesTheNextBlocksChildren: Children's slice shares the tree's array, capped, so a
// caller's append copies rather than writing over the next block's children there.
func TestAppendingToChildrenLeavesTheNextBlocksChildren(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, left, right := function.block(), function.block(), function.block()
	leftChild, rightChild := function.block(), function.block()
	function.entry = entry.id
	goTo(entry, left, right)
	goTo(left, leftChild)
	goTo(right, rightChild)
	finalize(function)
	dominance := ssa.ComputeDominance(testGraph{}, function)

	before := map[ssa.BlockId][]ssa.BlockId{}
	for _, block := range function.blocks {
		before[block.id] = slices.Clone(dominance.Children(block.id))
	}
	parents := 0
	for _, block := range function.blocks {
		children := dominance.Children(block.id)
		if len(children) == 0 {
			continue
		}
		parents++
		_ = append(children, 999)
		for _, other := range function.blocks {
			if got := dominance.Children(other.id); !slices.Equal(got, before[other.id]) {
				test.Errorf("after appending to bb%d's children, bb%d's are %v, want %v",
					block.id, other.id, got, before[other.id])
			}
		}
	}
	if parents < 3 {
		test.Fatalf("%d blocks have children, want the entry and both arms, so an append has a sibling to reach",
			parents)
	}
}

// TestACollapsedComponentResolvesTheNextComponentsOperand: componentA holds only x and collapses to it, and
// componentB's outside values are x and componentA's phiA1. The step visits componentA first, since componentB
// uses it, and reads phiA1 through its collapse as x, so componentB holds one value too and collapses in the
// same pass. Read as itself, phiA1 would be a second value and componentB would wait for another round, which
// EliminateRedundantPhis's fixed point gives it, so this runs the step once, alone.
func TestACollapsedComponentResolvesTheNextComponentsOperand(test *testing.T) {
	test.Parallel()
	function := newTestFunction()
	entry, loopA, loopB, exit := function.block(), function.block(), function.block(), function.block()
	function.entry = entry.id
	x := function.define(entry, "x")
	phiA1, phiA2 := function.value("x"), function.value("x")
	phiB1, phiB2 := function.value("x"), function.value("x")
	loopA.phis = []*ssa.Phi[testPlace]{handPhi(phiA1, x.defines[0], phiA2), handPhi(phiA2, x.defines[0], phiA1)}
	loopB.phis = []*ssa.Phi[testPlace]{handPhi(phiB1, phiA1, phiB2), handPhi(phiB2, x.defines[0], phiB1)}
	read := &testInstruction{uses: []testPlace{phiB1, phiB2}}
	exit.instructions = append(exit.instructions, read)

	if !ssa.EliminateRedundantComponentsOnceForTest(testGraph{}, function) {
		test.Fatal("the step collapsed nothing")
	}

	if phis := phiIds(function); len(phis) != 0 {
		test.Errorf("one pass kept the phis %v, want none", phis)
	}
	if read.uses[0].id != x.defines[0].id || read.uses[1].id != x.defines[0].id {
		test.Errorf("the reads of phiB1 and phiB2 name $%d and $%d, want x's $%d",
			read.uses[0].id, read.uses[1].id, x.defines[0].id)
	}
}
