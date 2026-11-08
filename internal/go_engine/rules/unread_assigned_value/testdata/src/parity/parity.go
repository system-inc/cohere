// Package parity holds the shapes SA4006 depends on: which assignments and increments have a value of their own in
// the function's representation, what reads such a value, through phis and switch tags, and the forms that assign
// several values at once. Each line's comment says what the original makes of it; the parity test holds only that the
// port makes the same.
package parity

type holder struct {
	count int
	value int
}

func use(values ...any) {}

func produce() int { return 1 }

func pair() (int, error) { return 1, nil }

// Found: a value overwritten before any read.
func overwritten() {
	value := produce() // Found.
	value = produce()
	use(value)
}

// Not found: a constant, the zero value the original leaves alone.
func constant() {
	value := 0
	value = produce()
	use(value)
}

// Found: operating assignments nothing reads after; the increment is read by the next.
func increments(count int, text string) {
	count++    // Not found: count += 2 reads it.
	count += 2 // Found: the value it holds after is never read.
	text += "" // Found.
	use()
}

// Not found: a value read on one path only, through a phi.
func onePath(condition bool) {
	value := produce()
	if condition {
		use(value)
	}
}

// Found: a value a loop overwrites on every path before reading.
func inLoop(rows int) {
	value := produce() // Found.
	for row := 0; row < rows; row++ {
		value = row
		use(value)
	}
}

// Not found: a switch's tag reads its value, cases or not.
func switchTag() {
	value := produce()
	switch value {
	}
	other := produce()
	switch other {
	case 1:
	}
}

// Found, the forms that assign several values: a call, a comma-ok map read, an assertion and a receive, each
// overwritten in part before any read.
func several(table map[string]int, thing any, channel chan int) {
	first, err := pair() // Found twice.
	first, err = pair()
	use(first, err)
	value, isPresent := table[""] // Found: value only.
	value = 2
	use(value, isPresent)
	number, isNumber := thing.(int) // Found: isNumber only.
	isNumber = true
	use(number, isNumber)
	received, isOpen := <-channel // Found twice.
	received, isOpen = 0, false
	use(received, isOpen)
}

// Not found: the blank identifier, a field, a pointer and an element are no named variables.
func notVariables(target *holder, values []int) {
	_ = produce()
	target.value = produce()
	*target = holder{}
	values[0] = produce()
	target.count++
}

// Not found: a variable a closure captures lives in memory, and the store reads the value.
func captured() {
	value := produce()
	value = produce()
	func() { use(value) }()
}

// Not found: a variable whose address is taken.
func addressTaken() {
	value := produce()
	pointer := &value
	value = produce()
	use(pointer)
}

// Found inside a closure, judged as the closure's own function.
func inClosure() {
	use(func() {
		value := produce() // Found.
		value = produce()
		use(value)
	})
}

// Not found: a named result is read by the return.
func namedResult() (result int) {
	result = produce()
	return
}

// Not found: copying a parameter gives the variable the parameter's own value, which the later read uses.
func generic[Value any](value Value) {
	copied := value
	copied = value
	use(copied)
}

// Not found: a declaration with var is no assignment.
func declared() {
	var value = produce()
	value = produce()
	use(value)
}

// Not found: both values meet in a phi, and staticcheck's representation keeps the blank assignment as a BlankStore
// of it, a referrer that is no phi and so a read.
func meetBlank(condition bool) {
	value := produce()
	if condition {
		value = produce()
	}
	_ = value
}

// Found twice: after the first value is read, the next two meet in a phi nothing reads.
func meetUnread(condition bool) {
	value := produce()
	use(value)
	value = produce() // Found.
	if condition {
		value = produce() // Found.
	}
}
