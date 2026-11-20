package standing_facts

import "github.com/system-inc/cohere/internal/go_engine/fact_reach"

// Result is one package's standing bodies in one build, settled, and what settling them took, for the build's census.
// A nil Result, and the empty one a package with no host's side or one the build settles nothing of has, hold nothing
// standing.
type Result struct {
	// Partial is whether the build settled this package, so that facts were compared at all: false for a package the
	// checker handed no host's side of, and for one the host's side says the build lints whole and asks nothing of. A
	// settled package may leave nothing standing, and may have had nothing standing as the walk started, as one whose
	// variable initializers alone the reach leaves unreached: the build lints all of it, and its settlement says what
	// was named.
	Partial bool

	// Settlement is what the fixed point left standing, took out and asked (fact_reach.Settle), nil when Partial is
	// false. Its Standing is the bodies still standing once the walk ended, only read once run has returned.
	Settlement *fact_reach.Settlement

	// FunctionsCompared is how many functions' no-return facts were compared with the host's, and FunctionsNamed how
	// many of them named their object: the fact differed, or the build has no such function.
	FunctionsCompared int
	FunctionsNamed    int

	// PushedCompared is how many of the functions' other pushed facts, a wrapper's and a deprecation, were compared
	// with the host's, and PushedNamed how many of them named their function: the fact differed.
	PushedCompared int
	PushedNamed    int

	// ImportedAsked is how many answers the host read from other packages were asked of the build before the walk,
	// and ImportedNamed how many of them named their object: the answer differed, or could not be read.
	ImportedAsked int
	ImportedNamed int

	// BuildOrderNamed is how many functions were named as members of a cycle of calls in the build that touches a
	// body the build lints (buildOrderNamed).
	BuildOrderNamed int
}

// Stands is whether the function body declared in the file parsed from filename, its `func` at offset, is left to the
// host's findings in this build. It only reads, so every rule of the package may ask it at once.
func (result *Result) Stands(filename string, offset int) bool {
	return result != nil && result.Settlement != nil &&
		result.Settlement.Standing[fact_reach.Place{File: filename, Offset: offset}]
}
