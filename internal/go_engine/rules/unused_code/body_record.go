package unused_code

import (
	"encoding/json"
	"fmt"
)

// bodyRecord is what one function declaration's body adds to its package's graph, as the walk of that body found it
// (#xeafx79): the nodes it makes, the use and own edges it adds, and what of the steps after the walk falls on its
// nodes. An extra build that leaves the body to the host has no body to walk, so it splices the host's record in
// where the walk would have been. A record therefore holds observations only, each object named by where its
// declaring identifier is written or by its index among the body's locals: no types.Object, no token.Pos and no
// index into the graph, none of which another build's graph shares.
//
// A run holds one record a body of every package it records, so the record is kept small: a place names its file by
// an index into the record's own PlaceFiles, an end of an edge is a few words, the own edges are one list and not a
// list a local, and each list is made at its length once the body is walked. encoded is its canonical form, which
// spells every place and kind out.
type bodyRecord struct {
	// Body is the function declaration the body is of, as a standing body is keyed (rule.StandingBodies.Stands): the
	// file a reader sees its `func` in, past any `//line` directive, and the offset of that `func` in the file as
	// parsed. It is the one place of a record that takes a directive's name. Two bodies of a package can share a key,
	// in two files whose directives name one file, and then neither has a record (bodyRecords).
	Body recordedPlace
	// PlaceFiles is each file a place of the record is in, by the index a place names it with (indexedPlace), in the
	// order the walk first placed something in it. Each name is the one the file set holds, so the record shares its
	// bytes. A place the walk found in no file is in the empty name's, as an unplaced place always was.
	PlaceFiles []string
	// Locals is every node the body's walk makes, in the order the walk met them. A local is its index here and never
	// a position: a type switch makes one variable a clause, all at one offset, and an unnamed parameter of a function
	// literal has no declaring identifier.
	Locals []recordedLocal
	// Owned is each node that owns a local, in the order the walk found them. `var a, b struct{ x int }` reads the type
	// once a name, so x has two owners, and a local the walk only ever uses, a blank result of a function literal, has
	// none. Nearly every local has one, so the edges are one list and not a list a local.
	Owned []recordedOwner
	// Uses is every use edge the walk adds while the body is open, each once, in the order the walk added them. An
	// edge another body adds too is in both records, though the graph holds it once. Either end may be an object
	// declared outside the body: a struct to struct conversion makes the fields of the two structs use each other,
	// a package-level struct's or those of an anonymous struct in a signature.
	Uses []recordedUse
	// Rooted is each local the root uses by a step after the walk, a generated file's nodes and the objects a
	// directive ignores, in the order those steps met them. Both steps read one file's bytes, which a standing body's
	// file shares with the host's.
	Rooted []uint32
	// Files is each file an object declared outside the body is declared in, where the body uses it or it uses a
	// local, and each file whose name the walk read to judge a write to a package-level variable, whether or not the
	// write became a use. Sorted, each once. A build that does not compile one of them as the host did cannot take
	// the record.
	Files []string
	// Present is why the body cannot be summarized by a record at all, when any of it is set.
	Present recordedPresence
}

// recordedPlace is a place in the package's source: a file and an offset in that file as parsed. But for a record's
// own key the file is named as it was parsed, never as a `//line` directive names it, so a place is found in a
// build's own files by name and offset alone.
type recordedPlace struct {
	File   string `json:"file"`
	Offset int    `json:"offset"`
}

// indexedPlace is a recordedPlace inside a record, its file named by its index among the record's PlaceFiles: two
// words where a file's name is three. A file larger than its offset can count is placed nowhere (indexedPlaceOf).
type indexedPlace struct {
	File   uint32
	Offset uint32
}

// recordedLocal is one node a body's walk makes: what a finding says of it. What owns it is in the record's Owned.
type recordedLocal struct {
	// Place is where the local is declared. Several locals may share one, and a local without a declaring identifier
	// is placed where go/types places its object.
	Place indexedPlace
	// Name is the local's name as a finding words it (objectOf): a method of an interface under a local named type
	// reads `L.m`.
	Name string
	// Kind is the kind a finding names the local's kind with (kindOf).
	Kind objectKind
}

// recordedOwner is one own edge onto a local: Owner owns the local at Local, its index among the record's locals.
type recordedOwner struct {
	Local uint32
	Owner recordedEnd
}

