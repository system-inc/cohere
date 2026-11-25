package parity

import "errors"

var outer int

type pair struct {
	first  int
	second int
}

func value() int { return 1 }

func valueAndError() (int, error) { return 1, errors.New("fails") }

// merged holds the shapes both report, each with its fix.
func merged(channel chan int, pointer *int) {
	var named int // a comment the fix deletes
	named = 1
	_ = named

	var empty interface{}
	empty = nil
	_ = empty

	var (
		grouped int
	)
	grouped = 2
	_ = grouped

	var structured pair
	structured = pair{
		first:  1,
		second: 2,
	}
	_ = structured

	var called int
	called = value() +
		value()
	_ = called

	var commented int
	commented = value( /* inside */ ) // after
	_ = commented

	var received int
	received = <-channel
	_ = received

	var pointed *int
	pointed = pointer
	_ = pointed

	// An outer variable spelled alike is another object, and so are its assignments.
	var outer int
	outer = 3
	_ = outer

	// Increments, range clauses and a parenthesized left side count as no second assignment.
	var incremented int
	incremented = 1
	incremented++
	(incremented) = 2
	for incremented = range []int{1, 2} {
	}
	_ = incremented

	// A declared type, and an index on the left, which counts as no second assignment.
	type local int
	var typed local
	typed = 1
	_ = typed

	var element []int
	element = []int{1}
	element[0] = 2
	_ = element

	var function func() int
	function = func() int { return value() }
	_ = function

	if true {
		var nested string
		nested = "nested"
		_ = nested
	} else {
		var other string
		other = "other"
		_ = other
	}

	for range []int{1, 2} {
		var looped int
		looped = 1
		_ = looped
	}

	func() {
		var literal int
		literal = 1
		_ = literal
	}()

	{
		var bare int
		bare = 1
		_ = bare
	}
}

// left holds the near misses neither reports.
func left(channel chan int) {
	var valued int = 1
	valued = 2
	_ = valued

	var first, second int
	first = 1
	_, _ = first, second

	var (
		one int
		two int
	)
	one = 1
	_, _ = one, two

	const constant = 1
	_ = constant

	type local int
	_ = local(1)

	var operated int
	operated += 1
	_ = operated

	var several, others int
	several, others = 1, 2
	_, _ = several, others

	var parenthesized int
	(parenthesized) = 1
	_ = parenthesized

	var unrelated int
	outer = 1
	_ = unrelated

	var reading int
	reading = reading + 1
	_ = reading

	var recursive func() int
	recursive = func() int { return recursive() }
	_ = recursive

	var reassigned int
	reassigned = 1
	reassigned = 2
	_ = reassigned

	var later int
	later = 1
	_ = later
	later = 2

	var closed int
	closed = 1
	func() { closed = 2 }()
	_ = closed

	var redeclared int
	redeclared = 1
	redeclared, failure := valueAndError()
	_, _ = redeclared, failure

	var compound int
	compound = 1
	compound -= 1
	_ = compound

	var tupled int
	tupled = 1
	tupled, _ = valueAndError()
	_ = tupled

	var labeled int
label:
	labeled = 1
	_ = labeled
	goto label

	var _ int
	_ = 1

	var received int
	received, _ = <-channel
	_ = received

	var interrupted int
	_ = outer
	interrupted = 1
	_ = interrupted

	var last int
	_ = last
}

func declaredLast() {
	var last int
	_ = last
}
