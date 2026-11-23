package merged

import (
	"github.com/system-inc/go-types/types"
)

// wholeReading is what a whole plain importer has read whole so far, under its build's lock: every package and every
// type it walked, so a package imported again, or a type reached again, is walked once.
type wholeReading struct {
	packages map[*types.Package]bool
	walked   map[types.Type]bool
}

// readPackage reads imported whole: every object of its scope, and every type each reaches, through to the
// declaration of every named type and the methods of every instance. A plain universe reads an object the first time
// it is looked up and a named type's declaration the first time it is needed, sharing the universe's state and its
// readers' caches with no lock of their own, so checks that run at once would read export data at once. Read whole
// at its import, under the build's lock, a package leaves its checks only decoded types to read, which are safe to read
// at once as go/types' own are.
//
// Requires: the build's lock is held.
func (whole *wholeReading) readPackage(imported *types.Package) {
	// A package read whole once holds nothing more to read, so an import after a warm-up reads no export data.
	if whole.packages[imported] {
		return
	}
	whole.packages[imported] = true
	walk := typeWalk{firstVisit: whole.firstVisit}
	scope := imported.Scope()
	for _, name := range scope.Names() {
		walk.object(scope.Lookup(name))
	}
}

// firstVisit says whether reachedType is walked for the first time, and marks it walked.
//
// Requires: the build's lock is held.
func (whole *wholeReading) firstVisit(reachedType types.Type) bool {
	if whole.walked[reachedType] {
		return false
	}
	whole.walked[reachedType] = true
	return true
}