// recordedUse is one use edge: By uses Used.
type recordedUse struct {
	By   recordedEnd
	Used recordedEnd
}

// recordedEnd is one end of a recorded edge, which Kind tells apart: the body's function, a local by Local, its index
// among the record's locals, an object declared outside the body by Place, where its declaring identifier is, or the
// root. Local and Place are zero for the kinds that do not read them.
type recordedEnd struct {
	Kind  endKind
	Local uint32
	Place indexedPlace
}

// endKind is which of the four things an end of a recorded edge is.
type endKind uint8

const (
	// endFunction is the function the body is of.
	endFunction endKind = iota
	// endLocal is a local of the body.
	endLocal
	// endDeclared is an object declared outside the body: at package level, a field or a method of a package-level
	// type, a name of the function's own signature, or a field of an anonymous struct any signature writes.
	endDeclared
	// endRoot is the graph's root.
	endRoot
)

// endWords is the word the canonical form names each kind of end with, by the kind.
var endWords = [...]string{
	endFunction: "function",
	endLocal:    "local",
	endDeclared: "declared",
	endRoot:     "root",
}

// recordedPresence is the six reasons a body cannot be summarized by its record. Each reads the build's own types
// past what a record of positions can replay, in a step after the walk or in the walk itself, so a build keeps such
// a body present and walks it itself.
type recordedPresence struct {
	// UnexportedInterfaceMethod is whether the body holds an interface type literal whose method set, with what it
	// embeds, holds an unexported method. Any named type of the package may come to implement it, and use the methods
	// that do. An interface whose methods are all exported adds no use a type's own exported methods have not.
	UnexportedInterfaceMethod bool `json:"unexportedInterfaceMethod"`
	// LocalMethodSet is whether the body declares a named type whose method set, its own or its pointer's, is not
	// empty. It is read from go/types and not from the syntax: `type L T` and `type L I` take methods with no
	// embedded field in the local declaration.
	LocalMethodSet bool `json:"localMethodSet"`
	// InterfaceInstance is whether the body writes an instantiation of a generic interface type of the package,
	// which entry reads from the package's instances whole. The generic interface does not always stand in for its
	// instance: with `holder[T any, P interface{ *T }]` implements binds P and then finds it outside a constraint that
	// names an unbound T, so only the instance holder[int, *int] makes a type's method used.
	InterfaceInstance bool `json:"interfaceInstance"`
	// IgnoredType is whether a directive ignores a type name the body declares, one on its line or one on its whole
	// file. useIgnored then uses that type's methods and fields from go/types, which for a local alias of a
	// package-level type are declared outside the body.
	IgnoredType bool `json:"ignoredType"`
	// AllFieldsUsed is whether the body's walk used every field of a type, and of the structs and arrays it holds
	// (useAllFieldsRecursively), as it does for a conversion to or from unsafe.Pointer and for a local struct with a
	// structs.HostLayout field. Through a named pointer type, `type P *T` and `unsafe.Pointer(p)`, that reads fields
	// of a type nothing in the body names.
	AllFieldsUsed bool `json:"allFieldsUsed"`
	// UnplacedEnd is whether an end of one of the body's edges is an object declared outside the body that its place
	// does not find: a build resolves such an end by asking its own definitions for the object declared at that file
	// and offset, and the host's definitions, asked the same, hand back another object or none. go/types checks
	// the type of `var a, b struct{ x int }` once a variable and so makes x twice at one offset, keeping one for the
	// identifier, and a selection of the other's x would resolve to the wrong field. It is set too for a local of the
	// body declared in a file whose name another file of the package was parsed under, which a build cannot place,
	// and for what no record has a place for at all, which no package's syntax should hold but which keeps the body
	// present rather than stop the host's run: the body, a local or a written variable in no file of the file set, or
	// placed past what an offset can count, and an object the body's walk sees, an own edge onto it, that the body
	// does not declare.
	UnplacedEnd bool `json:"unplacedEnd"`
}

// placeOf is place with its file named, and an error for a place whose file is past the record's files, which no
// recorder writes.
func (record bodyRecord) placeOf(place indexedPlace) (recordedPlace, error) {
	if uint64(place.File) >= uint64(len(record.PlaceFiles)) {
		return recordedPlace{}, fmt.Errorf("a place in file %d of the record's %d", place.File, len(record.PlaceFiles))
	}
	return recordedPlace{File: record.PlaceFiles[place.File], Offset: int(place.Offset)}, nil
}

