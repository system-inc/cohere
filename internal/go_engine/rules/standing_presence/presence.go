// Package standing_presence keeps present, in an extra build, the standing bodies a package-wide rule reads along one
// narrow edge (#xeafx79, stage 5 of #9paaen9). A body that stands may be stripped from the build's type check, and a
// rule that judges one declaration by reading the bodies that name it, as hostport judges a variable by the dials it
// is passed to, would read none of those bodies once they are gone. So for each such rule, a row, the bodies its edge
// reaches must be present, and a standing body on that edge leaves the standing set: it is linted by the build, as any
// body that leaves is, which changes no finding while nothing is stripped.
//
// A row takes bodies out two ways, before the facts' fixed point runs (fact_reach.Settle): by an object whose users
// leave, named with the row's reason, and by a body that leaves by itself. Every name it reads is the host's
// (fact_reach.Host.Used): the build finds its own object for each identity it can, judges the row's predicate on its
// own declaration of it, and names the host's key, never one it made of its own types, which could miss the host's and
// take no body out. An object the build has no own of is never a candidate.
package standing_presence

import (
	"cmp"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// Leaving is a body that leaves the standing set by itself, not as a user of an object, and why: its row's reason.
type Leaving struct {
	Place  fact_reach.Place
	Reason fact_reach.Reason
}

// Present is what each row takes out of standing, the pass's package's standing set as the walk starts: the objects
// whose users leave, each named with its row's reason, and the bodies that leave by themselves. find is the build's
// own object for a host's identity, and false where the build has none. The named rows come first, then noany's bound,
// then the bodies the host's record says hold what a row reads, and last the edge rows, which read which bodies the
// build holds present once the others have taken theirs out.
func Present(
	pass *analysis.Pass,
	host fact_reach.Host,
	standing map[fact_reach.Place]bool,
	find func(identity build_reads.Identity) (types.Object, bool),
) ([]fact_reach.Named, []Leaving) {
	used := host.Used()
	records := presenceRecords(host)
	var recordedMarkers []string
	for _, record := range records {
		recordedMarkers = append(recordedMarkers, record.LocalMarkers...)
	}
	named := slices.Concat(
		hostPortNamed(pass, used, find),
		scannerErrorNamed(pass, used, find),
		sqlRowsErrorNamed(pass, used, find),
		screamingNamed(pass, used, find),
		emptyFunctionNamed(pass, used, find, recordedMarkers),
	)
	noAnyNamed, leaving := noAnyBound(pass, host, used, find)
	named = append(named, noAnyNamed...)
	leaving = append(leaving, recordedLeaving(records, standing)...)
	leaving = append(leaving, edgesLeaving(syntaxOf(pass), host, records, standing, named, leaving)...)
	return named, leaving
}

// recordedLeaving are the standing bodies that leave for what the host's record says they hold, in the order of their
// places: one that writes a candidate of consistency-no-any's of its own, noany's census reason 6, which the rule
// reads wherever the body is present and no use records when it is spelled interface{}; and one that declares a
// marker interface, which emptyfunction reads as it reads a package-level one.
func recordedLeaving(
	records map[fact_reach.Place]fact_reach.PresenceRecord,
	standing map[fact_reach.Place]bool,
) []Leaving {
	var leaving []Leaving
	for _, place := range slices.SortedFunc(maps.Keys(records), comparePlace) {
		if !standing[place] {
			continue
		}
		switch {
		case records[place].OwnAny:
			leaving = append(leaving, Leaving{Place: place, Reason: NoAny.Reason()})
		case len(records[place].LocalMarkers) > 0:
			leaving = append(leaving, Leaving{Place: place, Reason: EmptyFunction.Reason()})
		}
	}
	return leaving
}

// comparePlace orders two places by file and then by offset.
func comparePlace(first fact_reach.Place, second fact_reach.Place) int {
	return cmp.Or(cmp.Compare(first.File, second.File), cmp.Compare(first.Offset, second.Offset))
}

// packageVariable is the build's own package-level variable of the pass's package that identity names, and false for
// any other object, or none.
func packageVariable(
	pass *analysis.Pass,
	find func(identity build_reads.Identity) (types.Object, bool),
	identity build_reads.Identity,
) (*types.Var, bool) {
	found, isFound := find(identity)
	variable, isVariable := found.(*types.Var)
	if !isFound || !isVariable || variable.Pkg() != pass.Pkg || variable.Parent() != pass.Pkg.Scope() {
		return nil, false
	}
	return variable, true
}
