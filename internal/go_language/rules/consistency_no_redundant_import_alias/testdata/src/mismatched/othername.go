// Package othername lives in a folder named mismatched, so an alias spelling the folder renames the package rather
// than repeating its name. The fixtures import it and the test never analyzes it.
package othername

// Count is something for an importing fixture to use, so its import compiles.
const Count = 4
