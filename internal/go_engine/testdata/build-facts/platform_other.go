//go:build !windows

package facts

// size and code are every platform's but windows's.
const (
	size = 4
	code = 1
)

// platformFinish returns on every platform but windows.
func platformFinish() { /* Nothing ends the process on these platforms, so the call returns. */ }
