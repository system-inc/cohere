// Package parity holds the shapes SA4031 depends on: which if statements' conditions it matches, which function each
// is judged in, and which values it knows are never nil, with the related places it gives them. Each function's
// comment says what the original makes of it; the parity test holds only that the port makes the same.
package parity

type record struct {
	field   *int
	handler func()
}

func (record) method() {}

type generic[T any] struct {
	value T
}

func use(values ...any) {}

func produce() *int { return nil }

func first() {}

func second() {}

var functionVariable = first

// Found in each, with the related place of each value: a new, makes of each kind, an address of a literal and a
// slicing of a literal.
func allocations() {
	pointer := new(int)
	slice := make([]int, 0)
	table := make(map[string]int)
	channel := make(chan int)
	literal := &record{}
	sliced := []int{1, 2}
	if pointer != nil {
	}
	if slice == nil {
	}
	if table != nil {
	}
	if channel == nil {
	}
	if literal != nil {
	}
	if sliced != nil {
	}
}

// Found in each: parentheses around a side of the condition are stripped as the original's pattern strips them.
// gofmt drops parentheses around a whole condition, and pairs of them, so a formatted fixture cannot hold those.
func parentheses() {
	pointer := new(int)
	if pointer != nil {
	}
	if (pointer) != (nil) {
	}
	if (pointer) == nil {
	}
}

// Not found: nil on the left, a typed nil, and a comparison outside an if statement are no shape the original
// matches.
func otherShapes() bool {
	pointer := new(int)
	if nil != pointer {
	}
	if pointer != (*int)(nil) {
	}
	return pointer != nil
}

// Not found: a nil that shadows the universe's is no nil to the original.
func shadowedNil() {
	nil := (*int)(nil)
	pointer := new(int)
	if pointer != nil {
	}
}

// Found: the condition of an if statement with an init statement, and of an else if.
func initAndElse(condition bool) {
	if pointer := new(int); pointer != nil {
	}
	if condition {
	} else if table := make(map[int]int); table == nil {
	}
}

// Not found: a condition joined with && is no comparison with nil.
func joined(condition bool) {
	pointer := new(int)
	if pointer != nil && condition {
	}
}

// Not found: the address of a variable is SA4022's, which the original leaves to it, parenthesized or not.
func addressTaken() {
	var value int
	if &value != nil {
	}
	if (&value) == nil {
	}
}

// Found: a field's address of a local, related.
func fieldAddress() {
	var local record
	address := &local.field
	if address != nil {
	}
}

// Not found: a field's address through a pointer parameter, which may be nil.
func fieldAddressOfParameter(pointer *record) {
	address := &pointer.field
	if address != nil {
	}
}

// Not found: values the function did not make, a parameter, a call's result, a loaded field, a global, an
// interface.
func notMadeHere(parameter *int, local record) {
	result := produce()
	var boxed any = new(int)
	if parameter != nil {
	}
	if result != nil {
	}
	if local.field != nil {
	}
	if functionVariable != nil {
	}
	if boxed != nil {
	}
}

// Found in each: a function literal, a closure, a named function held in a variable, a bound method, and the
// function's own name and a method expression, which the original names apart.
func functions() {
	captured := 1
	literal := func() {}
	closure := func() { use(captured) }
	named := first
	bound := record{}.method
	if literal != nil {
	}
	if closure != nil {
	}
	if named != nil {
	}
	if bound != nil {
	}
	if first != nil {
	}
	if record.method != nil {
	}
}

// Found: a phi of two named functions, related to each, though neither belongs to this function.
func phiOfFunctions(condition bool) {
	var chosen func()
	if condition {
		chosen = first
	} else {
		chosen = second
	}
	if chosen != nil {
	}
}

// Found: phis of makes across a loop and a branch, related to every make.
func phiOfMakes(count int, condition bool) {
	values := make([]int, 0)
	for index := 0; index < count; index++ {
		if condition {
			values = make([]int, index)
		} else {
			values = values[:0]
		}
	}
	if values != nil {
	}
}

// Not found: one edge of the phi is nil.
func phiWithNil(condition bool) {
	pointer := new(int)
	if condition {
		pointer = nil
	}
	if pointer != nil {
	}
}

// Found: inside a closure, its own allocation, judged in the closure; not found: the variable it captures, which is
// no value of the closure's.
func closures() func() {
	outer := new(int)
	return func() {
		inner := new(int)
		if inner != nil {
		}
		if outer != nil {
		}
	}
}

// Found: a closure inside the condition's init statement belongs to the function the if statement is in.
func closureInInit() {
	if handler := func() {}; handler != nil {
	}
}

// Found: in a generic function.
func genericFunction[T any]() {
	pointer := new(T)
	if pointer != nil {
	}
}

// Found: in a method of a generic type.
func (receiver generic[T]) genericMethod() {
	pointer := &receiver.value
	if pointer != nil {
	}
}

// Found: in an explicit init function, of which a package may have many.
func init() {
	pointer := new(int)
	if pointer != nil {
	}
}

func init() {
	table := make(map[string]bool)
	if table == nil {
	}
}

// Found: in a package-level initializer's closure.
var initializer = func() {
	pointer := new(int)
	if pointer != nil {
	}
}

// Not found: staticcheck builds a function named _, but ir.EnclosingFunction never finds it among the package's
// members, so the original judges nothing in it, nor in the literal inside it.
func _() {
	pointer := new(int)
	if pointer != nil {
	}
	func() {
		table := make(map[int]int)
		if table != nil {
		}
	}()
}

// Not found: a method named _ is in no method set, so the original finds no function for it either.
func (record) _() {
	pointer := new(int)
	if pointer != nil {
	}
}
