// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation, and the port's walk is the
// original's. Each walks a function's whole syntax, its literals included, and asks the function for each range's
// operand, so the two could disagree where one range sits in syntax several functions walk: literals nested deep, a
// literal in a package variable's initializer, and a method of a generic type.
package differences

// Reported once: three literals deep, walked by four functions, judged by the innermost alone.
func deep(text string) {
	func() {
		func() {
			func() {
				for _, r := range []rune(text) {
					println(r)
				}
			}()
		}()
	}()
}

// Reported once: a literal called in a package variable's initializer.
var counted = func(text string) int {
	count := 0
	for _, r := range []rune(text) {
		count += int(r)
	}
	return count
}("text")

type box[Text ~string] struct{ text Text }

// Reported once: a method of a generic type.
func (value box[Text]) runes() {
	for _, r := range []rune(value.text) {
		println(r)
	}
}

// Reported once each: the same slice made twice, each ranged over once.
func twoConversions(text string) {
	for _, r := range []rune(text) {
		println(r)
	}
	for _, r := range []rune(text) {
		println(r)
	}
}
