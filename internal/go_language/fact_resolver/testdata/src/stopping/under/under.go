// Package under declares two functions that never return, one stopping calls and one it never does, and Checked,
// whose nil-ness rests on Abort's never returning.
package under

// failure is an error whose pointer is never nil where it is made.
type failure struct{}

// Error says what failed.
func (*failure) Error() string {
	return "failure"
}

// Checked returns a failure, never a nil error, when it returns at all: where fail holds it calls Abort, which never
// returns, so the nil return after the call is never reached. Built without Abort's no-return, the nil return is
// reached, and Checked's error may be nil.
func Checked(fail bool) error {
	if fail {
		Abort()
		return nil
	}
	return &failure{}
}

// Abort never returns, and stopping calls it, so stopping's analysis imports its fact.
func Abort() {
	panic("abort")
}

// Unused never returns, and stopping never calls it, so stopping's analysis never reads its fact.
func Unused() {
	panic("unused")
}
