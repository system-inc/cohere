//go:build windows

package p

// inner is two int64s on windows.
type inner struct {
	low, high int64
}
