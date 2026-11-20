package gate

import _ "unsafe"

// Other is windows's, whose file imports unsafe, so unusedwrite leaves the whole package alone.
func Other() int { return 0 }
