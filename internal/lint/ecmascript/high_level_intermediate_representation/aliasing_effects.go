package high_level_intermediate_representation

// AliasingEffects is the table this pass produces: the effect list of every instruction.
//
// Keyed by InstructionId rather than stored on the Instruction because an Instruction is shared
// through the per-file lowering cache and several rules hold the same one. Writing effects onto it
// would make one rule's pass visible to another that never asked for it, and `Construct` is not
// idempotent, so a table that has to be rebuilt is safer than a graph that has been mutated.
type AliasingEffects struct {
	byInstruction map[InstructionId][]AliasingEffect
}

// Get returns one instruction's effects, or nil when it has none.
func (effects *AliasingEffects) Get(id InstructionId) []AliasingEffect {
	if effects == nil {
		return nil
	}
	return effects.byInstruction[id]
}

// Length is how many instructions carry at least one effect.
func (effects *AliasingEffects) Length() int {
	if effects == nil {
		return 0
	}
	return len(effects.byInstruction)
}
