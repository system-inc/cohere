package go_language

import (
	"maps"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// factReachHost is one package's host side in one build (fact_reach.Host): the package's side every build shares,
// and what the build alone decides, the bodies its reach leaves standing and whether it settles the package.
// It reads standing and never writes it.
type factReachHost struct {
	*factReachPackage
	standing map[fact_reach.Place]bool
	partial  bool
}

// The compiler holds a build's side to everything a pass reads of the host.
var _ fact_reach.Host = factReachHost{}

// Standing is the bodies the reach leaves to the host in the build, a copy at each call, since its caller takes
// bodies out of it, but for those placed in a name two parsed files share, whose place may be another body's
// (factReachPackage.shared). It is nil and false for a package the build lints whole and settles nothing of: one with
// no body standing and no variable initializer unreached (factReachSides.inBuild), and one whose side holds anything
// unplaced, whatever the reach left, since an answer or a read the host's side could not place is one no build is
// asked, and a body resting on it would stand unproven. It is empty and true for a package settled with nothing
// standing, as one whose variable initializers alone are unreached: the build lints all of that one too.
func (host factReachHost) Standing() (map[fact_reach.Place]bool, bool) {
	if !host.partial || host.unplaced > 0 {
		return nil, false
	}
	standing := maps.Clone(host.standing)
	maps.DeleteFunc(standing, func(place fact_reach.Place, _ bool) bool { return host.shared[place.File] })
	return standing, true
}
