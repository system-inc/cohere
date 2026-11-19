// Package facts holds a body for each way the host's side of the facts' reach reads a package (#5vbs1np): a chain of
// calls, a method called and one taken as a value, a generic function called by an instance, a parameter an answer is
// about, a function declared without a body, whose stub.s makes this a package go build takes, a value whose
// initializer calls, a type only a signature names, an interface's method called, and another package's functions
// called.
package facts

import "example.com/facts/lower"

// platformSize is the size a build's own file would declare.
func platformSize() int { return 8 }

// size calls platformSize and nothing else.
func size() int { return platformSize() }

// Warm calls size and drops what it returns, a finding only where size is pure.
func Warm() { size() }

// started is a value whose initializer calls size, which makes it no body's place.
var started = size()

// Pair takes a slice, the parameter an answer about an odd slice argument is of.
func Pair(values []string) int { return len(values) / 2 }

// Box is a type with methods.
type Box struct{}

// Open is a method a body calls.
func (Box) Open() *Box { return &Box{} }

// Close is a method a body takes as a value and never calls.
func (Box) Close() { /* A box closes by being asked to; there is nothing more to it. */ }

// Open is a function of the method's name, which names another object.
func Open() int { return 1 }

// Use calls the method Open.
func Use(box Box) *Box { return box.Open() }

// Closer takes the method value Close.
func Closer(box Box) func() { return box.Close }

// First is a generic function.
func First[Item any](items []Item) Item { return items[0] }

// Leading calls First's instance of string.
func Leading(names []string) string { return First(names) }

// declared has no body, as a function written in assembly has none.
func declared() int

// Sized calls declared.
func Sized() int { return declared() }

// Label is a type a signature names and no body uses.
type Label string

// Labelled takes a Label and never reads it.
func Labelled(_ Label) int { return 0 }

// Opener is an interface whose method a body calls.
type Opener interface{ Open() *Box }

// Through calls Open through the interface, which names no function with a body.
func Through(opener Opener) *Box { return opener.Open() }

// Deep calls another package's function.
func Deep() int { return lower.Depth() }

// Halved calls another package's function with a slice.
func Halved(names []string) int { return len(lower.Halve(2, names)) }
