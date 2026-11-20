// Package initialized holds an init function, which declares no object, so an answer about it finds no declaration
// of the record (#30dtg3p).
package initialized

// started is how many times the package started.
var started int

// count is called by init's body alone.
func count() int { return 1 }

// init counts the package's start.
func init() { started += count() }
