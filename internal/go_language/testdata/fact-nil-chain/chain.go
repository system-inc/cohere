// Package nilchain holds, for the facts' reach (#z9fze0t), two chains whose nil-ness rests on platformError's, a nil
// error everywhere but windows, where it is a nil *failure, never a nil error. In each, a user compares its top's error
// with nil, a finding on windows alone; the top names only its middle, and the middle only a leaf, which calls
// platformError, so the reach lints the leaf alone. No body asks a leaf's nil-ness or a middle's, which the top's
// judgment judges, so their records are the ones the walks hand the resolver.
//
// The demand walk judges the first chain, and keeps leafShared's verdict and middle's as it judges them. In the second,
// middleReplayed enters cycleHead first, whose cycle with cycleTail leaves the demand walk's answer to the push's
// order, so it throws away what it judged, and the replay judges leafReplayed and middleReplayed and keeps them. The
// cycle ends at the defaults of its signature, so the order bound names neither member.
package nilchain

// failure is an error whose nil pointer is never a nil error.
type failure struct{}

// Error says what failed.
func (*failure) Error() string {
	return "failure"
}

// leafShared returns what each platform declares, after working.
func leafShared() error {
	println("leaf")
	return platformError()
}

// middle names only leafShared.
func middle() error {
	println("middle")
	return leafShared()
}

// top names only middle.
func top() error {
	println("top")
	return middle()
}

// User compares top's error with nil, always true on windows alone.
func User() bool {
	return top() != nil
}

// lastCycle holds what cycleHead last returned.
var lastCycle error

// cycleHead calls cycleTail, which calls it back, and may return nil.
func cycleHead(depth int) error {
	if depth > 0 {
		return cycleTail(depth - 1)
	}
	return nil
}

// cycleTail calls cycleHead back, and may return nil.
func cycleTail(depth int) error {
	if depth > 0 {
		return cycleHead(depth - 1)
	}
	return nil
}

// leafReplayed returns what each platform declares, after working.
func leafReplayed() error {
	println("leaf replayed")
	return platformError()
}

// middleReplayed enters the cycle, then names only leafReplayed.
func middleReplayed() error {
	lastCycle = cycleHead(2)
	return leafReplayed()
}

// topReplayed names only middleReplayed.
func topReplayed() error {
	println("top replayed")
	return middleReplayed()
}

// UserReplayed compares topReplayed's error with nil, always true on windows alone.
func UserReplayed() bool {
	return topReplayed() != nil
}
