// Package parity holds the shapes SA5000 depends on: how a map an update writes into was made, and the phis it passed
// through on the way. Each function's comment says what the original makes of it; the parity test holds only that the
// port makes the same.
package parity

type set map[string]bool

type holder struct {
	entries map[int]int
}

var global map[int]int

func use(values ...any) {}

func produce() map[int]int { return nil }

// Found: a declared map, never made.
func declared() {
	var entries map[int]int
	entries[1] = 1
}

// Found: a map set to nil outright.
func assignedNil() {
	entries := map[int]int{}
	entries = nil
	entries[1] = 1
}

// Found: a nil map converted to its type.
func converted() {
	entries := map[int]int(nil)
	entries[1] = 1
}

// Found: a named map type, never made.
func namedType() {
	var members set
	members["a"] = true
}

// Found: a compound assignment reads the map, then writes into it.
func compound() {
	var counts map[string]int
	counts["a"] += 2
}

// Found: an increment is an update too.
func increment() {
	var counts map[string]int
	counts["a"]++
}

// Found twice: one assignment of two updates.
func twoAtOnce() {
	var entries map[int]int
	entries[1], entries[2] = 1, 2
}

// Found: the map is nil on every path into the loop.
func inLoop(values []int) {
	var entries map[int]int
	for index, value := range values {
		entries[index] = value
	}
	use(entries)
}

// Found: nil on both branches, each spelled on its own, which the phi flattens to one constant.
func nilOnBothBranches(condition bool) {
	var entries map[int]int
	if condition {
		entries = nil
	} else {
		entries = nil
	}
	entries[1] = 1
}

// Found: two phis that bring each other and one nil, which the builder keeps and Flatten walks down to that nil.
func swappedInLoop(rows int) {
	var current map[int]int
	previous := current
	for row := 0; row < rows; row++ {
		current, previous = previous, current
		current[row] = row
	}
}

// Not found: made on one branch, so the phi brings two values.
func madeOnOneBranch(condition bool) {
	var entries map[int]int
	if condition {
		entries = map[int]int{}
	}
	entries[1] = 1
}

// Not found: made in the loop before the update.
func madeInLoop(values []int) {
	var entries map[int]int
	for index, value := range values {
		if entries == nil {
			entries = make(map[int]int)
		}
		entries[index] = value
	}
}

// Not found: a literal and a make.
func made() {
	literal := map[int]int{1: 1}
	literal[2] = 2
	sized := make(map[int]int, 4)
	sized[1] = 1
}

// Not found: a parameter may be made by the caller.
func parameter(entries map[int]int) {
	entries[1] = 1
}

// Not found: a global, a field and a call's result are loaded, not constant.
func loaded(target *holder) {
	global[1] = 1
	target.entries[1] = 1
	var local holder
	local.entries[1] = 1
	produce()[1] = 1
}

// Not found: a captured map is the closure's free variable.
func captured() {
	var entries map[int]int
	func() {
		entries[1] = 1
	}()
}

// Not found: delete and a read of a nil map do not panic, and are no update.
func readAndDelete() {
	var entries map[int]int
	delete(entries, 1)
	use(entries[1])
}

// Found: the zero value of a map type parameter.
func typeParameter[Map ~map[int]int]() {
	var entries Map
	entries[1] = 1
}

// Not found: a type parameter's map from the caller.
func typeParameterArgument[Map ~map[int]int](entries Map) {
	entries[1] = 1
}

// Found: the nil map of a function literal's own.
var literal = func() {
	var entries map[string]string
	entries["a"] = "b"
}
