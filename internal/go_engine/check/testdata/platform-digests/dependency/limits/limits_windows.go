package limits

// Size is windows'.
const Size = 16

// Open takes a name and a mode on windows.
func Open(name string, mode int) int { return len(name) + mode }

// Halt returns on windows.
func Halt() {}

// Handle holds a uintptr on windows.
type Handle struct{ descriptor uintptr }

// Close is declared alike on every platform, so it reads alike while Handle does not.
func (Handle) Close() error { return nil }
