// Package parity holds the loops SA5002 judges, flagged and left alone, for the port and the original to judge side by
// side: every kind of condition MayHaveSideEffects answers, the constants the original spares, and loops that are
// not empty, not plain or not for statements at all.
package parity

type point struct {
	x    int
	done bool
}

type switchState bool

const (
	off                  = false
	on                   = true
	disabled switchState = false
)

var origin point

func check() bool { return true }

func spinning(values []int, flag bool, counter int, channel chan bool, boxed interface{}, pointer *bool) {
	for {
	}
	for {
		// A comment is no statement, so the body is still empty.
	}
	for true {
	}
	for on {
	}
	for !(false) {
	}
	for flag {
	}
	for !flag {
	}
	for counter < 10 {
	}
	for counter != 0 && flag {
	}
	for values[0] > 0 {
	}
	for origin.done {
	}
	for (point{}) == origin {
	}
	for boxed.(bool) {
	}
	for *pointer {
	}
	for values[1:][0] == 0 {
	}
	go func() {
		for {
		}
	}()
	switch {
	default:
		for {
		}
	}
}

func spared(values []int, flag bool, channel chan bool, counter int) {
	for false {
	}
	for off {
	}
	for disabled {
	}
	for check() {
	}
	for len(values) > 0 {
	}
	for bool(flag) {
	}
	for <-channel {
	}
	for &counter == nil {
	}
	for func() bool { return flag }() {
	}
	for index := 0; ; {
		_ = index
	}
	for ; ; counter++ {
	}
	for {
		break
	}
	for flag {
		counter++
	}
	for range values {
	}
}
