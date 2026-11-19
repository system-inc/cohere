// Package standing holds, for the body differential's second leg (#adq5ahd), a chain whose nil-ness differs on windows
// alone through a function each platform's file declares, and one whose nil-ness is the same in every build, each
// read where a call's result is compared with nil.
package standing

// Wrapped returns value's error, which each platform's file declares.
func Wrapped() error {
	return value()
}

// Outer returns Wrapped's error, so its nil-ness is Wrapped's, though nothing of its own differs by build.
func Outer() error {
	return Wrapped()
}

// OuterFailed compares Outer's error with nil.
func OuterFailed() bool {
	return Outer() != nil
}

// stableError is an error that is never nil.
type stableError struct{}

// Error says what failed.
func (stableError) Error() string {
	return "stable"
}

// Stable returns an error that is never nil, in every build.
func Stable() error {
	return stableError{}
}

// StableFailed compares Stable's error with nil.
func StableFailed() bool {
	return Stable() != nil
}
