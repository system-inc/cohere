//go:build !windows

package reach

// platformInteger is every platform's but windows's.
type platformInteger int32

// platformPart adds nothing to Holder but on windows.
type platformPart struct{}

// platformLimit is every platform's but windows's.
const platformLimit = 8

// platformName is every platform's but windows's.
func platformName() string {
	return "other"
}

// platformSize counts its calls, so it is no pure function.
func platformSize(count int) int {
	calls++
	return count * 8
}
