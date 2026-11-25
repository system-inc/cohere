package parity

type channel chan int

type receiver <-chan int

type slice []int

type pipe[Element any] chan Element

type holder struct {
	size int
}

func size() int {
	return 1
}

var counter int

// next answers anew on each call, so its result as a length is not its result as a capacity.
func next() int {
	counter++
	return counter
}

// Channels with a size written as the integer literal 0, which both report, at the literal.
func channels() {
	_ = make(chan int, 0)
	_ = make(chan string, 0)
	_ = make(<-chan int, 0)
	_ = make(chan<- int, 0)
	_ = make(channel, 0)
	_ = make(receiver, 0)
	_ = make(pipe[int], 0)
	_ = make(chan struct{}, 0)
	_ = make(chan chan int, 0)
}

// Every spelling of the integer literal 0, which prints as 0 however it is written.
func zeroes() {
	_ = make(chan int, 0x0)
	_ = make(chan int, 0b0)
	_ = make(chan int, 0o0)
	_ = make(chan int, 00)
	_ = make(chan int, 0_0)
	_ = make(chan int, -0)
	_ = make(chan int, +0)
	_ = make(chan int, - -0)
	_ = make(chan int, -(+(-(0))))
}

// Parentheses around the callee, the type and the size, which both look through.
func parenthesized() {
	_ = (make)(chan int, 0)
	_ = make((chan int), 0)
	_ = make(chan int, (0))
	_ = make((channel), (-0))
	_ = make([]int, (1), 1)
	_ = make([]int, 1, (1))
}

// Near misses of the first query: a size that is not the literal 0, or a type that is no channel.
func sizeNearMisses() {
	const zero = 0
	var variable int
	_ = make(chan int, 1)
	_ = make(chan int, zero)
	_ = make(chan int, variable)
	_ = make(chan int, int(0))
	_ = make(chan int, 0.0)
	_ = make(chan int, '\x00')
	_ = make(chan int, 1-1)
	_ = make(chan int, 0*1)
	_ = make(chan int)
	_ = make([]int, 0)
	_ = make(slice, 0)
	_ = make(map[int]int, 0)
	_ = make(map[int]chan int, 0)
	_ = make([]chan int, 0)
}

// Lengths and capacities the same syntax, which both report, at the length.
func capacities(length int, value holder, values []int) {
	_ = make([]int, 0, 0)
	_ = make([]int, 1, 1)
	_ = make([]int, length, length)
	_ = make(slice, length, length)
	_ = make([]string, value.size, value.size)
	_ = make([]int, len(values), len(values))
	_ = make([]int, length+1, length+1)
	_ = make([]int, size(), size())
	// A known false positive both report: the capacity is the next call's answer, which the suggested make([]int,
	// next()) never asks for. Only staticcheck's purity pass could tell next is impure, so the port reports calls as
	// the original does and leaves this for the follow-up once the purity port lands.
	_ = make([]int, next(), next())
	_ = make([]int, values[0], values[0])
	_ = make([]int, length, (length))
	_ = make([]int, (length), length)
}

// Near misses of the second query: a capacity that differs in its syntax, however equal its value.
func capacityNearMisses(length int, other int, value holder) {
	_ = make([]int, 1, 2)
	_ = make([]int, length, other)
	_ = make([]int, 1, 01)
	_ = make([]int, 1, 0x1)
	_ = make([]int, length, length+0)
	_ = make([]int, length+1, 1+length)
	_ = make([]int, value.size, holder{}.size)
	_ = make([]int, length)
}

// A make declared in a block is not the universe's, so neither query matches its calls.
func shadowed(input chan int) {
	make := func(channel chan int, sizes ...int) chan int {
		return channel
	}
	_ = make(input, 0)
	_ = make(input, 1, 1)
}

// A generic channel type, whose core type is a channel of one element, and a generic slice type, whose is not.
func generic[
	Channel chan int,
	Directed interface{ chan int | <-chan int },
	Approximate ~chan int,
	Slice ~[]int,
](length int) {
	_ = make(Channel, 0)
	_ = make(Directed, 0)
	_ = make(Approximate, 0)
	_ = make(Slice, 0)
	_ = make(Slice, length, length)
}

// A length rendered across lines, as both print it, and two function literals, which are never the same syntax, since
// the pattern language never finds a field list with an untagged field the same as itself.
func multiline() {
	_ = make([]int, len([]int{
		1,
	}), len([]int{1}))
	_ = make([]int, func() int {
		return 1
	}(), func() int { return 1 }())
}
