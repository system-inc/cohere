// Package shadow declares a make of its own at package scope, which is not the universe's, so neither query matches
// a call of it, though each argument would match the query it is shaped for.
package shadow

func make(channel chan int, sizes ...int) chan int {
	return channel
}

func calls(input chan int) {
	_ = make(input, 0)
	_ = make(input, 1, 1)
}
