//go:build !windows

package p

// inner is one int64 everywhere but windows.
type inner struct {
	low int64
}
