package return_nil_ness

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// numberings holds the numberings of functions done judging, for the next function any goroutine judges.
var numberings = sync.Pool{New: func() any { return &numbering{others: map[intermediate_representation.Value]int{}} }}

// numbering numbers the values of one function densely, in the order they are first asked about, shared by every
// state of the function so a number means one value throughout.
//
// An instruction is found by its Id, which the build gives every instruction of a built function, one apiece, in
// numberById, and every other value, a parameter, a constant or a callee among them, in others. Each function's
// numbering was a map made afresh and grown from empty, a quarter of the package's cost on a cold run at cohere's root
// (#fex6s0v); this one is pooled, and release empties only what the function used. An Id is trusted for nothing: the
// slot of an Id names the value that claimed it, through numbered, and a value that finds its slot claimed by another
// is numbered in others, so every value gets the number the map gave it, whatever the Ids are.
type numbering struct {
	// numberById is the number of each instruction that claimed an Id, plus one, by that Id, zero for an Id none has
	// claimed. It is zero throughout whenever the pool holds it.
	numberById []int
	// numbered is each value numbered, by its number.
	numbered []intermediate_representation.Value
	// others is the number of each value not numbered by its Id.
	others map[intermediate_representation.Value]int
}

// numberingOf is an empty numbering for function, from the pool, with a slot for every Id its build gives.
func numberingOf(function *intermediate_representation.Function) *numbering {
	values, isNumbering := numberings.Get().(*numbering)
	if !isNumbering {
		// The pool's New makes every value it holds, so this is only a pool emptied by a collection.
		values = &numbering{others: map[intermediate_representation.Value]int{}}
	}
	// The build numbers a function's instructions from one, with no Id past their count.
	ids := 1
	for _, block := range function.Blocks {
		ids += len(block.Instructions)
	}
	if len(values.numberById) < ids {
		values.numberById = make([]int, ids)
	}
	return values
}

// number is value's number, given the next one when value has none yet.
func (values *numbering) number(value intermediate_representation.Value) int {
	if id, hasId := values.idOf(value); hasId {
		claimed := values.numberById[id]
		if claimed == 0 {
			values.numberById[id] = len(values.numbered) + 1
			values.numbered = append(values.numbered, value)
			return len(values.numbered) - 1
		}
		if values.numbered[claimed-1] == value {
			return claimed - 1
		}
		// Another value claimed value's Id first, and keeps it for the function, so value is always numbered below.
	}
	number, numbered := values.others[value]
	if !numbered {
		number = len(values.numbered)
		values.others[value] = number
		values.numbered = append(values.numbered, value)
	}
	return number
}

// idOf is value's Id and whether it has a slot for one: an instruction's Id within numberById.
func (values *numbering) idOf(value intermediate_representation.Value) (int, bool) {
	instruction, isInstruction := value.(intermediate_representation.Instruction)
	if !isInstruction {
		return 0, false
	}
	id := int(instruction.Id())
	return id, id >= 0 && id < len(values.numberById)
}

// release empties values, each slot and entry the function used and no other, and gives it back to the pool.
func (values *numbering) release() {
	for number, value := range values.numbered {
		if id, hasId := values.idOf(value); hasId && values.numberById[id] == number+1 {
			values.numberById[id] = 0
			continue
		}
		delete(values.others, value)
	}
	clear(values.numbered)
	values.numbered = values.numbered[:0]
	numberings.Put(values)
}
