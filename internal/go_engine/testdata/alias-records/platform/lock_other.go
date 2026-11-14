//go:build !windows

package platform

// Lock holds nothing, on every platform but windows.
type Lock struct{}
