// Package kept holds, for the facts' reach (#z9fze0t), a callee whose purity no body asks: outer's judgment judges
// inner inside its own walk, which hands the resolver inner's verdict all the same, so the host holds a record of it.
// inner calls platformBase, which each platform's file declares, so the reach lints inner in every build, and inner is
// pure in each, platformBase being pure everywhere. With the record the build compares inner's purity, finds it the
// same, and outer, which names only inner, stands. Without it inner would read unknown, and take outer out.
//
// A body the build lints must hold a record of every kind the build can ask again, so inner holds the other two as
// well: its nil-ness, which outer asks by comparing its error with nil, and odd slice's, which is read by the
// parameter here, pairs' being asked of, and inner has none.
package kept

// inner returns no error whatever platformBase's is: pure in every build, and its error may be nil.
func inner() error {
	if platformBase(3) < 0 {
		return nil
	}
	return nil
}

// outer counts inner's error. It names only inner, whose signature is the same in every build, so the reach leaves it
// unreached, and comparing inner's error with nil has the host ask inner's nil-ness.
func outer() int {
	if inner() != nil {
		return 0
	}
	return 1
}

// Twice discards outer's result, a finding in every build, so the host asks outer's purity, and never inner's.
func Twice() int {
	outer()
	return 1
}

// pairs panics unless its values come in pairs.
func pairs(values []any) {
	if len(values)%2 != 0 {
		panic("odd")
	}
}

// PairsUser hands pairs an odd slice, a finding in every build, so the host asks the mark of pairs' parameter and odd
// slice is read by the parameter in this package.
func PairsUser() {
	pairs([]any{1})
}
