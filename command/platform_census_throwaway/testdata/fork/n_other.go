//go:build !linux && !windows

package fork

// N is 3 on every platform but linux and windows.
const N = 3
