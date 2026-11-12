package check

import (
	"fmt"
	"reflect"
)

// heldReads are a package's persisted reads as a build judges them: the record, each of its fact types as this
// build's analyzers learn it, by place, nil for a type none of them learns, whose reads the verdict declines, and why
// the record is unreadable, empty when every reference it holds names something it holds.
type heldReads struct {
	record     persistedReads
	factTypes  []reflect.Type
	unreadable string
}

// newHeldReads is record as a build holds it, its fact types read by learned, every fact type the build's analyzers
// learn by its factTypeName. A record read from the binary's table or a store is checked here, once, so a reference
// past what it holds declines the package as unreadable (declined) rather than reach an accessor, which trusts every
// reference it is handed.
func newHeldReads(record persistedReads, learned map[string]reflect.Type) *heldReads {
	held := &heldReads{record: record, unreadable: unreadableReads(record)}
	if held.unreadable != "" {
		return held
	}
	held.factTypes = make([]reflect.Type, len(record.FactTypes))
	for place, name := range record.FactTypes {
		held.factTypes[place] = learned[name]
	}
	return held
}

// heldReadsOf is the persisted reads contents hold as a build holds them (newHeldReads), or, for contents gob refuses,
// held reads that are unreadable, which a build declines.
func heldReadsOf(contents []byte, learned map[string]reflect.Type) *heldReads {
	record, decodeError := decodePersistedReads(contents)
	if decodeError != nil {
		return &heldReads{unreadable: "unreadable: " + decodeError.Error()}
	}
	return newHeldReads(record, learned)
}

// unreadableReads is why record names something it does not hold, the first such reference, or empty when every
// reference names something it holds: an owner of Owners or an object of Objects by its place counted from one, zero
// for none where a read may have none, and a fact type of FactTypes by its place counted from zero. A reference of each
// kind is checked where it is held: every object's owner, every type read, every fact read's owner, object and type,
// and every lookup's package, answer, subject and candidates.
func unreadableReads(record persistedReads) string {
	owners, objects, factTypes := len(record.Owners), len(record.Objects), len(record.FactTypes)
	// held is how many the record holds of what reference names, which it may name from least to most.
	past := func(what string, reference int, least int, most int, held int) string {
		if reference < least || reference > most {
			return fmt.Sprintf("unreadable: its %s is %d, past the %d it holds", what, reference, held)
		}
		return ""
	}
	var reasons []string
	for _, object := range record.Objects {
		reasons = append(reasons, past("object's owner", object.Owner, 0, owners, owners))
	}
	for _, reference := range record.Types {
		reasons = append(reasons, past("type read", reference, 1, objects, objects))
	}
	for _, fact := range record.Facts {
		reasons = append(reasons, past("fact read's owner", fact.Owner, 1, owners, owners),
			past("fact read's object", fact.Object, 0, objects, objects),
			past("fact read's type", fact.FactType, 0, factTypes-1, factTypes))
	}
	for _, lookup := range record.Lookups {
		reasons = append(reasons, past("lookup's package", lookup.Path, 0, owners, owners),
			past("lookup's answer", lookup.Answer, 0, objects, objects),
			past("lookup's subject", lookup.Subject, 0, objects, objects))
		for _, candidate := range lookup.Candidates {
			reasons = append(reasons, past("lookup's candidate", candidate, 1, objects, objects))
		}
	}
	for _, reason := range reasons {
		if reason != "" {
			return reason
		}
	}
	return ""
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

// declined is why held's reads are declined as a whole before any read is judged, or empty when they are not: a record
// naming something it does not hold (unreadableReads), or one rendered by another renderer than this binary's, whose
// digests no rendering of this binary can match.
func (held *heldReads) declined() string {
	switch {
	case held.unreadable != "":
		return held.unreadable
	case held.record.Renderer != ReadsRendererVersion:
		return "unknown: its reads were rendered by " + held.record.Renderer + ", this binary renders " +
			ReadsRendererVersion
	}
	return ""
}

// typesDeclined is why held's type reads are declined as a whole in build before any is judged, or empty when they are
// not: declined's reasons, and sizes. A digest that reaches another package's unexported field holds its properties
// under the host's sizes (typeGraph.measureBy), so a build measured by other sizes, or by none, which renders every
// field whole, never renders it alike, and is declined as unknown rather than compared: compared, the reads that reach
// no such field would hold and the rest would decline by a reason that only stands for these sizes.
func (held *heldReads) typesDeclined(build *ReadsBuild) string {
	if why := held.declined(); why != "" {
		return why
	}
	wordSize, maximumAlignment, measured := build.measuredSizes()
	if !measured {
		return fmt.Sprintf("unknown: its reads were measured under word size %d, maximum alignment %d, and the "+
			"build is measured by no sizes", held.record.WordSize, held.record.MaximumAlignment)
	}
	if wordSize != held.record.WordSize || maximumAlignment != held.record.MaximumAlignment {
		return fmt.Sprintf("unknown: its reads were measured under word size %d, maximum alignment %d, the build's "+
			"are %d, %d", held.record.WordSize, held.record.MaximumAlignment, wordSize, maximumAlignment)
	}
	return ""
}
