// Package position declares a const block whose second spec repeats the first's iota, and windows declares iota for
// itself in a file of its own, so Second is 1 on the host and 7 on windows though its own span names nothing.
package position

// First and Second are positions, counted by iota.
const (
	First = iota
	Second
)
