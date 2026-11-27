//go:build !linux

package methods

// Speaking only speaks everywhere but linux.
type Speaking interface {
	Speak() string
}
