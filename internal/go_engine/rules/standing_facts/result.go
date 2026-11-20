package standing_facts

import (
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_presence"
)

// Result is one package's standing bodies in one build, settled, and what settling them took, for the build's census.
// A nil Result, and the empty one a package with no host's side or one linted whole has, hold nothing standing.
type Result struct {
	// Partial is whether the build lints only part of the package, so that facts were compared at all: false for a
	// package the checker handed no host's side of, and for one the host's side says the build lints whole.
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

	// PresenceNamed is how many objects each presence row named, whose users leave so the row's rule reads them in
	// the build (standing_presence), and PresenceRemoved how many bodies left the standing set for each row: the users
	// the settlement took out with the row's reason, and each body that left by itself. Both are empty, never nil, in
	// a partial result.
	PresenceNamed   map[standing_presence.Row]int
	PresenceRemoved map[standing_presence.Row]int

	// PresenceLeft are the bodies that left the standing set by themselves for a presence row, each with its row's
	// reason, which the settlement, taking out only an object's users, never records.
	PresenceLeft []standing_presence.Leaving
}

// keepLeaving keeps on result each body of leaving that standing holds, once, counting it by its row, and is the
// places it kept, in leaving's order, for the caller to take out of standing.
func (result *Result) keepLeaving(
	standing map[fact_reach.Place]bool,
	leaving []standing_presence.Leaving,
) []fact_reach.Place {
	var kept []fact_reach.Place
	for _, left := range leaving {
		if !standing[left.Place] || slices.Contains(kept, left.Place) {
			continue
		}
		kept = append(kept, left.Place)
		result.PresenceLeft = append(result.PresenceLeft, left)
		if row, isRow := standing_presence.RowOf(left.Reason); isRow {
			result.PresenceRemoved[row]++
		}
	}
	return kept
}

// Stands is whether the function body declared in the file parsed from filename, its `func` at offset, is left to the
// host's findings in this build. It only reads, so every rule of the package may ask it at once.
func (result *Result) Stands(filename string, offset int) bool {
	return result != nil && result.Settlement != nil &&
		result.Settlement.Standing[fact_reach.Place{File: filename, Offset: offset}]
}
