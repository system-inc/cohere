// Package kept holds, for the facts' reach (#z9fze0t), two callees whose purity no body asks, each judged inside its
// caller's judgment by a walk that hands the resolver its verdict all the same, so the host holds a record of each.
// Both call what each platform's file declares, so the reach lints them in every build.
//
// inner is pure in every build, platformBase being pure everywhere: the build compares inner's record, finds it the
// same, and outer, which names only inner, stands. shifting is pure on windows alone, platformShift counting its calls
// everywhere else: its record differs on windows and takes outerShifting out, whose own purity then differs and takes
// TwiceShifting out. Without the records nothing would say the two apart: purity records every judgment, so a
// function with no answer of it is taken as one no answer rests on, and outerShifting would stand on the host's
// findings as outer does.
package kept

// inner doubles platformBase's, pure in every build.
func inner(value int) int {
	return platformBase(value) * 2
}

// outer returns one more than inner's. It names only inner, whose signature is the same in every build, so the reach
// leaves it unreached.
func outer(value int) int {
	return inner(value) + 1
}

// Twice discards outer's result, a finding in every build, so the host asks outer's purity, and never inner's.
func Twice() int {
	outer(2)
	return 1
}

// shifting doubles platformShift's, pure where platformShift is, on windows alone.
func shifting(value int) int {
	return platformShift(value) * 2
}

// outerShifting returns one more than shifting's. It names only shifting, so the reach leaves it unreached.
func outerShifting(value int) int {
	return shifting(value) + 1
}

// TwiceShifting discards outerShifting's result, a finding only where outerShifting is pure, on windows, so the host
// asks outerShifting's purity, and never shifting's.
func TwiceShifting() int {
	outerShifting(2)
	return 1
}
