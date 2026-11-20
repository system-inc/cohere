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
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// Leaving is a body that leaves the standing set by itself, not as a user of an object, and why: its row's reason.
type Leaving struct {
	Place  fact_reach.Place
	Reason fact_reach.Reason
}

// Present is what each row takes out of the standing set of the pass's package: the objects whose users leave, each
// named with its row's reason, and the bodies that leave by themselves, none yet. find is the build's own
// package-level variable or constant for a host's identity, and false where the build has none.
func Present(
	pass *analysis.Pass,
	host fact_reach.Host,
	find func(identity build_reads.Identity) (types.Object, bool),
) ([]fact_reach.Named, []Leaving) {
	return hostPortNamed(pass, host.Used(), find), nil
}
