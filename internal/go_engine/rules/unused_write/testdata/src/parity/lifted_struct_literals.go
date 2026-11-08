package parity

// Pointers to struct literals whose pointer nothing reads through. staticcheck lifts an allocation whose every use is
// a load or a store through it, a literal's included, so once the variable holding the pointer is a register and
// nothing loads the pointer, the allocation and the literal's store are gone, where x/tools keeps each given field's
// write and the original reports it. loom's coordinator test held the first shape.

type machine struct {
	label string
	has   []string
}

type passing struct {
	machine
	strict bool
}

type breaking struct {
	machine
	ran []string
}

func (value passing) name() string { return value.label }

func consume(values ...any) {}

// Two pointer literals assigned by one tuple, the second variable read before and never again: loom's shape.
func tupleReassignedNeverReadAgain() {
	bad, good := &breaking{machine: machine{label: "bad"}}, &passing{machine: machine{label: "good"}}
	consume(bad, good)
	bad, good = &breaking{machine: machine{label: "bad"}}, &passing{machine: machine{label: "good"}}
	consume(bad, bad)
}

// The same by a single assignment.
func reassignedNeverReadAgain() {
	good := &passing{machine: machine{label: "good"}}
	consume(good)
	good = &passing{machine: machine{label: "good"}}
}

// The same, a plain field given in place of the embedded one.
func plainFieldReassignedNeverReadAgain() {
	good := &passing{strict: true}
	consume(good)
	good = &passing{strict: true}
}

// The same, two fields given, positionally.
func positionalReassignedNeverReadAgain() {
	good := &passing{machine{label: "good"}, true}
	consume(good)
	good = &passing{machine{label: "good"}, true}
}

// Reassigned in a loop and never read after the loop.
func reassignedInLoop(count int) {
	good := &passing{strict: true}
	consume(good)
	for range count {
		good = &passing{strict: true}
	}
}

// Assigned to the blank identifier, a use staticcheck keeps.
func toBlank() {
	good := &passing{machine: machine{label: "good"}}
	_ = good
}

// A whole struct stored through the pointer, which the original passes over, and nothing loaded.
func storedThrough() {
	good := &passing{strict: true}
	*good = passing{strict: false}
}

// Loaded whole, which the original counts as a use.
func loaded() passing {
	good := &passing{strict: true}
	return *good
}

// Loaded into a variable that is given another value before it is read: the load is dead, and still a use.
func deadLoad(other passing) passing {
	good := &passing{strict: true}
	copied := *good
	copied = other
	return copied
}

// A value method called through the pointer, an implicit load.
func valueMethod() string {
	good := &passing{machine: machine{label: "good"}}
	return good.name()
}

// A whole struct stored through the pointer, then a value method called through it, which loads what was stored.
func storedThroughThenValueMethod(other passing) string {
	good := &passing{machine: machine{label: "good"}}
	*good = other
	return good.name()
}

// Stored through, then loaded.
func storedThroughThenLoaded(other passing) passing {
	good := &passing{strict: true}
	*good = other
	return *good
}

// Copied into another variable, and the copy loaded.
func copiedThenLoaded() passing {
	good := &passing{strict: true}
	other := good
	return *other
}

// Reassigned inside a closure that captures the variable.
func reassignedInClosure() func() {
	good := &passing{strict: true}
	consume(good)
	return func() {
		good = &passing{strict: true}
	}
}

// Another pointer of the same type loaded, which cannot hold the literal's pointer.
func otherPointerLoaded(other *passing) passing {
	good := &passing{strict: true}
	consume(good)
	good = &passing{strict: true}
	return *other
}

// The same variable loaded before it is given the literal, outside any loop, which reads the earlier pointer.
func loadedBeforeReassigned() passing {
	good := &passing{strict: true}
	copied := *good
	good = &passing{strict: true}
	return copied
}

// A whole struct stored through the pointer, and a method called through another variable of the same type.
func otherPointerMethod(other *passing) string {
	good := &passing{strict: true}
	*good = passing{}
	return other.name()
}

// Loaded in a loop before it is given the literal, so the next iteration loads the literal's pointer.
func loadedInLoopNextIteration(count int) passing {
	good := &passing{strict: true}
	var copied passing
	for range count {
		copied = *good
		good = &passing{strict: true}
	}
	return copied
}

// Loaded through a conversion to its own type into a variable given another value before it is read: the load is
// dead, and still a use.
func deadLoadThroughConversion(other passing) passing {
	good := &passing{strict: true}
	copied := *(*passing)(good)
	copied = other
	return copied
}

// Given the literal, then another before anything loads it: the load reads the second.
func reassignedBeforeLoaded() passing {
	good := &passing{strict: true}
	good = &passing{machine: machine{label: "second"}}
	return *good
}

// Given another in a branch before the load, so either may be loaded.
func reassignedInBranchBeforeLoaded(condition bool) passing {
	good := &passing{strict: true}
	if condition {
		good = &passing{machine: machine{label: "second"}}
	}
	return *good
}

// Loaded before it is given the literal, in a function with a goto that only jumps forward.
func loadedBeforeReassignedWithForwardJump(count int) passing {
	good := &passing{strict: true}
	copied := *good
	if count > 0 {
		goto done
	}
	consume(count)
done:
	good = &passing{strict: true}
	return copied
}

// Given the literal after the load, and a goto jumps back to the load.
func loadedAfterJumpBack(count int) passing {
	good := &passing{strict: true}
	var copied passing
again:
	copied = *good
	if count > 0 {
		count--
		good = &passing{machine: machine{label: "second"}}
		goto again
	}
	return copied
}

// Given another in a branch that panics, so only the first reaches the load.
func reassignedInPanickingBranch(condition bool) passing {
	good := &passing{strict: true}
	if condition {
		good = &passing{machine: machine{label: "second"}}
		panic("second")
	}
	return *good
}

// Copied through a conversion to its own type, and the copy loaded.
func convertedThenLoaded(other passing) passing {
	good := &passing{strict: true}
	converted := (*passing)(good)
	copied := *converted
	copied = other
	return copied
}

// A whole struct stored straight through the literal's pointer, which no variable holds.
func storedStraightThrough() {
	*&passing{strict: true} = passing{}
}

// Loaded straight through the literal's pointer into a variable given another value before it is read.
func deadLoadStraightThrough(other passing) passing {
	copied := *&passing{strict: true}
	copied = other
	return copied
}

// A value literal reassigned and never read again, which x/tools builds in a temporary and copies.
func valueReassignedNeverReadAgain() {
	good := passing{machine: machine{label: "good"}}
	consume(good)
	good = passing{machine: machine{label: "good"}}
}
