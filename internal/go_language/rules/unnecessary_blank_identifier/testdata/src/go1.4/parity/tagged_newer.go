//go:build go1.23

package parity

import "iter"

// sequence is a named iterator, whose underlying type is a function.
type sequence func(yield func(int) bool)

// A file tagged newer than the module has its tag's language, which ranges over integers and functions.
func taggedNewer(count int, all iter.Seq[int], pairs iter.Seq2[string, int], named sequence) {
	var key string

	// A range over an integer is a range over a value with a key and no value.
	for _ = range count {
	}
	for _ = range 10 {
	}

	// A range over a function names its iteration variables by their count, so none of these is reported.
	for _ = range all {
	}
	for _, _ = range pairs {
	}
	for key, _ = range pairs {
	}
	for _ = range named {
	}
	for _ = range func(yield func(int) bool) {} {
	}
	_ = key
}

// A type parameter's underlying type is its constraint, never a function, so a range over one whose core type is a
// function is judged as a range over any other value.
func overTypeParameter[Function ~func(yield func(int) bool), Slice ~[]int](function Function, slice Slice) {
	for _ = range function {
	}
	for _ = range slice {
	}
	for index, _ := range slice {
		_ = index
	}
}

// A generic channel receive is a receive like any other.
func receiveGeneric[Element any](ch chan Element) {
	_ = <-ch
	var element Element
	element, _ = <-ch
	_ = element
}
