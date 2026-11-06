package top

import "example.com/chain/middle"

// Top is the end of the chain.
func Top() string { return "top " + middle.Middle() }
