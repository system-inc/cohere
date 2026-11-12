package check

import (
	"reflect"
)

// heldReads are a package's persisted reads as a build judges them: the record, and each of its fact types as this
// build's analyzers learn it, by place, nil for a type none of them learns, whose reads the verdict declines.
type heldReads struct {
	record    persistedReads
	factTypes []reflect.Type
}

// newHeldReads is record as a build holds it, its fact types read by learned, every fact type the build's analyzers
// learn by its factTypeName.
func newHeldReads(record persistedReads, learned map[string]reflect.Type) *heldReads {
	held := &heldReads{record: record, factTypes: make([]reflect.Type, len(record.FactTypes))}
	for place, name := range record.FactTypes {
		held.factTypes[place] = learned[name]
	}
	return held
}

// owner is the path of the package reference names, empty for none.
func (held *heldReads) owner(reference int) string {
	if reference == 0 {
		return ""
	}
	return held.record.Owners[reference-1]
}

// object is the object reference names.
func (held *heldReads) object(reference int) persistedObject {
	return held.record.Objects[reference-1]
}

// bareName is the name of the object reference names: its objectpath, unless the record kept a name apart.
func (held *heldReads) bareName(reference int) string {
	stored := held.object(reference)
	if stored.Name != "" {
		return stored.Name
	}
	return string(stored.Path)
}

// objectName is the object reference names as a reason names it, as objectName names the host's: its package's path
// and its name, or its name alone for an object of no package.
func (held *heldReads) objectName(reference int) string {
	stored := held.object(reference)
	if stored.Owner == 0 {
		return held.bareName(reference)
	}
	return held.owner(stored.Owner) + "." + held.bareName(reference)
}

// rendererDeclined is why held's reads are declined as a whole, rendered by another renderer than this binary's, whose
// digests no rendering of this binary can match, or empty when they were not.
func (held *heldReads) rendererDeclined() string {
	if held.record.Renderer == ReadsRendererVersion {
		return ""
	}
	return "unknown: its reads were rendered by " + held.record.Renderer + ", this binary renders " +
		ReadsRendererVersion
}
