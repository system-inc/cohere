// Package shadowed_length declares its own len at package scope, which is not the builtin, so a call of it greater
// than zero is negated to less or equal on both sides.
package shadowed_length

func len(text string) int { return 0 }

func lengthPositive(text string) bool {
	if len(text) > 0 {
		return false
	}
	return true
}
