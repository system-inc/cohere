// Package differences holds the loops where the port and the original could plausibly disagree, and records no
// difference, since there is none: both resolve labels by their objects, walk a function literal as part of the
// function holding it and as itself, and stop at the same loops. A label name reused in a nested literal, a literal
// in a loop the outer walk never enters, literals nested twice, and a loop in a deferred and in a spawned literal are
// judged alike.
package differences

func condition() bool { return true }

// Found by both, the outer break once and the inner twice: the literal reuses the outer function's label name for a
// loop of its own, and each labelled break names the loop of its own function.
func reusedLabel() {
again:
	for {
		func() {
		again:
			for {
				if condition() {
					println()
				}
				break again
			}
		}()
		if condition() {
			println()
		}
		break again
	}
}

// Found by both, once: the outer loop has no exit, so the walk of the function never enters it, and the literal in it
// is found only when it is walked as itself.
func literalInLoopWithoutExit() {
	for {
		println()
		func() {
			for {
				if condition() {
					println()
				}
				break
			}
		}()
	}
}

// Found by both, three times at the same break: the function, the outer literal and the inner literal each walk it.
func nestedTwice() {
	func() {
		func() {
			for {
				if condition() {
					println()
				}
				break
			}
		}()
	}()
}

// Found by both, twice each: a loop in a deferred literal and in a spawned one.
func deferredAndSpawned() {
	defer func() {
		for {
			if condition() {
				println()
			}
			return
		}
	}()
	go func() {
		for {
			if condition() {
				println()
			}
			return
		}
	}()
}

// Not found by either: the literal's unlabelled continue gives up on the outer loop, though it continues the
// literal's own loop, and the literal's own loop has no exit.
func continueInLiteral() {
	for {
		if condition() {
			func() {
				for {
					if condition() {
						continue
					}
					println()
				}
			}()
		}
		break
	}
}
