package printf

import "go/types"

// calleeName is a called print function's full name, as fullName gives it, formatted only when a message is: every
// message reads it with %s, which calls String. Formatting it for every call of a print function, though most calls
// report nothing, was a share of checking each call a profile could see.
type calleeName struct {
	callee types.Object
}

// String is the callee's full name.
func (name calleeName) String() string {
	return fullName(name.callee)
}
