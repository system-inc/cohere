package merged

import (
	"fmt"

	"github.com/system-inc/go-types/types"
)

// Failure is a build's export data the merge couldn't read: the object it was merging, or the package it was
// importing (neither for an interface's type set), and why. The build reads the object as absent, so the check
// reports the build's use of it as it reports any name the build lacks, and an object no build could read is an
// invalid one.
type Failure struct {
	Builds types.Mask
	Path   string
	Name   string // empty when the package itself was being imported
	Cause  error
}

func (failure Failure) Error() string {
	if failure.Path == "" {
		return fmt.Sprintf("builds %08b: %v", failure.Builds, failure.Cause)
	}
	if failure.Name == "" {
		return fmt.Sprintf("builds %08b, importing %s: %v", failure.Builds, failure.Path, failure.Cause)
	}
	return fmt.Sprintf("builds %08b, %s.%s: %v", failure.Builds, failure.Path, failure.Name, failure.Cause)
}

func (failure Failure) Unwrap() error { return failure.Cause }

// failureKey keeps a failure from being noted twice: a build whose package can't be read fails each time an object of
// it is asked for.
type failureKey struct {
	build   int
	path    string
	message string
}
