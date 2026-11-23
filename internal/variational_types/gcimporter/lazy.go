// This file is cohere's, not upstream's (#yraxcp9): the reader reads each package-level object the first time it is
// looked up, as go1.27's types2 importer does, rather than every object of a package when the package is imported.
// The fork's InsertLazy holds each name until then. A named type is read whole when it is looked up (its type
// parameters, underlying type and methods), and what it refers to is looked up in turn, so a lookup reads the
// declarations reachable from the object and no more.

package gcimporter

import (
	"time"

	"github.com/system-inc/cohere/internal/variational_types/types"
)

// A Universe is one build's imported packages, by path, and the state of the objects being read on demand in them.
type Universe struct {
	Packages map[string]*types.Package
	// Elapsed is the time spent reading objects on demand, after their packages were imported.
	Elapsed time.Duration

	depth int // how many reads are running, one inside another: only the outermost finishes them

	// declaring are the type names whose declarations are being read. A declaration that refers to a type being
	// declared reads it from here, since looking it up would wait on its own lazy entry.
	declaring map[objectKey]*types.TypeName
	// aliasing are the aliases being read. An alias reached inside its own declaration (through a named type that
	// refers back to it) is read again, as the eager reader did, rather than waited on.
	aliasing map[objectKey]bool

	// later and ifaces are a read's work left for the end of the outermost read, when every named type it reached
	// has its underlying type.
	later  []func()
	ifaces []*types.Interface
}

// objectKey names a package-level object of a universe.
type objectKey struct {
	path string
	name string
}

// NewUniverse is an empty universe.
func NewUniverse() *Universe {
	return &Universe{
		Packages:  map[string]*types.Package{},
		declaring: map[objectKey]*types.TypeName{},
		aliasing:  map[objectKey]bool{},
	}
}

// resolve runs read, one object's lazy read, and finishes the work it left once the outermost read is done.
func (universe *Universe) resolve(read func() types.Object) types.Object {
	if universe.depth > 0 {
		return read()
	}
	universe.depth++
	started := time.Now()
	defer func() {
		universe.depth--
		universe.Elapsed += time.Since(started)
	}()
	object := read()
	universe.finish()
	return object
}

// finish runs the work reads left: type parameters get their constraints, then interfaces are completed. Either can
// look up objects not read yet, whose reads leave work of their own, so both run again until none is left.
func (universe *Universe) finish() {
	for len(universe.later) > 0 || len(universe.ifaces) > 0 {
		for len(universe.later) > 0 {
			later := universe.later
			universe.later = nil
			for _, run := range later {
				run()
			}
		}
		ifaces := universe.ifaces
		universe.ifaces = nil
		for _, iface := range ifaces {
			iface.Complete()
		}
	}
}
