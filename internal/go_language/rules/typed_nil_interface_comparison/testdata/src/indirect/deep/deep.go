// Package deep declares a type whose method returns an error that is never a nil interface and may hold a nil pointer.
package deep

// Failure is a concrete error.
type Failure struct{}

func (*Failure) Error() string { return "failure" }

// Core holds a failure, which may be nil.
type Core struct{ failure *Failure }

// Err is the failure as an error: an interface value never nil itself, holding a nil pointer where there is none.
func (core *Core) Err() error { return core.failure }
