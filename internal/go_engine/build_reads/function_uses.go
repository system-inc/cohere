package build_reads

import (
	"go/token"
	"iter"
	"sync"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/types/objectpath"
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
//
// Whether a use's identity renders unknown, which marks its part unknown, is judged when the record is first read
// (FunctionUses.judge), never as it is recorded: the judgment asks an objectpath of every field used, which indexes
// each package a field is of, and a run records every package it checks while its builds read the records of only
// those they check from source, about a hundred of cohere's 1,330 (#kr1m304).

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
// the name a function or type is declared by, and `_` for a value spec of blank names alone. Offset is where it starts
// in its file, a function at its `func`, a spec at its first name, as the dependence graph places a unit, so a reader
// can ask that graph about the same declaration (build_dependence.Dependence.UnitDependent, #w72zxtq).
type Declaration struct {
	Kind    DeclarationKind
	Objects []types.Object
	File    token.Pos
	Offset  int
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
//
// Several builds read one package's record at once, so what is judged at the first read is judged under judging, a
// `sync.Once`: the judgment is the whole package's, written once and only read from then on, so every reader waits
// for the one judgment and none holds a lock while it reads what was judged.
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

	// judging runs the record's judgment once, at its first read (judge), which makes the rest, nil until then:
	// objectsUnknown and selectionsUnknown are, by entry, whether its identity renders unknown, its object's or, for a
	// selection, its receiver's too, and signaturesUnknown and bodiesUnknown, by declaration, whether the part is
	// unknown, holding a use of such an entry or one the recorder could not follow.
	judging           sync.Once
	objectsUnknown    []bool
	selectionsUnknown []bool
	signaturesUnknown []bool
	bodiesUnknown     []bool
}

// usedSelection is a selection's entry: Object from Receiver at Index (see UsedEntry).
type usedSelection struct {
	receiver *types.TypeName
	object   types.Object
	index    string
}

// declarationRecord is one declaration as its record holds it: where its objects lie in declared, where its signature's
// and its body's uses lie in uses, the body's from signatureEnd, whether each part holds a use the recorder could not
// follow (functionUsesRecorder.selectionAt), which marks it unknown whatever its uses render, and the rest of what
// Declaration names it by, its offset in its file as start, which fits the padding the record had beside its ordinal.
type declarationRecord struct {
	file                token.Pos
	name                string
	start               uint32
	objectsStart        uint32
	objectsEnd          uint32
	usesStart           uint32
	signatureEnd        uint32
	bodyEnd             uint32
	ordinal             int32
	kind                declarationKindIndex
	hasBody             bool
	signatureUnfollowed bool
	bodyUnfollowed      bool
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
// record's own uses, which a reader never writes. It is the one way to a part's uses, and judges the record before it
// yields any (judge), so no reader holds a use whose part's flag is not yet told.
func (record *FunctionUses) Declarations() iter.Seq[DeclarationUses] {
	record.judging.Do(record.judge)
	return func(yield func(DeclarationUses) bool) {
		for index, declared := range record.declarations {
			uses := DeclarationUses{
				Declaration: Declaration{Kind: declarationKinds[declared.kind], File: declared.file,
					Offset: int(declared.start), Ordinal: int(declared.ordinal), Name: declared.name},
				Signature:        record.uses[declared.usesStart:declared.signatureEnd:declared.signatureEnd],
				Body:             record.uses[declared.signatureEnd:declared.bodyEnd:declared.bodyEnd],
				HasBody:          declared.hasBody,
				SignatureUnknown: record.signaturesUnknown[index],
				BodyUnknown:      record.bodiesUnknown[index],
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

// judge judges the record, once, under judging: whether each entry's identity renders unknown, and from those whether
// each declaration's parts are unknown, a part the recorder could not follow a use of unknown whatever its uses
// render. A const spec that repeats another holds that one's parts, so reads that one's answer. The encoder lives for
// the judgment alone, the index it keeps of each package's objectpaths let go once every entry is judged.
func (record *FunctionUses) judge() {
	var encoder objectpath.Encoder
	objectsUnknown := make([]bool, len(record.objects))
	for entry, object := range record.objects {
		objectsUnknown[entry] = rendersUnknown(&encoder, object)
	}
	selectionsUnknown := make([]bool, len(record.selections))
	for entry, selection := range record.selections {
		selectionsUnknown[entry] = rendersUnknown(&encoder, selection.object) ||
			selection.receiver != nil && rendersUnknown(&encoder, selection.receiver)
	}
	signaturesUnknown := make([]bool, len(record.declarations))
	bodiesUnknown := make([]bool, len(record.declarations))
	for index, declared := range record.declarations {
		signaturesUnknown[index] = declared.signatureUnfollowed ||
			holdsUnknown(record.uses[declared.usesStart:declared.signatureEnd], objectsUnknown, selectionsUnknown)
		bodiesUnknown[index] = declared.bodyUnfollowed ||
			holdsUnknown(record.uses[declared.signatureEnd:declared.bodyEnd], objectsUnknown, selectionsUnknown)
	}
	// The record takes the judgment only once it is whole: judging never runs judge again, even after a panic, so a
	// judgment cut short leaves every slice nil, which a later reader fails on, rather than a part of one, whose
	// missing answers it would read as known.
	record.objectsUnknown, record.selectionsUnknown = objectsUnknown, selectionsUnknown
	record.signaturesUnknown, record.bodiesUnknown = signaturesUnknown, bodiesUnknown
}

// holdsUnknown reports whether any of uses, one part's, is of an entry judged unknown, by objectsUnknown and
// selectionsUnknown (FunctionUses.judge).
func holdsUnknown(uses []PartUse, objectsUnknown []bool, selectionsUnknown []bool) bool {
	for _, use := range uses {
		entry := uint32(use) >> partUseEntryShift
		if use&partUseSelected == 0 && objectsUnknown[entry] ||
			use&partUseSelected != 0 && selectionsUnknown[entry] {
			return true
		}
	}
	return false
}

// rendersUnknown reports whether object's identity renders unknown, so a part that uses it can never be matched to
// another build's. It asks encoder for a field alone, or an object no package's scope holds: a package-level object,
// a method of a named type, a universe object and an import's name are each named by what they hold already
// (unplacedIdentity).
func rendersUnknown(encoder *objectpath.Encoder, object types.Object) bool {
	return unplacedIdentity(encoder, object).Kind == IdentityUnknown
}
