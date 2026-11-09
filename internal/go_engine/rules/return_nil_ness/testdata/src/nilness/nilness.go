// Package nilness holds the shapes the nilness facts depend on, each function marked with the nilness of its results
// where a fact holds it, and unmarked where every result may be nil: allocations, nil, globals, interfaces made of
// concrete values and of nil, results of several kinds, branches on nil, slicing, appending, maps, closures, type
// switches and assertions, calls to the package's own functions in either order, to itself and in a cycle, and to the
// standard library, whose facts cross into this package, and a loop carrying one value set from another. Most are
// exported; a few are not, which only the facts within the package see.
package nilness

import (
	"errors"
	"fmt"
)

// Node is a value results point at.
type Node struct {
	Kind int
	Next *Node
}

// failure is an error whose pointer may be nil.
type failure struct{}

func (*failure) Error() string { return "failure" }

// shared is a global, whose value may be nil.
var shared *Node

// A fresh allocation is never nil.
func Fresh() *Node { // want Fresh:`nilness: \[\{MaybeNil NeverNil\}\]`
	return &Node{}
}

// Nil is always nil.
func Nil() *Node { // want Nil:`nilness: \[\{MaybeNil AlwaysNil\}\]`
	return nil
}

// A parameter may be nil, which says nothing, so no fact.
func Passed(node *Node) *Node {
	return node
}

// A global's value may be nil, but only because of the global.
func Shared() *Node { // want Shared:`nilness: \[\{MaybeNil MaybeNilGlobal\}\]`
	return shared
}

// A typed nil made into an interface: the interface is never nil, and what it holds always is.
func TypedNil() error { // want TypedNil:`nilness: \[\{AlwaysNil NeverNil\}\]`
	var typed *failure
	return typed
}

// errors.New's fact crosses from the standard library: never nil, holding a value never nil.
func Wrapped() error { // want Wrapped:`nilness: \[\{NeverNil NeverNil\}\]`
	return errors.New("wrapped")
}

// fmt.Errorf may return a nil interface value in no case, but its fact is the standard library's to say.
func Formatted(value int) error {
	if value > 0 {
		return fmt.Errorf("value %d", value)
	}
	return nil
}

// Several results, of several kinds: a value type is never nil, and an untyped nil error always is.
func Pair() (int, *Node, error) { // want Pair:`nilness: \[\{NeverNil NeverNil\} \{MaybeNil NeverNil\} \{AlwaysNil AlwaysNil\}\]`
	return 1, &Node{}, nil
}

// A result of a value type alone says nothing worth a fact.
func Count() int {
	return 1
}

// On the false branch of a check against nil, the parameter is not nil.
func Checked(node *Node) *Node { // want Checked:`nilness: \[\{MaybeNil NeverNil\}\]`
	if node == nil {
		return &Node{}
	}
	return node
}

// Either branch may return nil.
func Either(node *Node) *Node {
	if node != nil {
		return nil
	}
	return &Node{}
}

// Loading through a pointer says the pointer was not nil, but the loaded field may be.
func Dereferenced(node *Node) *Node {
	return node.Next
}

// Slicing from a bound that is not zero succeeds only on a slice that is not nil.
func Sliced(values []int) []int { // want Sliced:`nilness: \[\{MaybeNil NeverNil\}\]`
	return values[1:]
}

// Slicing from zero is as nil as the slicee.
func SlicedFromZero(values []int) []int {
	return values[0:]
}

// Appending to a fresh slice gives one that is not nil.
func Appended() []int { // want Appended:`nilness: \[\{MaybeNil NeverNil\}\]`
	return append([]int{}, 1)
}

// A made map is never nil.
func Made() map[string]int { // want Made:`nilness: \[\{MaybeNil NeverNil\}\]`
	return make(map[string]int)
}

// A closure is never nil, and the anonymous function inside is never judged.
func Closure() func() *Node { // want Closure:`nilness: \[\{MaybeNil NeverNil\}\]`
	return func() *Node { return &Node{} }
}

// A type switch's case for a concrete type extracts what the interface held.
func Switched(value any) error {
	switch typed := value.(type) {
	case *failure:
		return typed
	case nil:
		return &failure{}
	default:
		return nil
	}
}

// A type assertion to an interface gives an interface value never nil.
func Asserted(value any) any { // want Asserted:`nilness: \[\{MaybeNil NeverNil\}\]`
	return value.(fmt.Stringer)
}

// EarlyCaller calls a function declared after it, judged as it is met.
func EarlyCaller() *Node { // want EarlyCaller:`nilness: \[\{MaybeNil NeverNil\}\]`
	return lateCallee()
}

func lateCallee() *Node { // want lateCallee:`nilness: \[\{MaybeNil NeverNil\}\]`
	return new(Node)
}

// A call to itself, already begun, answers that it may be nil, so no fact.
func Recursive(depth int) *Node {
	if depth == 0 {
		return &Node{}
	}
	return Recursive(depth - 1)
}

// Even and Odd call each other: Odd, met while Even is being judged, finds Even already begun, and neither gets a
// fact.
func Even(depth int) *Node {
	if depth == 0 {
		return &Node{}
	}
	return Odd(depth - 1)
}

func Odd(depth int) *Node {
	if depth == 0 {
		return &Node{}
	}
	return Even(depth - 1)
}

// A call through a function value cannot be told, so no fact.
func Dynamic(make func() *Node) *Node {
	return make()
}

// Received from a channel, a value may be nil, so no fact.
func Received(channel chan *Node) *Node {
	return <-channel
}

// A map lookup may give nil.
func Looked(table map[string]*Node) *Node {
	return table["key"]
}

// A loop whose every path returns a fresh value.
func Looped(count int) *Node { // want Looped:`nilness: \[\{MaybeNil NeverNil\}\]`
	node := &Node{}
	for range count {
		node = &Node{Next: node}
	}
	return node
}

// The loop carries outer and inner, outer set from inner. Each phi reads the other's value from before the edge, so
// outer is the inner already dereferenced, never nil, whichever order the phis stand in.
func Innermost(node *Node) *Node { // want Innermost:`nilness: \[\{MaybeNil NeverNil\}\]`
	outer := node
	inner := node.Next
	for inner.Kind == 1 {
		outer = inner
		inner = outer.Next
	}
	return outer
}
