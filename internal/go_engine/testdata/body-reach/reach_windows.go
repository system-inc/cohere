package reach

// platformInteger is windows's.
type platformInteger int64

// platformPart gives Holder its Spin on windows.
type platformPart struct{}

// Spin spins on windows.
func (platformPart) Spin() { /* Windows's part spins by being called, as a gadget does. */ }

// platformLimit is windows's.
const platformLimit = 16

// byte is windows's own name for the universe's byte.
type byte = uint8

// platformName is windows's.
func platformName() string {
	return "windows"
}

// len counts values on windows in place of the universe's.
func len(values []int) int {
	count := 0
	for range values {
		count++
	}
	return count
}

// platformSize is pure on windows.
func platformSize(count int) int {
	return count * 8
}

// init calls helper on windows alone.
func init() {
	_ = helper()
}
