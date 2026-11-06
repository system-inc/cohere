package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// calleeProducers is the table isModuleHookCall resolves a callee through: each value's producing
// instruction value, plus each StoreLocal under the binding it writes.
//
// It is built on first use and then shared by every call a pass asks about in one function. Building
// it inside isModuleHookCall made each call cost a walk of every instruction in the function, which
// is quadratic in a function's size and was 85 MB of map on a cold ahra run. A pass makes one with
// newCalleeProducers for the function it walks, and must not mutate that function's instructions
// while holding it, since the table is not rebuilt.
type calleeProducers struct {
	function *Function
	table    map[static_single_assignment.IdentifierId]InstructionValue
	// builds counts how many times the table was built, for the test that it is built once.
	builds int
}

// producer returns the value that produced identifier, building the table on the first ask.
func (producers *calleeProducers) producer(identifier static_single_assignment.IdentifierId) InstructionValue {
	if producers.table == nil {
		producers.builds++
		producers.table = make(
			map[static_single_assignment.IdentifierId]InstructionValue,
			len(producers.function.Instructions),
		)
		for _, candidate := range producers.function.Instructions {
			if candidate == nil {
				continue
			}
			producers.table[candidate.LValue.Identifier] = candidate.Value
			if store, ok := candidate.Value.(*StoreLocal); ok {
				producers.table[store.LValue.Identifier] = store
			}
		}
	}
	return producers.table[identifier]
}
