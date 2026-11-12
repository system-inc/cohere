// Package platform declares Open, whose error is never a nil interface but on windows.
package platform

// Failure is the error Open returns.
type Failure struct{}

// Error says what failed.
func (*Failure) Error() string { return "failed" }
