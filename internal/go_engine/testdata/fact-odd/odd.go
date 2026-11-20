// Package odd holds, for the facts' reach (#5vbs1np, round 2's N1), a function whose odd slice record covers one of
// its parameters: G needs its first slice even everywhere, and hands its second to platformPairs, which needs it even
// on windows alone. The host asks the mark of G's first parameter, where GUser hands it an odd slice, and never of its
// second, which F's own mark is judged from inside the kind's state. The judgment hands the resolver every mark it
// settles (#z9fze0t), so G, which the reach lints, holds a record of odd slice for both parameters, and its second's,
// differing on windows, takes F, and FUser, out. H is G with a second parameter no build needs even: its marks read
// the same on windows, both on record, so E, which hands its slice on as H's second, stands.
package odd

// anything is any by another name, so a parameter of it is no candidate of consistency-no-any, whose presence bound
// would take G and F out before any fact could (#xeafx79), and so is any to every fact this fixture holds.
// platformPairs writes it too in each platform's file, since a candidate there would make every parameter that
// feeds it a key, the bound being closed under the rule's flow.
type anything = any

// G checks its first slice even, hands its second to platformPairs, and returns no error.
func G(first []anything, second []anything) error {
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
func F(values []anything) {
	G(nil, values)
}

// FUser hands F an odd slice, a finding wherever F needs its slice even: windows's alone.
func FUser() {
	F([]any{1})
}

// H checks its first slice even, as G does, hands its second to platformCount, which needs no slice even in any build,
// and returns no error.
func H(first []anything, second []anything) error {
	if len(first)%2 != 0 {
		panic("odd")
	}
	platformCount(second)
	return nil
}

// HUser hands H an odd first slice, so the host asks the mark of H's first parameter, and discards H's error, so it
// asks H's purity too.
func HUser() {
	H([]any{1}, nil)
}

// HFailed compares H's error with nil, so the host asks H's nil-ness: H holds a record of every kind, and of odd slice
// for its second parameter only because the judgment that settled its first handed the second's mark over too.
func HFailed() bool {
	return H(nil, nil) != nil
}

// E hands its slice on as H's second, so E's mark is H's second parameter's: needed even in no build. It names only
// H, whose signature is the same in every build, so the reach leaves it standing.
func E(values []anything) {
	H(nil, values)
}

// EUser hands E an odd slice, a finding in no build, so the host asks E's mark.
func EUser() {
	E([]any{1})
}
