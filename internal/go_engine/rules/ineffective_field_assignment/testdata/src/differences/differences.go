// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation. The original ranges over a map
// of the writes by field, in an order Go leaves to chance, where its translation ranges over the fields in order;
// these methods write many fields, some more than once, with reads that see only some of the writes, so a finding
// that hung on the order would show here as one side makes and the other does not.
package differences

type wide struct {
	first  int
	second int
	third  int
	fourth int
	fifth  string
	sixth  []int
}

type renamed = wide

func use(values ...any) {}

// Found by both: every field written, two of them read.
func (receiver wide) manyFields() {
	receiver.first = 1
	receiver.second = 2
	receiver.third = 3
	receiver.fourth = 4
	receiver.fifth = "five"
	receiver.sixth = nil
	use(receiver.second, receiver.fifth)
}

// Found by both: fields written in an order apart from their declaration, some twice, across branches.
func (receiver wide) scattered(condition bool) {
	if condition {
		receiver.fourth = 1
		receiver.first = 2
	} else {
		receiver.third = 3
		use(receiver.first)
	}
	receiver.fourth = 4
	receiver.second = 5
	use(receiver.third)
}

// Found by both: through an alias, so the message names the alias.
func (receiver renamed) throughAlias() {
	receiver.sixth = append(receiver.sixth, 1)
	receiver.first++
}
