// Package parity holds the shapes SA4010 depends on: where an append's result goes, and where the slice it extends
// came from. Each function's comment says what the original makes of it; the parity test holds only that the port
// makes the same.
package parity

import "fmt"

type holder struct {
	values []int
	inner  *holder
}

type integers []int

var global []int

func use(values ...any) {}

func keep(values []int) {}

func produce() []int { return nil }

// Found: a local the append assigns, never read again.
func localNeverRead() {
	values := []int{}
	values = append(values, 1)
}

// Found: a slice from make, appended twice, the second extending the first.
func appendedTwice() {
	values := make([]int, 0, 4)
	values = append(values, 1)
	values = append(values, 2)
}

// Not found: a field is memory, and the store reads the result.
func toField(target *holder) {
	target.values = append(target.values, 1)
}

// Not found: a field of a local struct is stored too.
func toLocalField() {
	var local holder
	local.values = append(local.values, 1)
}

// Not found: a store through a pointer.
func throughPointer(pointer *[]int) {
	*pointer = append(*pointer, 1)
}

// Not found: a field reached through two pointers.
func throughNestedPointer(target *holder) {
	target.inner.values = append(target.inner.values, 2)
}

// Found: a loop over a local, its phi read only by the next append.
func inLoop(count int) {
	var values []int
	for index := 0; index < count; index++ {
		values = append(values, index)
	}
}

// Not found: the loop's result is read after it.
func inLoopRead(count int) int {
	var values []int
	for index := 0; index < count; index++ {
		values = append(values, index)
	}
	return len(values)
}

// Found: a range loop with a break, its phis merging on two exits.
func inRangeWithBreak(input []int) {
	var values []int
	for _, value := range input {
		if value < 0 {
			break
		}
		values = append(values, value)
	}
}

// Found: the closure's own local, never read; the closure is a function of its own.
func closureLocal() func() {
	return func() {
		var values []int
		values = append(values, 1)
	}
}

// Not found: the closure appends to a captured variable, which it stores.
func closureCaptured() []int {
	var values []int
	add := func() {
		values = append(values, 1)
	}
	add()
	return values
}

// Not found: a captured variable that nothing reads after is still a store to memory.
func closureCapturedUnread() {
	var values []int
	func() {
		values = append(values, 1)
	}()
}

// Not found: the result escapes through a return.
func viaReturn() []int {
	var values []int
	return append(values, 1)
}

// Not found: through a call.
func viaCall() {
	var values []int
	keep(append(values, 1))
}

// Not found: through a variadic call, boxed.
func viaVariadic() {
	var values []int
	values = append(values, 1)
	fmt.Println(values)
}

// Not found: through a channel send.
func viaSend(channel chan []int) {
	var values []int
	channel <- append(values, 1)
}

// Not found: through a map store.
func viaMapStore(store map[string][]int) {
	var values []int
	store["key"] = append(values, 1)
}

// Not found: through a slice element store.
func viaSliceStore(store [][]int) {
	var values []int
	store[0] = append(values, 1)
}

// Not found: through a global.
func viaGlobal() {
	var values []int
	global = append(values, 1)
}

// Found twice: both appends of the branches flow into a phi nothing reads.
func phiUnread(condition bool) {
	var values []int
	if condition {
		values = append(values, 1)
	} else {
		values = append(values, 2)
	}
}

// Not found: the phi of the two branches is returned.
func phiRead(condition bool) []int {
	var values []int
	if condition {
		values = append(values, 1)
	}
	return values
}

// Found: the phi merges two slices from make.
func phiOfMakes(condition bool) {
	var values []int
	if condition {
		values = make([]int, 0, 1)
	} else {
		values = make([]int, 0, 2)
	}
	values = append(values, 1)
}

// Not found: one edge of the phi is a parameter, whose backing array the caller may share.
func phiWithParameter(condition bool, input []int) {
	values := make([]int, 0, 1)
	if condition {
		values = input
	}
	values = append(values, 1)
}

// Found: a generic function's local of the type parameter's elements.
func genericLocal[T any](value T) {
	var values []T
	values = append(values, value)
}

