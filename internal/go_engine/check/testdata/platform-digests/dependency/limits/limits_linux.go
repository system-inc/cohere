package limits

// Size is linux's.
const Size = 8

// Open takes a name on linux.
func Open(name string) int { return len(name) }

// Halt never returns on linux, which ctrlflow's noReturn fact says, while its signature is windows' too.
func Halt() { panic("halted") }

// OnlyLinux is declared on linux alone.
func OnlyLinux() {}

// Handle holds an int on linux.
type Handle struct{ descriptor int }

// Close is declared alike on every platform, so it reads alike while Handle does not.
func (Handle) Close() error { return nil }
