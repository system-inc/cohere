package check

import (
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

// readBodies reads read's bodies while its syntax and type information are whole.
func (presence *factReachPresence) readBodies(read *packages.Package) {
	bodies := standing_presence.ReadBodies(read.Fset, read.Syntax, read.TypesInfo, read.Types)
	presence.lock.Lock()
	defer presence.lock.Unlock()
	presence.read[read.ID] = bodies
}

// of is what was read of the bodies of the package of ID packageId, nil for none, and nil from no presence at all.
func (presence *factReachPresence) of(packageId string) map[fact_reach.Place]standing_presence.BodyRecord {
	if presence == nil {
		return nil
	}
	presence.lock.Lock()
	defer presence.lock.Unlock()
	return presence.read[packageId]
}
