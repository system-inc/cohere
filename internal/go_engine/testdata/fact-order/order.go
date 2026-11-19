// Package order holds, for the facts' reach (#5vbs1np, must 3), a cycle of calls that forms in windows's build alone:
// orderA calls orderB, which calls platformHook, which calls orderA back on windows and nothing on any other platform.
// So the host's walk of the package is order-free, and its order bound names nothing, while windows's answer about
// orderA hangs on which member its walk enters first. orderB, the one member the reach lints, holds a record of every
// kind the host answered about the package, each the same in every build, so nothing but the build's own groups can
// take orderA's users out.
package order

// failure is an error whose pointer is never nil.
type failure struct{}

// Error says what failed.
func (*failure) Error() string {
	return "failure"
}

// orderB calls platformHook, then returns a failure, never nil. It is declared before orderA, so a walk of the package
// in the order of its declarations enters it first, and on windows reads orderA while orderB is begun. Its values are
// there for a caller's odd count to ask the odd slice kind of it.
func orderB(values ...any) error {
	platformHook()
	return &failure{}
}

// orderA returns orderB's error, never nil on the host.
func orderA() error {
	return orderB()
}

// OrderUser compares orderA's error with nil, so the host asks orderA's nil-ness, and keeps orderB's.
func OrderUser() bool {
	return orderA() != nil
}

// OrderDiscard discards orderB's error after handing it three values, so the host asks orderB's purity and the odd
// slice mark of its values.
func OrderDiscard() {
	orderB(1, 2, 3)
}

// stillHead and stillTail call each other in every build, and neither names anything a platform declares, so the reach
// lints neither: a cycle whose members all stand is the host's as well as the build's, and names nothing.
func stillHead(depth int) int {
	if depth > 0 {
		return stillTail(depth - 1)
	}
	return 0
}

// stillTail returns what stillHead returns.
func stillTail(depth int) int {
	return stillHead(depth)
}

// StillUser counts down through the standing cycle, and stands.
func StillUser() int {
	return stillHead(3)
}
