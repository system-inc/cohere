// Package differences holds the shapes where the port and the original could most plausibly part, and records the
// one place they do: a length that receives from a channel, which the original reports and the port drops.
package differences

type alias = chan int

type both interface {
	chan int | <-chan int
}

type pipe[Element any] chan Element

// A package variable's initializer is a call like any other.
var global = make(chan int, 0)

func use(values ...any) {}

// An alias of a channel type renders by the alias's name, and a channel of a type parameter's element is a channel.
func aliases[Element any]() {
	_ = make(alias, 0)
	_ = make(chan Element, 0)
	_ = make(pipe[Element], 0)
}

// A named constraint whose channels agree on their element and differ in direction has a core type, a channel.
func constrained[Channel both]() {
	_ = make(Channel, 0)
}

// A type declared in the function, and calls inside other calls and function literals.
func nested(length int) {
	type local chan int
	use(make(local, 0))
	func() {
		_ = func() chan int {
			return make(chan int, 0)
		}
	}()
	_ = make([]int, len(make([]int, 1, 1)), len(make([]int, 1, 1)))
}

// Lengths read anew for the capacity, which the original's `TODO` says should not be the same. Each receive takes the
// next value from the channel, so the capacity may differ from the length, and the original's make with one size
// receives once where the code receives twice; the port drops these.
func received(sizes chan int, slices chan []int, size func(int) int) {
	// The length and the capacity are two receives, two values.
	_ = make([]int, <-sizes, <-sizes) // port drops: should use make([]int, <-sizes) instead
	// The receive is an argument of the call, made once for each size.
	_ = make([]int, size(<-sizes), size(<-sizes)) // port drops: should use make([]int, size(<-sizes)) instead
	// Each len reads the length of another received slice.
	_ = make([]int, len(<-slices), len(<-slices)) // port drops: should use make([]int, len(<-slices)) instead
}

// Sizes both report: a receive inside a function literal that is never called receives nothing, a call stays
// reported as the original reports it, since only a purity pass could say it answers anew, and comments are no
// syntax.
func agreed(sizes chan int, length int, size func() int) {
	_ = make([]int, len([]func(){func() { <-sizes }}), len([]func(){func() { <-sizes }}))
	_ = make([]int, size(), size())
	_ = make([]int, 1<<3, 1<<3)
	_ = make([]int, length /* first */, length /* second */)
}

// A method of a generic type, where the type parameter's channel is a channel.
type queue[Channel ~chan int] struct{}

func (queue[Channel]) make() Channel {
	return make(Channel, -(0))
}
