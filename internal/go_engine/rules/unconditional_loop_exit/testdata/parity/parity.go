// Package parity holds the loops the original and the port must judge alike: every exit and branching statement the
// check reads, labels pointing at this loop and at another, every kind of ranged type and type parameter, and the
// shapes the original leaves alone.
package parity

func condition() bool { return true }

// Found: an if, then a break at the top level.
func breakAfterIf() {
	for {
		if condition() {
			println()
		}
		break
	}
}

// Found: a return is an exit too.
func returnAfterIf() int {
	for {
		if condition() {
			println()
		}
		return 1
	}
}

// Found once, at the return: only the last exit at the top level is reported.
func lastExitReported() int {
	for {
		if condition() {
			println()
		}
		break
		return 1
	}
	return 0
}

// Found: each branching statement the check reads counts.
func everyBranching(values []int, channel chan int) {
	for {
		for range values {
		}
		break
	}
	for {
		for index := 0; index < 1; index++ {
		}
		break
	}
	for {
		switch {
		case condition():
		}
		break
	}
	for {
		select {
		case <-channel:
		}
		break
	}
}

// Not found: a type switch, a labelled statement and a block are not branching the check reads.
func notBranching() {
	for {
		switch any(1).(type) {
		case int:
		}
		break
	}
	for {
	inner:
		switch {
		case condition():
			break inner
		}
		break
	}
	for {
		{
			if condition() {
				println()
			}
		}
		break
	}
}

// Found: a break labelled with this loop exits it.
func labelledBreak() {
outer:
	for {
		if condition() {
			println()
		}
		break outer
	}
}

// The inner loop's break names the outer loop, so it is no exit of the inner one, which has none; the outer loop's
// own break, after the inner loop, is found.
func breakOfOuterLoop() {
outer:
	for {
		for {
			if condition() {
				println()
			}
			break outer
		}
		break
	}
}

// Not found: a continue at the top level, unlabelled or naming this loop, runs it again.
func continueAtTopLevel() {
	for {
		if condition() {
			println()
		}
		continue
		break
	}
outer:
	for {
		if condition() {
			println()
		}
		continue outer
		break
	}
}

// The inner loop's continue names the outer loop, so the inner loop is found and the outer one is not.
func continueOfOuterLoop() {
outer:
	for {
		for {
			if condition() {
				continue outer
			}
			break
		}
		break
	}
}

// Not found: an unlabelled continue anywhere, even in a nested loop or a function literal, gives up.
func continueNested(values []int) {
	for {
		if condition() {
			for range values {
				continue
			}
		}
		break
	}
	for {
		if condition() {
			func() {
				for {
					continue
				}
			}()
		}
		break
	}
}

// Not found: a goto anywhere gives up, even one in a function literal.
func gotoAnywhere() {
	for {
		if condition() {
			goto done
		}
		break
	}
done:
	for {
		if condition() {
			func() {
				goto inside
			inside:
			}()
		}
		break
	}
}

// Not found: a body of one statement, or one with no branching, or one with no exit.
func leftAlone(values []int) {
	for range values {
		break
	}
	for {
		println()
		break
	}
	for {
		if condition() {
			break
		}
		println()
	}
}

// Not found: the outer loop has no exit at its top level, so the original walks no further into it and the inner
// loop, which would be found alone, goes unjudged.
func nestedInLoopWithoutExit() {
	for {
		println()
		for {
			if condition() {
				println()
			}
			break
		}
	}
}

// Found twice: the outer loop exits at its top level, so the walk goes on into it, and the inner loop is found too.
func nestedInLoopWithExit() {
	for {
		for {
			if condition() {
				println()
			}
			break
		}
		break
	}
}

// Found twice at the same break: the literal is walked as part of the function and as itself, as the original does.
func nestedLiteral() {
	run := func() {
		for {
			if condition() {
				println()
			}
			break
		}
	}
	run()
}

type element struct{}

// Found: a method's loop.
func (element) method() {
	for {
		if condition() {
			println()
		}
		break
	}
}

// Found over a slice, a string, an integer, an array, a pointer to an array and a channel; not over a map or a
// function.
func rangedKinds(values []int, text string, array [3]int, channel chan int, table map[int]int,
	sequence func(func(int) bool)) {
	for range values {
		if condition() {
			println()
		}
		break
	}
	for range text {
		if condition() {
			println()
		}
		break
	}
	for range 10 {
		if condition() {
			println()
		}
		break
	}
	for range array {
		if condition() {
			println()
		}
		break
	}
	for range &array {
		if condition() {
			println()
		}
		break
	}
	for range channel {
		if condition() {
			println()
		}
		break
	}
	for range table {
		if condition() {
			println()
		}
		break
	}
	for range sequence {
		if condition() {
			println()
		}
		break
	}
}

// Found over a type parameter whose types are strings or integers; not over one whose types are maps or functions.
func rangedTypeParameters[Text ~string, Integer ~int, Table ~map[int]int, Sequence ~func(func(int) bool)](
	text Text, integer Integer, table Table, sequence Sequence) {
	for range text {
		if condition() {
			println()
		}
		break
	}
	for range integer {
		if condition() {
			println()
		}
		break
	}
	for range table {
		if condition() {
			println()
		}
		break
	}
	for range sequence {
		if condition() {
			println()
		}
		break
	}
}

// Found: a package-level function literal, judged as itself alone.
var literal = func() {
	for index := 0; index < 3; index++ {
		if condition() {
			println()
		}
		return
	}
}
