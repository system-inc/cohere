package merged

import (
	"fmt"
	"sync"

	"github.com/system-inc/go-types/types"
)

// build is one build's own universe, unmerged, and how its types read in the merged universe.
type build struct {
	index int
	mask  types.Mask
	// mutex guards source, and every read of the build's types that could read export data.
	mutex  sync.Mutex
	source Source
	// translated is memoized, under the importer's lock; a type parameter maps to the merged one its declaration binds.
	translated map[types.Type]types.Type
}

// declared is a build's object of a name.
type declared struct {
	build  *build
	object types.Object
}

// read runs work, a read of the build's export data or of its types, behind the build's lock, and is its error. A
// panic of the read (export data the reader can't make sense of) is its error too, so a read that fails inside a
// lazy lookup leaves no half-made object behind.
func (build *build) read(work func() error) (failure error) {
	build.mutex.Lock()
	defer build.mutex.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			if recoveredError, isError := recovered.(error); isError {
				failure = fmt.Errorf("reading export data: %w", recoveredError)
				return
			}
			failure = fmt.Errorf("reading export data: %v", recovered)
		}
	}()
	return work()
}

// declared is the build's object of name at path's package level, or nil when the build doesn't declare it. A
// package the build has only reached through another's export data holds the objects that one refers to, so its
// export data is read only when the name isn't among them.
//
// Requires: the build's lock is held.
func (build *build) declared(path string, name string) (types.Object, error) {
	if reachedPackage := build.source.Reached(path); reachedPackage != nil {
		if object := reachedPackage.Scope().Lookup(name); object != nil || reachedPackage.Complete() {
			return object, nil
		}
	}
	importedPackage, importError := build.source.Import(path)
	if importError != nil || importedPackage == nil {
		return nil, importError
	}
	return importedPackage.Scope().Lookup(name), nil
}
