// Package alone holds, for the facts' reach (#30dtg3p), an init function whose one statement discards shared's
// result. shared is the same in every build and is pure exactly where platformValue is, which each platform's file
// declares, so SA4017 reports init's statement on one platform alone, and init's body must not stand on the other
// with the host's findings.
package alone

// init discards shared's result. It names nothing a platform declares, so the reach leaves it unreached, and only
// shared's purity, which differs, can take it out of the standing set.
func init() {
	shared()
}

// shared is pure where platformValue is.
func shared() int {
	return platformValue(1) + 1
}

// quiet is pure in every build, and names nothing a platform declares.
func quiet(value int) int {
	return value * 2
}

// Twice discards quiet's result, a finding in every build, in a body nothing reaches.
func Twice() int {
	quiet(2)
	return 1
}

// Name calls platformName, which each platform's file declares, so the reach lints it in every build.
func Name() string {
	return platformName()
}
