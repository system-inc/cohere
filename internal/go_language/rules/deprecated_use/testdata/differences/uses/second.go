package uses

import "example.com/differences/library"

// Found by both: the original clears the deprecated function its walk was last in, in the file before, at this file's
// start, and the port judges the variable by its own declaration.
var second = library.OldFunction()
