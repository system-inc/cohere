//go:build !windows

package platform

// Open is an error that is never a nil interface, holding a nil *Failure.
func Open() error {
	var failure *Failure
	return failure
}
