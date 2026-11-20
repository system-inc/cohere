// Package lower holds the callees an importer's nilness rests on, for the stub differential across a package boundary
// (check's TestNoAnswerOfARecordingKindRestsOnABodyWithNoAnswerAcrossAPackage): Middle's error is never nil, by way of
// inner, and Other is a function no question reaches.
package lower

// failure is an error whose pointer is never nil where it is made.
type failure struct{}

// Error says what failed.
func (*failure) Error() string {
	return "failure"
}

// inner returns a failure, never a nil error.
func inner() error {
	return &failure{}
}

// Middle returns inner's error, never nil as inner's is.
func Middle() error {
	return inner()
}

// Other returns no error, and nothing asks of it.
func Other() error {
	return nil
}
