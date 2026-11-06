package allowed

import "fmt"

var name = "world"

// A literal joined to a variable, on either side.
var greeting = "hello, " + name
var signed = name + "!"

// A literal joined to a call.
var formatted = "count: " + fmt.Sprint(3)

// A long string wrapped across lines, the + ending the line.
var wrapped = "a long first half of a message that would not fit on one line " +
	"and its second half on the next"

// One literal, the form the rule asks for.
var whole = "hello, world"

// A literal joined to a sum whose element beside the operator is not a literal.
var mixed = "a" + (name + "b")
