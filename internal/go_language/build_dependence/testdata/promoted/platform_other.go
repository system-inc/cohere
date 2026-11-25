//go:build !linux

package promoted

// Extra has no field on any platform but linux.
type Extra struct{}

// Second has no field on any platform but linux.
type Second struct{}
