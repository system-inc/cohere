package build_reads

import (
	"go/token"
	"go/types"
	"iter"
)

// What each top-level declaration of a package uses, its signature and its body apart (#kr1m304), so an extra build
// can re-check only the bodies a difference reaches and share the rest from the host's run (#9paaen9). A body is
// reached when it sits in a differing file, uses an object a differing file declares or changes, or resolves a name a
// differing file now declares, as a windows file declaring a package-level `len` changes what `len` means in every
// body of the package. So every use is recorded, the package's own package-level objects and the universe's included,
// and only what the declaration itself declares inside it is left out. A use recorded wider than it is costs a
// re-check; one recorded narrower is a wrong finding, so every doubt goes wide.
//
// The record holds the host's own objects, never handed to another build: what crosses builds is each object's
// identity, rendered on demand (IdentityRenderer).

// DeclarationKind is what a top-level declaration declares.
type DeclarationKind string

const (
	// DeclaredFunction is a function or a method: its signature, and its body when it has one.
	DeclaredFunction DeclarationKind = "Function"

	// DeclaredType is one type of a type declaration: its signature alone.
	DeclaredType DeclarationKind = "Type"

	// DeclaredValue is one spec of a var or const declaration: its type as its signature, and its values as its body.
	DeclaredValue DeclarationKind = "Value"
)

// Declaration is one top-level declaration of a package's files. Objects are what it declares by name, each but a
// blank one; an init function and a declaration of `_` alone have no objectpath and declare none, and are told apart
// by File, the start of the file that declares them, and Ordinal, their place among such in the file from 0. Name is
// the name a function or type is declared by, and `_` for a value spec of blank names alone.
type Declaration struct {
	Kind    DeclarationKind
	Objects []types.Object
	File    token.Pos
	Ordinal int
	Name    string
}

// UsedEntry is one thing a package's parts use, as its record holds it once however many parts use it. A selection is
// a use of Object from Receiver, the named type the selection starts from as its generic declares it, nil when it
// starts from no named type, at Index, the path through embedded fields, each index written in decimal and ended by a
// comma. Any other use is of Object alone, with no Receiver nor Index.
type UsedEntry struct {
	Object   types.Object
	Receiver *types.TypeName
	Index    string
}

// DeclarationUses is what one declaration uses, its signature and its body apart, as a reader sees it (see
// FunctionUses.Declarations). HasBody says it has a body, a function's or a value's initializer, which a type and a
// function declared without one have not. SignatureUnknown and BodyUnknown say the part holds a use whose identity
// renders unknown, or one the recorder could not follow: such a part matches no other build's, so a reader takes it
// as differing and never compares its uses.
type DeclarationUses struct {
	Declaration      Declaration
	Signature        []PartUse
	Body             []PartUse
	HasBody          bool
	SignatureUnknown bool
	BodyUnknown      bool
}

// FunctionUses is one package's record of what its declarations use, kept compact while it is held through the
// builds: every thing used once in a table, objects and selections apart, and every part's uses back to back as
// packed entries and kinds, each declaration holding where its parts lie.
type FunctionUses struct {
	// objects are the objects the parts use other than by a selection, and selections the selections, each once: a
	// PartUse's entry indexes one or the other.
	objects    []types.Object
	selections []usedSelection

	// declarations are each declaration in the order its files declare them, declared every declaration's objects back
	// to back, and uses every part's uses back to back.
	declarations []declarationRecord
	declared     []types.Object
	uses         []PartUse
}

// usedSelection is a selection's entry: Object from Receiver at Index (see UsedEntry).
type usedSelection struct {
	receiver *types.TypeName
	object   types.Object
	index    string
}

// declarationRecord is one declaration as its record holds it: where its objects lie in declared, where its signature's
// and its body's uses lie in uses, the body's from signatureEnd, whether each part is unknown, and the rest of what
// Declaration names it by.
type declarationRecord struct {
	file             token.Pos
	name             string
	objectsStart     uint32
	objectsEnd       uint32
	usesStart        uint32
	signatureEnd     uint32
	bodyEnd          uint32
	ordinal          int32
	kind             declarationKindIndex
	hasBody          bool
	signatureUnknown bool
	bodyUnknown      bool
}

// declarationKindIndex is a DeclarationKind as a record holds it, a byte rather than a string's two words: its index
// in declarationKinds.
type declarationKindIndex uint8

// declarationKinds are the kinds a declarationKindIndex indexes.
var declarationKinds = [...]DeclarationKind{DeclaredFunction, DeclaredType, DeclaredValue}

// The index of each kind in declarationKinds.
const (
	indexOfFunction declarationKindIndex = iota
	indexOfType
	indexOfValue
)

// Declarations yields what each declaration uses, in the order its files declare them, each part a view of the
// record's own uses, which a reader never writes.
func (record *FunctionUses) Declarations() iter.Seq[DeclarationUses] {
	return func(yield func(DeclarationUses) bool) {
		for _, declared := range record.declarations {
			uses := DeclarationUses{
				Declaration: Declaration{Kind: declarationKinds[declared.kind], File: declared.file,
					Ordinal: int(declared.ordinal), Name: declared.name},
				Signature:        record.uses[declared.usesStart:declared.signatureEnd:declared.signatureEnd],
				Body:             record.uses[declared.signatureEnd:declared.bodyEnd:declared.bodyEnd],
				HasBody:          declared.hasBody,
				SignatureUnknown: declared.signatureUnknown,
				BodyUnknown:      declared.bodyUnknown,
			}
			if declared.objectsEnd > declared.objectsStart {
				objects := record.declared[declared.objectsStart:declared.objectsEnd:declared.objectsEnd]
				uses.Declaration.Objects = objects
			}
			if len(uses.Signature) == 0 {
				uses.Signature = nil
			}
			if len(uses.Body) == 0 {
				uses.Body = nil
			}
			if !yield(uses) {
				return
			}
		}
	}
}

// Used is the entry use is of.
func (record *FunctionUses) Used(use PartUse) UsedEntry {
	entry := uint32(use) >> partUseEntryShift
	if use&partUseSelected == 0 {
		return UsedEntry{Object: record.objects[entry]}
	}
	selection := record.selections[entry]
	return UsedEntry{Object: selection.object, Receiver: selection.receiver, Index: selection.index}
}
