package build_dependence

import (
	"go/types"
	"sync"

	"golang.org/x/tools/go/types/objectpath"
)

// objectPaths finds the objectpaths a graph's records name objects by (newRecord), with one encoder for each package
// whose objects it is asked of. An encoder indexes a package's API once, the second time an object of it is asked,
// and holds one lock over every search it makes, so one encoder for the whole graph made the records of different
// packages wait on each other's searches, and one for each record searched the packages it read again in every
// record; one for each package does neither.
type objectPaths struct {
	encoders sync.Map
}

// For is object's objectpath, or why it has none, by the encoder of object's package.
func (paths *objectPaths) For(object types.Object) (objectpath.Path, error) {
	stored, isStored := paths.encoders.Load(object.Pkg())
	if !isStored {
		stored, _ = paths.encoders.LoadOrStore(object.Pkg(), &objectpath.Encoder{})
	}
	encoder, isEncoder := stored.(*objectpath.Encoder)
	if !isEncoder {
		// Only this method stores into encoders, and only encoders.
		encoder = &objectpath.Encoder{}
	}
	return encoder.For(object)
}
