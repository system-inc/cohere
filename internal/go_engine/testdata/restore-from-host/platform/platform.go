// Package platform is what each build of the fixture declares alike and its own way, as os is: Version is the same in
// every build, and Shared's Size, Open's nil result and Logf's forwarding of its format are each a target's own.
package platform

// Version is the same in every build.
var Version = "1"

// Shared is one type whose method differs by build.
type Shared struct{ Value int }

// Failure is the error Open returns.
type Failure struct{}

// Error says what failed.
func (*Failure) Error() string { return "failed" }
