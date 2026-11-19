//go:build !windows

package reach

// platformInteger is every platform's but windows's.
type platformInteger int32

// platformPart adds nothing to Holder but on windows.
type platformPart struct{}

// platformName is every platform's but windows's.
func platformName() string {
	return "other"
}
