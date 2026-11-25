// Package differences holds the shapes where the port parts from the original, findings the original makes wrongly,
// which the port drops, each recorded on its line as a drop with the original's message and the reason above it,
// and the shapes nearest them, where the two agree.
package differences

import (
	"differences/box"
)

type from struct {
	a int
	b int
}

type into struct {
	a int
	b int
}

// alias and twin both stand for into.
type alias = into

type twin = into

func addressed() {
	var x from
	// Go reads the address of a parenthesized literal as the literal's own, `&into{x.a, x.b}`, which the original
	// declines since a conversion has no address, and the original's fix writes `&(into(x))`, which does not compile.
	_ = &(into{x.a, x.b}) // port drops: should convert x (type from) to into instead of using struct literal
	// Both decline the address of the literal as written, and both report the literal in parentheses alone.
	_ = &into{x.a, x.b}
	_ = (into{x.a, x.b})
}

func aliases() {
	var x into
	// alias is into, so the literal copies a value of its own type, and the original, comparing the two types as
	// pointers, suggests converting x to the type it has.
	_ = alias{x.a, x.b} // port drops: should convert x (type into) to alias instead of using struct literal
	var y alias
	// The same the other way around.
	_ = into{y.a, y.b} // port drops: should convert y (type alias) to into instead of using struct literal
	// Two aliases of one type are that type, written twice.
	_ = twin{y.a, y.b} // port drops: should convert y (type alias) to twin instead of using struct literal
	// The very alias on both sides is one pointer, which both decline, and another type both report.
	_ = alias{y.a, y.b}
	var z from
	_ = alias{z.a, z.b}
}

func instances() {
	x := box.Instance
	// x's type is the instance box's type checker made, and the literal's the one this package's made: one type
	// that the original, comparing pointers, takes as two. The message qualifies the type by its package's path, which
	// makes the record's line the one longer than 120 columns.
	_ = box.Of[int]{ // port drops: should convert x (type differences/box.Of[int]) to differences/box.Of[int] instead of using struct literal
		A: x.A,
		B: x.B,
	}
	// An instance this package makes twice is one pointer, which both decline.
	y := box.Of[int]{}
	_ = box.Of[int]{y.A, y.B}
}
