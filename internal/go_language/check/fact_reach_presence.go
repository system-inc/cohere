package check

import (
	"maps"
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// factReachPresence is what each package's bodies hold that the presence rows read, by package ID, read as the last
// action on the package finishes (checker.Options.BodiesRead), on whichever goroutine finished it, by the run's
// presence reader (Options.FactReachPresence).
type factReachPresence struct {
	reader  func(read *packages.Package) (map[fact_reach.Place]fact_reach.BodyRecord, []string)
	lock    sync.Mutex
	read    map[string]map[fact_reach.Place]fact_reach.BodyRecord
	markers map[string][]string
}

// readBodies reads read's bodies, and its marker interfaces, while its syntax and type information are whole, and
// keeps an entry even where none holds anything, so a package read apart from one never read.
func (presence *factReachPresence) readBodies(read *packages.Package) {
	bodies, markers := presence.reader(read)
	presence.lock.Lock()
	defer presence.lock.Unlock()
	presence.read[read.ID] = bodies
	presence.markers[read.ID] = markers
}

// markersOf are the marker method names read off the package of ID packageId, none for one never read.
func (presence *factReachPresence) markersOf(packageId string) []string {
	if presence == nil {
		return nil
	}
	presence.lock.Lock()
	defer presence.lock.Unlock()
	return presence.markers[packageId]
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
func (presence *factReachPresence) of(packageId string) (map[fact_reach.Place]fact_reach.BodyRecord, bool) {
	if presence == nil {
		return nil, false
	}
	presence.lock.Lock()
	defer presence.lock.Unlock()
	bodies, isRead := presence.read[packageId]
	return bodies, isRead
}
