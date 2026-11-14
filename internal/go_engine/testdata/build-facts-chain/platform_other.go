//go:build !windows

package chain

// platformFinish returns on every platform but windows.
func platformFinish() { /* Nothing ends the process on these platforms, so the call returns. */ }
