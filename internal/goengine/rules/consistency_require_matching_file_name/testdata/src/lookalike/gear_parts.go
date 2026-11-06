package main

// Every shape here mentions Gear and declares no method of it, so this file serves no type and has no name to match,
// although its name looks like a theme of Gear's.

// A function returning one, and a method expression and a method value of it, declare nothing.
func NewGear() *Gear {
	return &Gear{}
}

var turn = (*Gear).Turn

var turnSpare = NewGear().Turn

// An alias declares no type of its own, and a type defined from Gear inherits none of its methods.
type Shortcut = Gear

type Spare Gear

// A struct embedding a type with methods is promoted those methods but declares none.
type Car struct {
	*Gear
}
