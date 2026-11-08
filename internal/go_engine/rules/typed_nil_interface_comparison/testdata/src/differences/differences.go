// Package differences holds the comparisons where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a generic function's instance as the callee, a method expression, a call
// through a method value, a comparison in a closure inside generic code, a value made from a constant, which has no
// syntax to point at, and a phi whose edges are one call.
package differences

type failure struct{}

func (*failure) Error() string { return "failure" }

func typedGeneric[Value any]() error {
	println("not trivial")
	var value *failure
	return value
}

type receiver struct{}

func (receiver) typed() error {
	println("not trivial")
	var value *failure
	return value
}

func plausible(choose bool) {
	_ = typedGeneric[int]() == nil
	_ = receiver.typed(receiver{}) == nil
	bound := receiver{}.typed
	_ = bound() == nil
	var constant interface{} = "text"
	_ = constant != nil
	err := receiver{}.typed()
	if choose {
		_ = err == nil
	}
	_ = err != nil
}

func inGeneric[Value any]() {
	func() {
		_ = receiver{}.typed() == nil
	}()
}
