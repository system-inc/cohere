// Package differences holds the shapes where the port and x/tools' nilness disagree, each adjudicated in the port's
// favour. A finding only the port makes is recorded on its line by a "finds" comment with its message. Nothing the
// original finds may be missing from the port: that is a regression, which no comment can record.
package differences

type valueReceiver struct{ value int }

func (receiver valueReceiver) read() int { return receiver.value }

// A method with a value receiver called through a nil pointer reads the value the pointer points to, which panics.
// x/tools calls the method's pointer wrapper, which reads it out of sight, and staticcheck reads it in the caller, so
// the port is right.
func valueMethodCall(pointer *valueReceiver) {
	if pointer == nil {
		print(pointer.read()) // port finds: nil dereference in load
	}
}

// The same for the method taken as a value, which binds the value read through the pointer.
func valueMethodValue(pointer *valueReceiver) {
	if pointer == nil {
		read := pointer.read // port finds: nil dereference in load
		print(read())
	}
}

// staticcheck lifts a variable up to the first use that takes its address, so before it the variable is still the
// nil it was declared, and the comparison is decided. x/tools keeps the whole variable in memory, its value unknown:
// the port is right.
func addressTakenLater(escape func(**int)) {
	var pointer *int
	if pointer == nil { // port finds: tautological condition: nil == nil
		print(1)
	}
	escape(&pointer)
	if pointer == nil {
		print(*pointer)
	}
}

// The same for a variable a closure captures later, which x/tools keeps in memory for the whole function.
func capturedLater() func() *int {
	var pointer *int
	if pointer != nil { // port finds: impossible condition: nil != nil
		print(1)
	}
	return func() *int { return pointer }
}

// The variable is only ever assigned nil, so it is nil at every iteration. staticcheck merges the same constant from
// every edge into that constant, where x/tools keeps a merge it knows nothing of: the port is right.
func onlyEverNil(values []int) {
	var previous *int
	for _, value := range values {
		if previous != nil && value > 0 { // port finds: impossible condition: nil != nil
			print(1)
		}
		if value > 1 {
			previous = nil
		}
	}
}

// buildir's functions include the closures of package-level initializers, which buildssa's leave out: the port is
// right.
var initialized = func() int {
	var pointer *int
	return *pointer // port finds: nil dereference in load
}()
