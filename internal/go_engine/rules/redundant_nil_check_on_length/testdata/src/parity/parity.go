// Package parity holds every shape the query and the original's judgments branch on, and the near miss of each,
// where the port and the original agree.
package parity

import (
	"parity/constants"
	. "parity/constants"
)

type names []string

type lookup map[string]int

type pipe chan int

type holder struct {
	values  []int
	table   map[string]int
	pointer *[3]int
	nested  struct{ values []int }
}

const (
	zero  = 0
	one   = 1
	three = 3
	rune0 = '\x00'
	hex0  = 0x0
	big   = 1 << 40
)

func gen() []int { return nil }

func receive(channel chan []int) {
	if <-channel == nil || len(<-channel) == 0 { // a receive has side effects
	}
}

// Every comparison the original takes, beside || and beside &&, against zero and a positive bound.
func operators(values []int) {
	if values == nil || len(values) == 0 { // reported
	}
	if values == nil || len(values) == 3 { // a nil one is not of length 3
	}
	if values == nil || len(values) <= 0 { // reported
	}
	if values == nil || len(values) <= 3 { // reported
	}
	if values == nil || len(values) < 3 { // reported
	}
	if values == nil || len(values) < 0 { // never true
	}
	if values == nil || len(values) != 0 { // not an operator the original takes beside ||
	}
	if values == nil || len(values) > 0 { // not an operator the original takes beside ||
	}
	if values == nil || len(values) >= 0 { // not an operator the original takes beside ||
	}

	if values != nil && len(values) != 0 { // reported
	}
	if values != nil && len(values) != 3 { // a nil one is not of length 3
	}
	if values != nil && len(values) == 3 { // reported
	}
	if values != nil && len(values) == 0 { // a nil one is of length 0
	}
	if values != nil && len(values) > 0 { // reported
	}
	if values != nil && len(values) > 3 { // reported
	}
	if values != nil && len(values) >= 3 { // reported
	}
	if values != nil && len(values) >= 0 { // always true
	}
	if values != nil && len(values) < 3 { // not an operator the original takes beside &&
	}
	if values != nil && len(values) <= 3 { // not an operator the original takes beside &&
	}

	// The nil check and the outer operator must agree.
	if values != nil || len(values) == 0 {
	}
	if values == nil && len(values) != 0 {
	}
	// Nor is the length check on the left.
	if len(values) == 0 || values == nil {
	}
	if nil == values || len(values) == 0 { // nil on the left binds x to nil
	}
}

// The bound is a literal in any base, a named constant, a dot-imported one or a rune constant; a selector, a
// conversion, a negation, arithmetic or a variable is never read.
func bounds(values []int, count int) {
	if values == nil || len(values) == zero { // reported
	}
	if values != nil && len(values) == one { // reported
	}
	if values != nil && len(values) != hex0 { // reported
	}
	if values != nil && len(values) != 0x0 { // reported
	}
	if values != nil && len(values) != 0o0 { // reported
	}
	if values != nil && len(values) != 0b0 { // reported
	}
	if values == nil || len(values) == rune0 { // reported
	}
	if values == nil || len(values) < big { // reported
	}
	if values == nil || len(values) == Zero { // reported
	}
	if values != nil && len(values) > Three { // reported
	}
	if values == nil || len(values) == constants.Zero { // a selector is never read
	}
	if values == nil || len(values) == int(0) { // a conversion is never read
	}
	if values == nil || len(values) == -0 { // a negation is never read
	}
	if values == nil || len(values) == zero+0 { // arithmetic is never read
	}
	if values != nil && len(values) > count { // a variable is never read
	}
	if values != nil && len(values) > len(names{}) { // a call is never read
	}
	if values != nil && len(values) > 2.0 { // reported
	}
	if values == nil || len(values) < 3.0 { // reported
	}
}

