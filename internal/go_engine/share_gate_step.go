package go_engine

// shareGateStep is a step of an extra build at the share gate (shareGate), which a test's shareGateSteps hook is told
// of on the build's own goroutine.
type shareGateStep uint8

const (
	// shareGateWaiting is the build's share reported, and the shares judged when it was the last to arrive, before it
	// waits for its turn.
	shareGateWaiting shareGateStep = iota
	// shareGateHeld is the build looking for its turn once the shares are judged and not let through, told under the
	// gate's lock, so a hook told it must not wait on another build.
	shareGateHeld
	// shareGateAdmitted is the build let through, before its type check, told under the gate's lock before the build
	// after it can pass, so a hook told it must not wait on another build.
	shareGateAdmitted
	// shareGateRefused is the build turned away because a build before it in start order failed, before its type
	// check, which it never runs.
	shareGateRefused
)