// Not found: the generic slice is a parameter.
func genericParameter[S ~[]E, E any](values S, value E) {
	values = append(values, value)
}

// Found: a local of a slice type given by a type parameter.
func genericTypeLocal[S ~[]E, E any](value E) {
	var values S
	values = append(values, value)
}

// Not found: a generic function returns what it appends.
func genericReturned[T any](value T) []T {
	var values []T
	values = append(values, value)
	return values
}

// Found: a named slice type.
func namedSlice() {
	var values integers
	values = append(values, 1)
}

// Not found: a composite literal's array is stored into element by element, readers step two refuses.
func literalWithElements() {
	values := []int{1, 2}
	values = append(values, 3)
}

// Not found: the argument is a parameter.
func fromParameter(values []int) {
	values = append(values, 1)
}

// Not found: the argument is a global.
func fromGlobal() {
	values := global
	values = append(values, 1)
}

// Not found: the argument is the result of a call.
func fromCall() {
	values := produce()
	values = append(values, 1)
}

// Not found: the argument is loaded from a field.
func fromField(source holder) {
	values := source.values
	values = append(values, 1)
}

// Not found: the slice was passed to a call before the append, which may keep it.
func retainedBefore() {
	values := make([]int, 0, 2)
	keep(values)
	values = append(values, 1)
}

// Not found: a slicing of the slice is returned and shares its array.
func slicedAndReturned() []int {
	values := make([]int, 4)
	head := values[:2]
	values = append(values, 1)
	return head
}

// Found: a slicing of a local array nothing reads.
func slicedArrayUnread() {
	var array [4]int
	values := array[:0]
	values = append(values, 1)
}

// Not found: the array behind the slicing is read afterwards.
func slicedArrayRead() int {
	var array [4]int
	values := array[:0]
	values = append(values, 1)
	return array[0]
}

// Found twice: the first append's result is only spread into the second, whose result nothing reads.
func spreadIntoAnother() {
	var first, second []int
	first = append(first, 1)
	second = append(second, first...)
}

// Found: an append of itself, spread.
func appendSelf() {
	values := make([]int, 1)
	values = append(values, values...)
}

// Not found: `_ =` is an instruction that uses the result.
func discardedByBlank() {
	var values []int
	_ = append(values, 1)
}

// Not found: the length of the result is read.
func lengthRead() int {
	var values []int
	return len(append(values, 1))
}

// Not found: converted to a string.
func convertedToString() string {
	var bytes []byte
	return string(append(bytes, 'x'))
}

// Found: a string spread into bytes, never read.
func stringSpread() {
	var bytes []byte
	bytes = append(bytes, "text"...)
}

// Not found: the address of the slice is taken, so it lives in memory and the append is stored.
func addressTaken() {
	var values []int
	pointer := &values
	values = append(values, 1)
	_ = pointer
}

// Not found: the result is ranged over.
func rangedOver() int {
	var values []int
	values = append(values, 1, 2)
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

// Not found: unreachable after a return, so never built.
func afterReturn() {
	var values []int
	return
	values = append(values, 1)
}

// Found in each case: a switch whose cases each append to a local nothing reads.
func inSwitch(kind int) {
	var values []int
	switch kind {
	case 0:
		values = append(values, 0)
	case 1:
		values = append(values, 1)
	}
}

// Not found: read in a deferred call.
func readInDefer() {
	var values []int
	defer func() { use(values) }()
	values = append(values, 1)
}

// Found: a method's local.
func (target *holder) methodLocal() {
	var values []int
	values = append(values, len(target.values))
}

// Not found: the result is read as an index of another slice.
func indexedRead() int {
	values := make([]int, 0, 1)
	values = append(values, 7)
	return values[0]
}

// Not found: a goto loop reads the result on its way out.
func gotoLoop(count int) []int {
	var values []int
again:
	values = append(values, count)
	count--
	if count > 0 {
		goto again
	}
	return values
}

// Found: a goto loop whose result nothing reads.
func gotoLoopUnread(count int) {
	var values []int
again:
	values = append(values, count)
	count--
	if count > 0 {
		goto again
	}
}

// Found: a package-level initializer's closure, among the functions both read.
var initializer = func() {
	var values []int
	values = append(values, 1)
}
