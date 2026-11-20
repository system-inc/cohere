// Package repeated holds const specs that repeat the spec before them, `const ( A = iota; B )`, whose expressions name
// a name of the universe windows declares for itself in a file of its own, so a repeated spec means another value or
// type there though its own span names nothing.
package repeated

// First takes iota, the universe's on the host and windows' own constant there, so it is dependent on windows by the
// name its span uses; Second repeats First's iota without naming it, and is dependent on windows all the same.
const (
	First = iota
	Second
)

// Small is an int8, the universe's on the host and windows' own type there, so it is dependent on windows by the name
// its span uses; Other repeats Small's int8 without naming it, and is dependent on windows all the same.
const (
	Small int8 = 1
	Other
)

// Steady takes 3, which names nothing, and Kept repeats it: both independent.
const (
	Steady = 3
	Kept
)
