// Package odd holds, for the facts' reach (#5vbs1np, round 2's N1), a function whose odd slice record covers one of
// its parameters: G needs its first slice even everywhere, and hands its second to platformPairs, which needs it even
// on windows alone. The host asks the mark of G's first parameter, where GUser hands it an odd slice, and never of its
// second, which F's own mark is judged from inside the kind's state. So G, which the reach lints, holds a record of
// odd slice for one parameter, and only reading the other as unknown takes F, and FUser, out.
package odd

// G checks its first slice even, hands its second to platformPairs, and returns no error.
func G(first []any, second []any) error {
	if len(first)%2 != 0 {
		panic("odd")
	}
	platformPairs(second)
	return nil
}

// GUser hands G an odd first slice, so the host asks the mark of G's first parameter, and discards G's error, so it
// asks G's purity too.
func GUser() {
	G([]any{1}, nil)
}

// GFailed compares G's error with nil, so the host asks G's nil-ness: G holds a record of every kind but for its second
// parameter.
func GFailed() bool {
	return G(nil, nil) != nil
}

// F hands its slice on as G's second, so F's mark is G's second parameter's: needed even on windows alone. It names
// only G, whose signature is the same in every build, so the reach leaves it standing.
func F(values []any) {
	G(nil, values)
}

// FUser hands F an odd slice, a finding wherever F needs its slice even: windows's alone.
func FUser() {
	F([]any{1})
}
