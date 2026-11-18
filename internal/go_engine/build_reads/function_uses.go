package build_reads

import (
	"go/token"
	"go/types"
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

// PartUse is one object a part of a declaration used, with every kind of use it made of it. A selection is its own
// use, of Object from Receiver, the named type the selection starts from as its generic declares it, nil when it
// starts from no named type, at Index, the path through embedded fields, each index written in decimal and ended by a
// comma. Any other use has no Receiver nor Index.
type PartUse struct {
	Object   types.Object
	Receiver *types.TypeName
	Index    string
	Kinds    UseKinds
}

// DeclarationUses is what one declaration uses, its signature and its body apart. HasBody says it has a body, a
// function's or a value's initializer, which a type and a function declared without one have not.
type DeclarationUses struct {
	Declaration Declaration
	Signature   []PartUse
	Body        []PartUse
	HasBody     bool
}

// partUseKey is what makes two uses of a part one use, whose kinds are joined.
type partUseKey struct {
	object   types.Object
	receiver *types.TypeName
	index    string
}
