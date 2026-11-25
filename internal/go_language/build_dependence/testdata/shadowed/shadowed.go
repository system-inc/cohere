// Package shadowed holds a name of the universe that one build declares for itself: min is the universe's builtin on
// the host and a function of linux's file on linux, so a shared unit that uses the name means another object there.
package shadowed

// Smaller uses min in its body alone, where its signature names only int: dependent on linux, by the name its body
// uses.
func Smaller(left int, right int) int { return min(left, right) }

// Least takes its value from Smaller, so its bit follows Smaller's: dependent on linux.
var Least = Smaller(1, 2)

// Untouched uses no name a build declares: independent.
func Untouched() int { return 1 }