// Parentheses are looked through on every operand, and len's name may be parenthesized.
func parentheses(values []int) {
	if (values == nil) || (len(values) == 0) { // reported
	}
	if ((values) == (nil)) || ((len)((values)) == (0)) { // reported
	}
	if (values != nil && len(values) > 0) && true { // reported
	}
	if true && values != nil && len(values) > 0 { // the left operand of the outer && is no nil check
	}
	if values != nil && len(values) > 0 && true { // reported
	}
	_ = (values == nil || len(values) == 0) // reported
}

// Every kind len and nil share, under a name or not, and a pointer to an array, which len measures though nil.
func kinds(values []int, table map[string]int, channel chan int, receiveOnly <-chan int, sendOnly chan<- int) {
	if table == nil || len(table) == 0 { // reported
	}
	if channel != nil && len(channel) > 0 { // reported
	}
	if receiveOnly != nil && len(receiveOnly) > 0 { // reported
	}
	if sendOnly != nil && len(sendOnly) > 0 { // reported
	}
	var named names
	var mapped lookup
	var piped pipe
	if named == nil || len(named) == 0 { // reported
	}
	if mapped == nil || len(mapped) == 0 { // reported
	}
	if piped == nil || len(piped) == 0 { // reported
	}
	var pointer *[4]int
	if pointer == nil || len(pointer) == 0 { // a pointer to an array
	}
	if pointer != nil && len(pointer) > 0 { // a pointer to an array
	}
	var nilChannel chan int
	if channel == nilChannel || len(channel) == 0 { // not the predeclared nil
	}
	if channel == (chan int)(nil) || len(channel) == 0 { // a conversion of nil is not the predeclared nil
	}
}

// x may be a selector, an index or a slice, anything free of side effects, recalled by its spelling.
func shapes(record holder, records []holder, table map[string][]int, grid [][]int, index int) {
	if record.values == nil || len(record.values) == 0 { // reported
	}
	if record.table != nil && len(record.table) > 0 { // reported
	}
	if record.nested.values == nil || len(record.nested.values) == 0 { // reported
	}
	if record.pointer == nil || len(record.pointer) == 0 { // a pointer to an array
	}
	if records[index].values == nil || len(records[index].values) == 0 { // reported
	}
	if table["key"] == nil || len(table["key"]) == 0 { // reported
	}
	if grid[index] != nil && len(grid[index]) > 0 { // reported
	}
	if grid[index][1:] == nil || len(grid[index][1:]) == 0 { // reported
	}
	if (grid[index]) == nil || len(grid[(index)]) == 0 { // reported
	}
	if grid[index] == nil || len(grid[index+0]) == 0 { // a different spelling
	}
	if record.values == nil || len(records[0].values) == 0 { // a different spelling
	}
	if gen() == nil || len(gen()) == 0 { // a call has side effects
	}
	if gen() != nil && len(gen()) > 0 { // a call has side effects
	}
	if names(nil) == nil || len(names(nil)) == 0 { // a conversion is a call to the original
	}
}

// A block-local len is not the universe's.
func shadowed(values []int) {
	len := func([]int) int { return 0 }
	if values == nil || len(values) == 0 {
	}
}

// A type parameter's every term must be a slice, a map or a channel, and the message names the last term's kind.
func slices[Values ~[]int](values Values) {
	if values == nil || len(values) == 0 { // reported
	}
}

func maps[Table map[string]int | map[string]bool](table Table) {
	if table != nil && len(table) > 0 { // reported
	}
}

func mixed[Either []int | map[int]int | chan int](either Either) {
	if either != nil && len(either) > 0 { // reported
	}
}

func mapLast[Either chan int | []int | map[int]int](either Either) {
	if either == nil || len(either) == 0 { // reported
	}
}

func withPointer[Either []int | *[2]int](either Either) {
	if either == nil || len(either) == 0 { // one term is a pointer to an array
	}
}

type constraint interface {
	~[]string | ~map[string]string
	Len() int
}

func constrained[Values constraint](values Values) {
	if values != nil && len(values) > 0 { // reported
	}
}