// encodedRecord is a record in its canonical form, every place with its file named, every kind as its word, and each
// local with its owners, so the form reads alone and does not change with how a run holds a record.
type encodedRecord struct {
	Body    recordedPlace    `json:"body"`
	Locals  []encodedLocal   `json:"locals"`
	Uses    []encodedUse     `json:"uses"`
	Rooted  []uint32         `json:"rooted"`
	Files   []string         `json:"files"`
	Present recordedPresence `json:"present"`
}

// encodedLocal is a local in the canonical form.
type encodedLocal struct {
	Place  recordedPlace `json:"place"`
	Kind   string        `json:"kind"`
	Name   string        `json:"name"`
	Owners []encodedEnd  `json:"owners"`
}

// encodedUse is a use in the canonical form.
type encodedUse struct {
	By   encodedEnd `json:"by"`
	Used encodedEnd `json:"used"`
}

// encodedEnd is an end in the canonical form, its place zero but for an object declared outside the body.
type encodedEnd struct {
	Kind  string        `json:"kind"`
	Local uint32        `json:"local"`
	Place recordedPlace `json:"place"`
}

// canonical is record in its canonical form. Every list is made empty and never nil, so a record with none of
// something encodes as it does on any run. It fails for what no recorder writes: a place past the record's files, a
// kind no word names, or an owner of a local past the record's locals.
func (record bodyRecord) canonical() (encodedRecord, error) {
	canonical := encodedRecord{
		Body:    record.Body,
		Locals:  make([]encodedLocal, 0, len(record.Locals)),
		Uses:    make([]encodedUse, 0, len(record.Uses)),
		Rooted:  append(make([]uint32, 0, len(record.Rooted)), record.Rooted...),
		Files:   append(make([]string, 0, len(record.Files)), record.Files...),
		Present: record.Present,
	}
	for index, local := range record.Locals {
		place, placeError := record.placeOf(local.Place)
		if placeError != nil {
			return encodedRecord{}, fmt.Errorf("local %d: %w", index, placeError)
		}
		if uint64(local.Kind) >= uint64(len(kindWords)) {
			return encodedRecord{}, fmt.Errorf("local %d is of kind %d, which no word names", index, local.Kind)
		}
		canonical.Locals = append(canonical.Locals, encodedLocal{
			Place:  place,
			Kind:   local.Kind.word(),
			Name:   local.Name,
			Owners: []encodedEnd{},
		})
	}
	for _, owned := range record.Owned {
		if uint64(owned.Local) >= uint64(len(canonical.Locals)) {
			return encodedRecord{}, fmt.Errorf("an owner of local %d of its %d", owned.Local, len(canonical.Locals))
		}
		owner, ownerError := record.canonicalEnd(owned.Owner)
		if ownerError != nil {
			return encodedRecord{}, ownerError
		}
		canonical.Locals[owned.Local].Owners = append(canonical.Locals[owned.Local].Owners, owner)
	}
	for _, use := range record.Uses {
		by, byError := record.canonicalEnd(use.By)
		if byError != nil {
			return encodedRecord{}, byError
		}
		used, usedError := record.canonicalEnd(use.Used)
		if usedError != nil {
			return encodedRecord{}, usedError
		}
		canonical.Uses = append(canonical.Uses, encodedUse{By: by, Used: used})
	}
	return canonical, nil
}

// canonicalEnd is end in the canonical form.
func (record bodyRecord) canonicalEnd(end recordedEnd) (encodedEnd, error) {
	if uint64(end.Kind) >= uint64(len(endWords)) {
		return encodedEnd{}, fmt.Errorf("an end of kind %d", end.Kind)
	}
	canonical := encodedEnd{Kind: endWords[end.Kind], Local: end.Local}
	if end.Kind == endDeclared {
		place, placeError := record.placeOf(end.Place)
		if placeError != nil {
			return encodedEnd{}, placeError
		}
		canonical.Place = place
	}
	return canonical, nil
}

// encoded is record in its canonical form, as JSON: every field in its declared order and no map among them, so one
// record has one encoding and two records of one body compare byte for byte.
func (record bodyRecord) encoded() ([]byte, error) {
	canonical, canonicalError := record.canonical()
	if canonicalError != nil {
		return nil, fmt.Errorf("the record of the body at %s: %w", placeText(record.Body), canonicalError)
	}
	return json.Marshal(canonical)
}
