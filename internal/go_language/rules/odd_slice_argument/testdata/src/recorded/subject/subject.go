// Package subject holds a chain of passes for package reader to ask one end of: Back passes its slice to Forward,
// which passes it to a function literal and to below.Pairs, which checks it. One question, of Back's slice, settles
// Forward's and the literal's too, and Loose's is never reached.
package subject

import "recorded/below"

// Back passes its slice on to Forward.
func Back(values []string) {
	Forward(values)
}

// Forward passes its slice to a function literal, called where it is written, and to below.Pairs.
func Forward(values []string) {
	func(inner []string) {
		below.Pairs(inner)
	}(values)
	below.Pairs(values)
}

// Loose takes values of any length, and nothing passes it a parameter.
func Loose(values []string) {
	_ = len(values)
}
