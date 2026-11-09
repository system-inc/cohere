package static_single_assignment_test

import ssa "github.com/system-inc/cohere/static_single_assignment"

type testFunction struct {
	entry       ssa.BlockId
	blocks      []*testBlock
	table       map[ssa.BlockId]*testBlock
	next        ssa.BlockId
	identifiers []testIdentifier
	parameters  []testPlace
	returns     *testPlace
	contextual  map[ssa.DeclarationId]bool
	// variables maps a name to its declaration, for building.
	variables map[string]ssa.DeclarationId
	// boundUnderReported makes BlockBound answer zero, as a graph that hands out block ids lazily might.
	boundUnderReported bool
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
