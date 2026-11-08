// Package values holds package-level variables a parity file imports twice, once by a dot and once by name, so a
// loop variable assigned by its bare name and appended by its qualified one is the same object.
package values

// Value is a loop's value, assigned through the dot import.
var Value int

// Index is a loop's index, assigned through the dot import.
var Index int
