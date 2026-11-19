package go_engine

import (
	"maps"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// factReachHost is one package's host side in one build (fact_reach.Host): the package's side every build shares,
// and what the build alone decides, the bodies its reach leaves standing and whether it lints the package in part.
// It reads standing and never writes it.
type factReachHost struct {
	*factReachPackage
	standing map[fact_reach.Place]bool
	partial  bool
}

// The compiler holds a build's side to everything a pass reads of the host.
var _ fact_reach.Host = factReachHost{}

// Standing is the bodies the reach leaves to the host in the build, a copy at each call, since its caller takes
// bodies out of it. It is nil and false for a package the build lints whole: one it does not lint in part, and one
// whose side holds anything unplaced, whatever the reach left, since an answer or a read the host's side could not
// place is one no build is asked, and a body resting on it would stand unproven.
func (host factReachHost) Standing() (map[fact_reach.Place]bool, bool) {
	if !host.partial || host.unplaced > 0 {
		return nil, false
	}
	return maps.Clone(host.standing), true
}
