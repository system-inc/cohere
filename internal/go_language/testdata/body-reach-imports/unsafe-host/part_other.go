//go:build !windows

package gate

import _ "unsafe"

// Other is the host's here, the unsafe module turned round: its file imports unsafe, so unusedwrite leaves the whole
// package alone on the host, and its windows twin imports nothing.
func Other() int { return 0 }
