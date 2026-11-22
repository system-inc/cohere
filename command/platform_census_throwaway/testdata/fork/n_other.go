//go:build !linux && !windows

package fork

// N is 3 on every platform but linux and windows.
const N = 3

// Kill names its second parameter sig elsewhere.
var Kill func(pid int, sig int) error

// Stat's Mode is a uint32 elsewhere.
type Stat struct{ Mode uint32 }
