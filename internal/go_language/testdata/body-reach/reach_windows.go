package reach

// platformInteger is windows's.
type platformInteger int64

// platformPart gives Holder its Spin on windows.
type platformPart struct{}

// Spin spins on windows.
func (platformPart) Spin() { /* Windows's part spins by being called, as a gadget does. */ }

// platformLimit is windows's.
const platformLimit = 16

// platformName is windows's.
func platformName() string {
	return "windows"
}

// platformSize is pure on windows.
func platformSize(count int) int {
	return count * 8
}

// init calls helper on windows alone.
func init() {
	_ = helper()
}
