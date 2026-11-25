// Package parity holds the negations SA4013 judges, flagged and left alone, for the port and the original to judge
// side by side: a boolean negated twice, with parentheses around either negation or the operand, in runs of three and
// four, of every kind of operand, and the unary expressions the pattern refuses for their operator.
package parity

type flag bool

func check() bool { return true }

func flagged(first bool, second bool, values map[string]bool, pointer *bool, named flag) bool {
	_ = !!first
	_ = !(!first)
	_ = !(!(first))
	_ = (!!first)
	_ = !!(first)
	_ = !!(first && second)
	_ = !!(first == second)
	_ = !!!first
	_ = !!!!first
	_ = !!check()
	_ = !!values["key"]
	_ = !!*pointer
	_ = !!named
	_ = !!true
	_ = first && !!second
	_ = !!first == second
	_ = func() bool { return !!second }
	if !!first {
		return true
	}
	for !(!second) {
	}
	return !!first
}

// precedence shows the original's second fix dropping the parentheses x stood in: replacing !!(first || second) with
// first || second leaves first || second && third, which groups differently.
func precedence(first bool, second bool, third bool) bool {
	return !!(first || second) && third
}

func notFlagged(first bool, number int, channel chan bool) {
	_ = !first
	_ = !first && !first
	_ = !(first && !first)
	_ = !(!first == true)
	_ = -(-number)
	_ = ^^number
	_ = !<-channel
	_ = - -number
	_ = !first != !first
}

func generic[Boolean ~bool](value Boolean) Boolean {
	return !!value
}

func instantiate() {
	_ = generic(true)
	_ = generic(flag(false))
}
