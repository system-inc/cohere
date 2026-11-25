//go:build !windows

package nilchain

// platformError returns no error at all.
func platformError() error {
	return nil
}
