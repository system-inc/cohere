package high_level_intermediate_representation

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/static_single_assignment"
)

// Function is one lowered function: its control-flow graph, its instruction table, and the tables
// naming everything the graph refers to.
//
// It owns its tables rather than sharing an arena, which is the main structural divergence from
// upstream. See the package comment.
type Function struct {
	// ContextDeclarations are bindings this function declares, reassigns, and shares with a closure
	// inside it -- upstream's second `FindContextIdentifiers` rule. Recorded during lowering, and
	// read by SSA, which defines such a binding once rather than versioning every write.
	ContextDeclarations map[static_single_assignment.DeclarationId]bool

	// Node is the syntactic function this was lowered from. Retained so a pass can reach the type
	// checker, and so a diagnostic can point at source without the IR carrying its own copy of
	// every span.
	Node *ast.Node

	// Name is the function's name where it has one, for diagnostics only. Nothing may key on it.
	Name string

	// Kind records whether this looked like a React component or hook at lowering time.
	//
	// It is a syntactic guess - capitalized name, or `use` prefix - and it is recorded here rather
	// than computed by each consumer so that all of them are wrong in the same way, which is a
	// property worth having when the corpus disagrees.
	Kind FunctionKind

	// Parameters are the parameter values, in order. A destructured parameter lowers to a temporary
	// here plus destructuring instructions in the entry block, so this is always flat.
	Parameters []Place

	// Returns is the value returned. Every `return` stores into it, so a pass asking what a
	// function yields has one identifier to ask about rather than a set of terminals to collect.
	Returns Place

	// Context are the values this function closes over from an enclosing one. Empty for a
	// top-level function.
	Context []Place

	// Entry is the block control begins at.
	Entry static_single_assignment.BlockId

	// Blocks are the basic blocks in reverse postorder, so a forward analysis that iterates the
	// slice once converges in one pass over an acyclic region.
	Blocks []*BasicBlock

	// Instructions is the flat instruction table. Blocks hold indices into it.
	Instructions []*Instruction

	// Identifiers names every value. Indexed by IdentifierId.
	Identifiers []*Identifier

	// Functions are the nested functions lowered within this one. Indexed by FunctionId.
	Functions []*Function

	// Outlined maps a value holding an outlined function to the function it names.
	//
	// `OutlineFunctions` replaces a capture-free `FunctionExpression` with a `LoadGlobal`, which
	// leaves the entry in `Functions` reachable only through this map. A pass that needs the
	// callee's own body -- `argumentMutationsFromCallbacks` is the one today -- looks here rather
	// than parsing the synthetic name, because `Name` is documented as diagnostics-only and a user
	// function called `_temp` would otherwise resolve to the wrong body.
	//
	// Nil until something is outlined, which is the common case.
	Outlined map[static_single_assignment.IdentifierId]FunctionId

	// IsAsync and IsGenerator carry the modifiers, which change what `await` and `yield` mean and
	// are consulted by validators that do not want to walk back to the AST for them.
	IsAsync     bool
	IsGenerator bool

	blocksById map[static_single_assignment.BlockId]*BasicBlock
	nextBlock  static_single_assignment.BlockId

	// identifierSlab is the chunk NewIdentifier carves identifiers from. See NewIdentifier.
	identifierSlab []Identifier
}

// Block returns the block with the given id.
//
// The second result is false for InvalidBlock and for an id that no longer names a block, which a
// pass that removes blocks will produce. Callers that have just read the id out of a terminal of a
// well-formed function may ignore it; callers walking a function mid-mutation may not.
func (function *Function) Block(id static_single_assignment.BlockId) (*BasicBlock, bool) {
	block, ok := function.blocksById[id]
	return block, ok
}

// Instruction returns the instruction with the given id.
func (function *Function) Instruction(id InstructionId) *Instruction {
	return function.Instructions[id]
}

// Identifier returns the identifier naming one value.
func (function *Function) Identifier(id static_single_assignment.IdentifierId) *Identifier {
	return function.Identifiers[id]
}

// IdentifierOf returns the identifier a place names.
func (function *Function) IdentifierOf(place Place) *Identifier {
	return function.Identifiers[place.Identifier]
}

// NewBlock appends a block and returns it.
func (function *Function) NewBlock(kind BlockKind) *BasicBlock {
	block := &BasicBlock{Id: function.nextBlock, Kind: kind}
	function.nextBlock++
	function.Blocks = append(function.Blocks, block)
	function.blocksById[block.Id] = block
	return block
}

// NewIdentifier mints a new value.
//
// Every value gets its own IdentifierId. Passing a non-zero declaration groups this value with
// others of the same binding; passing 0 mints a fresh declaration, which is what a temporary wants.
func (function *Function) NewIdentifier(
	name string,
	node *ast.Node,
	declaration static_single_assignment.DeclarationId,
) *Identifier {
	id := static_single_assignment.IdentifierId(len(function.Identifiers))
	// A zero declaration asks for a fresh one, numbered from the new id.
	resolvedDeclaration := declaration
	if resolvedDeclaration == 0 {
		resolvedDeclaration = static_single_assignment.DeclarationId(id) + 1
	}
	// Identifiers are carved from a chunk this function owns rather than allocated one at a time,
	// which was 1.28M objects on a cold ahra run, the most of any allocation in lowering (#rwsffzm).
	// A chunk is only ever appended to within its capacity, so a pointer into it stays put; a full
	// chunk is left as it is and a new one started, sized to the function so far, from 4 to 256. The
	// first chunk is small because most functions are small: a callback's handful of values, and a
	// first chunk of 16 cost 0.07 GB of unused slots on a cold ahra run. Each function has its own
	// chunks, so no identifier of one shares storage with another's, which
	// TestIdentifiersShareNoStorageAcrossFunctions holds.
	if len(function.identifierSlab) == cap(function.identifierSlab) {
		function.identifierSlab = make([]Identifier, 0, min(max(4, len(function.Identifiers)), 256))
	}
	function.identifierSlab = append(
		function.identifierSlab,
		Identifier{Id: id, Declaration: resolvedDeclaration, Name: name, Node: node},
	)
	identifier := &function.identifierSlab[len(function.identifierSlab)-1]
	function.Identifiers = append(function.Identifiers, identifier)
	return identifier
}

// AddInstruction appends an instruction to the table and to the given block, returning its id.
//
// Mutates: block the instruction's id is appended to its list
// Mutates: instruction it takes the id it is stored under
func (function *Function) AddInstruction(block *BasicBlock, instruction *Instruction) InstructionId {
	id := InstructionId(len(function.Instructions))
	instruction.Id = id
	function.Instructions = append(function.Instructions, instruction)
	block.Instructions = append(block.Instructions, id)
	return id
}

// String renders a place as its identifier, for printing and tests.
func (function *Function) PlaceString(place Place) string {
	identifier := function.Identifiers[place.Identifier]
	if identifier.Name != "" {
		return fmt.Sprintf("%s$%d", identifier.Name, identifier.Id)
	}
	return fmt.Sprintf("$%d", identifier.Id)
}

// reindexBlocks rebuilds the id-to-block index from Function.Blocks.
//
// Lives here rather than in high_level_intermediate_representation.go because this is the only pass that replaces the
// block slice wholesale; lowering appends through `NewBlock`, which maintains the index as it goes.
func (function *Function) reindexBlocks() {
	function.blocksById = make(map[static_single_assignment.BlockId]*BasicBlock, len(function.Blocks))
	for _, block := range function.Blocks {
		function.blocksById[block.Id] = block
	}
}
