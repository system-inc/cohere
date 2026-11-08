// Package differences holds where the port's fix parts from the original's, which writes a negation in front of the
// other side's text: where that side is a comparison written without parentheses, the negation binds to its leftmost
// operand, and where that operand is no boolean, the original's fix does not compile, and the port negates the
// comparison whole. Beside each are the shapes where the two agree, a leftmost operand that is a boolean, a
// comparison in parentheses, one not negated. Each record says what the port does, its reason on the line above.
package differences

// numbers compares integers, whose comparison the negation must take whole.
func numbers(a int, b int) {
	// The original writes !a < b, which negates an integer and does not compile; the port writes !(a < b).
	_ = a < b == false // port moves: should omit comparison to bool constant, can be simplified to !a < b
	// The original writes !a+1 >= b, negating an integer under the addition; the port writes !(a+1 >= b).
	_ = a+1 >= b == false // port moves: should omit comparison to bool constant, can be simplified to !a+1 >= b
	// Not negated, the text is the comparison as it is, and the two agree.
	_ = a < b == true
	// Parenthesized, the negation takes the comparison whole in the original too.
	_ = (a < b) == false
}

// strings compares strings and an interface with nil, each a comparison whose leftmost operand no negation takes.
func strings(name string, word string, value any) {
	// The original writes !name == word, which negates a string; the port writes !(name == word).
	_ = name == word != true // port moves: should omit comparison to bool constant, can be simplified to !name == word
	// The original writes !value == nil, which negates an interface; the port writes !(value == nil).
	_ = value == nil == false // port moves: should omit comparison to bool constant, can be simplified to !value == nil
}

// chains compares a chain of comparisons, whose leftmost operand is the first of the chain.
func chains(a int, b int, on bool) {
	// The original writes !a < b == on, which negates an integer; the port writes !(a < b == on).
	_ = a < b == on != true // port moves: should omit comparison to bool constant, can be simplified to !a < b == on
	// A chain whose leftmost operand is a boolean is negated by negating that operand, so the two agree.
	_ = on == (a == b) == false
}

// booleans compares booleans, where negating the leftmost operand negates the comparison, so the two agree.
func booleans(x bool, y bool) {
	_ = x == y != true
	_ = !x == y == false
	_ = x != y == false
}

// generic compares values of a type parameter whose every type is a boolean, which a negation takes, so the two
// agree.
func generic[Boolean ~bool](x Boolean, y Boolean) {
	_ = x == y == false
}
