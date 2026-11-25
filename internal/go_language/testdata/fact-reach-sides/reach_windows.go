package reach

// platformInteger is windows's.
type platformInteger int64

// platformPart gives Holder its Spin on windows.
type platformPart struct{}

// Spin spins on windows.
func (platformPart) Spin() { /* Windows's part spins by being called, as a gadget does. */ }

// platformName is windows's.
func platformName() string {
	return "windows"
}
