// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation. The original reads the body of
// the started function from inside the walk of the function that starts it, where the port does the same from a
// function judged alone, so they could disagree where one function is started from many places, from itself, or from
// a function judged before or after it. One go statement reporting many calls repeats one message with a related
// entry for each, so they could disagree on which related entry goes with which.
package differences

import "testing"

// Found by both: one go statement, two findings with one message, told apart by their related entries.
func repeatedMessage(t *testing.T, condition bool) {
	go func() {
		if condition {
			t.Fatal("first")
		} else {
			t.Fatal("second")
		}
	}()
}

// Found by both: a function that starts itself.
func recursive(t *testing.T, depth int) {
	if depth > 0 {
		go recursive(t, depth-1)
	}
	t.FailNow()
}

// Found by both: started before its declaration and after it.
func before(t *testing.T) {
	go later(t)
}

func later(t *testing.T) {
	t.SkipNow()
}

func after(t *testing.T) {
	go later(t)
}

// Found by both: started from a closure that is never run, a function of its own that both judge.
func neverRun(t *testing.T) {
	_ = func() {
		go func() {
			t.Fatal("failed")
		}()
	}
}
