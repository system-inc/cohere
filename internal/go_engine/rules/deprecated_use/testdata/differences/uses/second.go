package uses

import "example.com/differences/library"

// Found by both: a file's start clears the deprecated function the walk was last in, in the file before.
var second = library.OldFunction()
