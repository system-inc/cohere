// Package instantiated holds a generic function that says more than the defaults of its signature, read through its
// instantiation by a function declared before it. The push walks Use first and reads Make's fact before it has judged
// Make, so Use gets the defaults, where a walk that judged Make first would give Use Make's fact. A question of Use
// replays the push, and the order bound names Make.
package instantiated

// Box is a value results point at.
type Box[T any] struct {
	Value T
}

// Use returns what Make[int] returns, through the instantiation, a function of no package that stands for Make.
func Use() *Box[int] {
	return Make[int]()
}

// Make is never nil.
func Make[T any]() *Box[T] {
	return &Box[T]{}
}
