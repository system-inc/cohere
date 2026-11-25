// Package plain is another package's code whose name its path's last element spells, so an alias equal to it is
// redundant. The fixtures import it and the test never analyzes it.
package plain

// Greeting is something for an importing fixture to use, so its import compiles.
const Greeting = "hello"
