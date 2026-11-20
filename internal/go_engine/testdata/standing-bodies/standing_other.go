//go:build !windows

package standing

// platformName is the name of every build but windows's.
func platformName() string {
	return "other"
}
