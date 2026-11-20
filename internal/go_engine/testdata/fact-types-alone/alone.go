// Package alone holds, for the facts' reach (#30dtg3p), an init function beside bodies no build's reach lints. The
// package's initializer calls init, so the host asks init's purity, an answer no place can take, since no identity
// names an init. Purity answers not pure for every function with no results, whatever its body, so the answer reads
// the same in every build, and the host's side leaves it out rather than have every build lint the package whole.
package alone

// started is how many times the package started.
var started int

// init counts the package's start. It names nothing a platform declares, so the reach leaves it unreached.
func init() {
	started++
}

// double is pure in every build, and names nothing a platform declares.
func double(value int) int {
	return value * 2
}

// Twice discards double's result, a finding in every build, so the host asks double's purity too, an answer double's
// place takes.
func Twice() int {
	double(2)
	return started
}

// Name calls platformName, which each platform's file declares, so the reach lints it in every build.
func Name() string {
	return platformName()
}
