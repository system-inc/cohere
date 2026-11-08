package parity

type holder struct {
	field int
}

// Every shape of a receive assigned to the blank identifier, and the near miss of each.
func receives(ch chan int, m map[int]int, values []int, pointer *holder) {
	var x int
	var ok bool

	// A comma-ok receive into one variable and the blank, assigned and defined: reported, fixed to the receive
	// into the one variable.
	x, _ = <-ch
	y, _ := <-ch
	// The first left side is anything at all: an index, a selector, a dereference, the blank itself.
	values[0], _ = <-ch
	pointer.field, _ = <-ch
	*(&x), _ = <-ch
	_, _ = <-ch
	// The second left side and the right side are looked through their parentheses.
	x, (_) = <-ch
	x, _ = (<-ch)
	// The receive alone into the blank: reported, fixed to the receive bare, out of its parentheses.
	_ = <-ch
	(_) = <-ch
	_ = (<-ch)
	// A receive from a received channel is one receive, its operand another.
	var channels chan chan int
	_ = <-<-channels
	x, _ = <-<-channels

	// Near misses: a second left side that names a variable, a first left side that is the only blank, the
	// comma-ok kept.
	x, ok = <-ch
	_, ok = <-ch
	// Two right sides, each a receive, are not one receive.
	x, _ = <-ch, <-ch
	_, _ = <-ch, <-ch
	// A map index and a call, whose second result the blank may stand for on purpose.
	x, _ = m[0]
	_ = m[0]
	var function func() (int, bool)
	x, _ = function()
	// A unary that is not a receive.
	_ = -x
	_ = !ok
	// A send is a statement of its own, and a receive in an expression statement assigns nothing.
	ch <- x
	<-ch

	// In a statement's initializer, and in a select's case.
	if _ = <-ch; ok {
	}
	for x, _ = <-ch; ok; x, _ = <-ch {
	}
	select {
	case _ = <-ch:
	case x, _ = <-ch:
	case y, _ := <-ch:
		_ = y
	}
	_, _, _ = x, y, ok
}

// Every shape of a range statement with a blank, and the near miss of each.
func ranges(m map[string]int, values []int, text string, ch chan int, array [3]int, pointer *[3]int) {
	var key string
	var value int

	// A blank key with no value, and a blank key and a blank value, each over a slice, a string and a map. A
	// definition of blanks alone declares nothing, which the type checker refuses, so these assign.
	for _ = range m {
	}
	for _ = range values {
	}
	for _, _ = range m {
	}
	for _, _ = range text {
	}
	// A key, then a blank value, assigned and defined, over every rangeable kind that has a value.
	for key, _ = range m {
	}
	for index, _ := range values {
		_ = index
	}
	for index, _ := range text {
		_ = index
	}
	for index, _ := range array {
		_ = index
	}
	for index, _ := range pointer {
		_ = index
	}
	// A key that is not an identifier.
	var keys [1]string
	for keys[0], _ = range m {
	}
	// A channel ranges with one variable only.
	for _ = range ch {
	}

	// Near misses: no variable, a key alone, a key and a value, a blank key before a value.
	for range m {
	}
	for key = range m {
	}
	for key, value = range m {
	}
	for _, value = range m {
	}
	for key, value := range m {
		_, _ = key, value
	}
	_, _ = key, value
}

// Comments and spacing in and around the deleted ranges, which the fixes take as the source spells them.
func spacing(m map[string]int, ch chan int) {
	var key string
	var x int

	for _ /* key */ = range m {
	}
	for _ /* key */, _ /* value */ = range m {
	}
	for key /* key */, _ /* value */ = range m {
	}
	for key, _ = range // The token's line comment.
	m {
	}

	// A comment on the line and inside the assignment, which the printed copy leaves out, and a right side across
	// lines, which it prints across lines from the source's positions.
	x /* blank */, _ = <-ch // the line's comment
	x, _ = <-ch             /* after */ // and the line's
	_ = <-ch                // the line's comment
	x, _ = <-func() chan int {
		// A comment inside the right side.
		return ch
	}()
	_ = <-func() chan int {
		return ch
	}()
	x, _ = <-(func() chan int { return ch })()
	_, _ = x, key
}
