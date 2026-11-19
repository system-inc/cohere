// Package definedfrom holds shared types defined from a platform type's name, `type Extra platformExtra`, whose fields
// and promoted methods are the platform type's, so they move with the build though the declaration reads alike.
package definedfrom

// Base holds a Name, an int, in every build.
type Base struct {
	Name int
}

// Wrap embeds Base, so it promotes Base's Name one level down.
type Wrap struct {
	Base
}

// Extra is defined from platformExtra, so its fields are platformExtra's: none on the host, a Name on linux.
type Extra platformExtra

// Inner embeds Wrap and a pointer to Extra. On the host its Name is Wrap's Base's, an int two levels down; on linux
// Extra's own Name, a string one level down, is shallower and takes its place.
type Inner struct {
	Wrap
	*Extra
}

// Label reads Inner's Name, an int on the host and a string on linux: dependent.
func Label(inner Inner) any { return inner.Name }

// Gadget is a shared type with one method, Spin.
type Gadget struct{}

// Spin is Gadget's only method, which a type embedding Gadget promotes.
func (Gadget) Spin() {}

// Part is defined from platformPart, so its promoted methods are platformPart's: none on the host, Spin on linux.
type Part platformPart

// Holder embeds a pointer to Part, so it promotes what Part promotes: on linux it has Spin, on the host no method.
type Holder struct {
	*Part
}

// Spinner is what Holder implements on linux alone.
type Spinner interface {
	Spin()
}

// Boxed converts a Holder to an interface, which carries its method set: dependent.
func Boxed(holder Holder) any { return holder }

// Steady is defined from Base, which no build changes, so nothing of it moves.
type Steady Base

// Kept embeds a pointer to Steady, and promotes the same Name in every build.
type Kept struct {
	*Steady
}

// Named reads Kept's Name, Steady's own in every build: independent.
func Named(kept Kept) any { return kept.Name }

// Target is an alias of platformExtra, so its bits are platformExtra's.
type Target = platformExtra

// FromAlias is defined from the alias Target, so its fields are platformExtra's as Extra's are.
type FromAlias Target

// AliasInner embeds Wrap and a pointer to FromAlias, so its Name moves on linux as Inner's does.
type AliasInner struct {
	Wrap
	*FromAlias
}

// AliasLabel reads AliasInner's Name, an int on the host and a string on linux: dependent.
func AliasLabel(inner AliasInner) any { return inner.Name }

// ExtraAlias is an alias of Extra, the defined type, so its bits are Extra's.
type ExtraAlias = Extra

// Embeds embeds Wrap and a pointer to ExtraAlias, so it promotes Extra's fields through the alias: its Name moves on
// linux as Inner's does.
type Embeds struct {
	Wrap
	*ExtraAlias
}

// EmbedsLabel reads Embeds' Name, an int on the host and a string on linux: dependent.
func EmbedsLabel(embeds Embeds) any { return embeds.Name }
