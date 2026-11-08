package parity

// Pointers to struct literals whose only other use is a read of a field through the pointer that nothing uses. A
// field read takes the field's address, a use staticcheck does not lift, so the literal's store stays and deadWrites
// judges it: a read of another field than the one written leaves the write dead in both forms, and a read of the same
// field keeps it.

type record struct {
	field int
	other int
	inner machine
	array [2]int
	slice []int
}

// Another field read into the blank identifier.
func fieldReadToBlank() {
	good := &record{other: 1}
	_ = good.field
}

// The written field read into the blank identifier.
func writtenFieldReadToBlank() {
	good := &record{field: 1}
	_ = good.field
}

// A field of a nested struct read into the blank identifier.
func nestedFieldReadToBlank() {
	good := &record{other: 1}
	_ = good.inner.label
}

// An element of an array field read into the blank identifier.
func arrayFieldElementReadToBlank() {
	good := &record{other: 1}
	_ = good.array[0]
}

// The length of a slice field read into the blank identifier.
func sliceFieldLengthToBlank() {
	good := &record{other: 1}
	_ = len(good.slice)
}

// A field read as the argument of an expression statement's call.
func fieldReadAsArgument() {
	good := &record{other: 1}
	print(good.field)
}

// The written field read as the argument of an expression statement's call.
func writtenFieldReadAsArgument() {
	good := &record{field: 1}
	print(good.field)
}
