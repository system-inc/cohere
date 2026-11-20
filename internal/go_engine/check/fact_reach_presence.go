package check

import (
	"maps"
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_presence"
)

// factReachPresence is what each package's bodies hold that the presence rows read, by package ID, read as the last
// action on the package finishes (checker.Options.BodiesRead), on whichever goroutine finished it.
type factReachPresence struct {
	lock sync.Mutex
	read map[string]map[fact_reach.Place]standing_presence.BodyRecord
}

// readBodies reads read's bodies while its syntax and type information are whole, and keeps an entry even where none
// holds anything, so a package read apart from one never read.
func (presence *factReachPresence) readBodies(read *packages.Package) {
	bodies := standing_presence.ReadBodies(read.Fset, read.Syntax, read.TypesInfo, read.Types)
	presence.lock.Lock()
	defer presence.lock.Unlock()
	presence.read[read.ID] = bodies
}

// packages are the IDs of the packages whose bodies were read, sorted, and none from no presence at all.
func (presence *factReachPresence) packages() []string {
	if presence == nil {
		return nil
	}
	presence.lock.Lock()
	defer presence.lock.Unlock()
	return slices.Sorted(maps.Keys(presence.read))
}

// of is what was read of the bodies of the package of ID packageId, and whether they were read at all: a package
// whose bodies hold nothing presence reads was read, with no entry, and one never handed over, as one whose type
// information was gone by its last action, or a run with no presence at all, was not.
func (presence *factReachPresence) of(packageId string) (map[fact_reach.Place]standing_presence.BodyRecord, bool) {
	if presence == nil {
		return nil, false
	}
	presence.lock.Lock()
	defer presence.lock.Unlock()
	bodies, isRead := presence.read[packageId]
	return bodies, isRead
}
