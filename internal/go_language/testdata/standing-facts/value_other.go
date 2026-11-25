//go:build !windows

package standing

// value is every platform's but windows's: no error at all.
func value() error {
	return nil
}
