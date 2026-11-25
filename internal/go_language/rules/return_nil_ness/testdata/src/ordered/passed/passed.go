// Package passed holds a generic function at its floor, no more than the defaults of its signature, read through its
// instantiation inside a replay: Loop and Back call each other, so a question of Loop replays the push, and Loop reads
// Pass through Pass[int]. Every read of Pass answers the defaults, judged or not, and Loop and Back end at their floor
// too, so the order bound names none of them. Pass is declared first, so the replay judges it, as a root, before Loop
// reads it, and its verdict is known.
package passed

// Box is a link of a list.
type Box[T any] struct {
	Next *Box[T]
}

// Pass returns what it is given, which may be nil.
func Pass[T any](box *Box[T]) *Box[T] {
	return box
}

// Loop returns what Pass or Back returns, either of which may be nil.
func Loop(box *Box[int]) *Box[int] {
	if box == nil {
		return Pass(box)
	}
	return Back(box.Next)
}

// Back returns what Loop returns, which may be nil.
func Back(box *Box[int]) *Box[int] {
	return Loop(box)
}
