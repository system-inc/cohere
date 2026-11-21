//go:build !windows

package gate

// Other imports nothing here, its windows twin importing the path the gate reads.
func Other() int { return 0 }
