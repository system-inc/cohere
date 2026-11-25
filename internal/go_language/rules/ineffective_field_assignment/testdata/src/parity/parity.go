// Package parity holds the shapes SA4005 depends on: how a value receiver is copied and used, and where a write to
// one of its fields sits against the reads of it. Each method's comment says what the original makes of it; the
// parity test holds only that the port makes the same.
package parity

type inner struct {
	a int
	b int
}

type outer struct {
	inner
	x      int
	y      int
	nested inner
	list   []int
	array  [2]int
}

type empty struct{}

type alias = outer

type converted outer

type generic[T any] struct {
	value T
	count int
}

func use(values ...any) {}

func keepPointer(pointer *int) {}

// Found: a write after the last read.
func (receiver outer) afterLastRead() {
	use(receiver.x)
	receiver.x = 1
}

// Not found: the write is read after it.
func (receiver outer) readAfter() {
	receiver.x = 1
	use(receiver.x)
}

// Found twice: a tuple assignment to two fields, neither read again.
func (receiver outer) tuple() {
	receiver.x, receiver.y = 1, 2
}

// Found: an operator assignment reads the field before the write, never after.
func (receiver outer) operatorAssignment() {
	receiver.y += 2
}

// Not found: a write in a branch, read after the branches join.
func (receiver outer) branchThenJoin(condition bool) {
	if condition {
		receiver.x = 1
	}
	use(receiver.x)
}

// Found: the read is in one case, the write in another, which never reaches it.
func (receiver outer) otherCase(kind int) {
	switch kind {
	case 0:
		receiver.x = 1
	case 1:
		use(receiver.x)
	}
}

// Found: the read and the write share the loop's body, the read first, so the original sees no later read even
// though the next turn of the loop makes one.
func (receiver outer) loopBodyReadFirst(count int) {
	for index := 0; index < count; index++ {
		use(receiver.x)
		receiver.x = index
	}
}

// Not found: the loop's condition, in a block of its own, reads the field the body writes.
func (receiver outer) loopConditionReads() {
	for receiver.x < 10 {
		receiver.x++
	}
}

// Not found: a goto loop reads the field again in the block the write jumps back to.
func (receiver outer) gotoLoop() {
again:
	use(receiver.x)
	if receiver.y > 0 {
		receiver.x = 2
		receiver.y--
		goto again
	}
}

// Not found: a pointer receiver is shared with the caller.
func (receiver *outer) pointerReceiver() {
	receiver.x = 1
}

// Not found: the receiver is passed on whole, a read of every field.
func (receiver outer) passedWhole() {
	receiver.x = 1
	use(receiver)
}

// Not found: a method value of the receiver copies it whole.
func (receiver outer) methodValue() func() {
	receiver.x = 1
	return receiver.afterLastRead
}

// Not found: a comparison of the whole receiver reads every field.
func (receiver inner) compared(other inner) bool {
	receiver.b = 3
	return receiver == other
}

// Not found: a closure captures the receiver, which moves to the heap.
func (receiver outer) captured() func() int {
	receiver.x = 1
	return func() int { return receiver.x }
}

// Not found: a field's address is taken, which moves the receiver to the heap.
func (receiver outer) addressTaken() {
	receiver.x = 1
	keepPointer(&receiver.y)
}

// Not found: the receiver is assigned whole, and the original gives up on the method.
func (receiver outer) assignedWhole() {
	receiver.x = 1
	receiver = outer{y: 2}
}

// Not found: a field of an embedded field is nested, and the original considers first level fields only.
func (receiver outer) embeddedField() {
	receiver.a = 1
}

// Found: the embedded field itself, written whole.
func (receiver outer) embeddedWhole() {
	receiver.inner = inner{a: 1}
}

// Found: a field of struct type written whole, never read.
func (receiver outer) nestedWhole() {
	receiver.nested = inner{b: 2}
}

// Not found: an element of an array field is nested.
func (receiver outer) arrayElement() {
	receiver.array[0] = 1
}

// Found: an array field written whole.
func (receiver outer) arrayWhole() {
	receiver.array = [2]int{1, 2}
}

// Not found: the slice field is ranged over after it is written.
func (receiver outer) sliceRanged() int {
	receiver.list = append(receiver.list, 1)
	total := 0
	for _, value := range receiver.list {
		total += value
	}
	return total
}

// Found: the slice field is written after its last read.
func (receiver outer) sliceAppended() {
	receiver.list = append(receiver.list, 1)
}

// Not found: a defer evaluates its arguments at once, a read after the write.
func (receiver outer) deferredArgument() {
	receiver.x = 1
	defer use(receiver.x)
}

// Found: a write after a deferred argument's read, which happens at the defer.
func (receiver outer) writeAfterDefer() {
	defer use(receiver.x)
	receiver.x = 1
}

// Found: a write on a path that returns before the read.
func (receiver outer) returnBeforeRead(condition bool) {
	if condition {
		receiver.x = 1
		return
	}
	use(receiver.x)
}

// Not found: the receiver has no name and nothing reads it.
func (outer) unnamedReceiver() {}

// Not found: a blank receiver.
func (_ outer) blankReceiver() {}

// Not found: an empty struct has no fields to write.
func (receiver empty) emptyStruct() {
	use(receiver)
}

// Found: a method declared on an alias names the alias.
func (receiver alias) onAlias() {
	receiver.y = 4
}

// Found: a type defined from another struct.
func (receiver converted) onConverted() {
	receiver.x = 5
}

// Found: a generic receiver's field of the type parameter.
func (receiver generic[T]) genericValue(value T) {
	receiver.value = value
}

// Not found: a generic receiver's field read after the write.
func (receiver generic[T]) genericRead() int {
	receiver.count++
	return receiver.count
}

// Found: a method of a generic type written after its last read, through a different parameter name.
func (other generic[T]) genericAfterRead() {
	use(other.count)
	other.count = 0
}

// Found: a closure inside a method is no method, but the method's own write is still judged.
func (receiver outer) closureInside() {
	receiver.x = 1
	func() {
		var local outer
		local.x = 2
	}()
}

// Found twice: two writes of one field, neither read again.
func (receiver outer) writtenTwice() {
	receiver.x = 1
	receiver.x = 2
}

// Not found: the second write is read, and the first is seen by the read too, since the read comes after it.
func (receiver outer) writtenTwiceRead() {
	receiver.x = 1
	receiver.x = 2
	use(receiver.x)
}
