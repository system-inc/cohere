// Package pointed reaches a struct's layout through a pointer that no expression of a body names (#mtfdb1m): a
// selection through a defined pointer type, and a field promoted through an embedded pointer. Every file but the two
// that set padLength is the same in both builds.
package pointed

import (
	"sync/atomic"
	"unsafe"
)

// Pad is padLength bytes: its underlying moves with the architecture and its name does not.
type Pad struct {
	_ [padLength]byte
}

// Counted points at an unnamed struct that holds a Pad by value before its counter.
type Counted *struct {
	pad     Pad
	counter int64
}

// Inner holds a Pad by value before its counter.
type Inner struct {
	pad     Pad
	counter int64
}

// Holder embeds a pointer to an Inner, whose fields it promotes.
type Holder struct {
	*Inner
}

// InnerPointer spells a pointer to an Inner through an alias.
type InnerPointer = *Inner

// AliasHolder embeds InnerPointer, whose Inner's fields it promotes.
type AliasHolder struct {
	InnerPointer
}

// Shared spells through an alias an unnamed struct that embeds a pointer to an Inner.
type Shared = struct {
	*Inner
}

// Wrapper embeds Shared, which promotes Inner's fields through the pointer Shared embeds.
type Wrapper struct {
	Shared
}

// SharedPointer points at Shared, so a selection through it dereferences to the struct the alias spells.
type SharedPointer *Shared

// Steady holds two words, which sit alike in every build.
type Steady struct {
	first  int64
	second int64
}

// Shifted shifts by the counter's offset in the struct Counted points at: 32 on amd64, where vet's shift finds
// nothing, and 64 on arm64, where it finds the shift too wide. No expression of the body has the struct's type.
func Shifted(counted Counted, bits uint64) uint64 {
	return bits << unsafe.Offsetof(counted.counter)
}

// Bump adds to the counter through Counted.
func Bump(counted Counted) {
	atomic.AddInt64(&counted.counter, 1)
}

// BumpHeld adds to the counter Holder promotes from Inner, through the embedded pointer.
func BumpHeld(holder *Holder) {
	atomic.AddInt64(&holder.counter, 1)
}

// BumpAliasHeld adds to the counter AliasHolder promotes through the alias of its embedded pointer.
func BumpAliasHeld(holder *AliasHolder) {
	atomic.AddInt64(&holder.counter, 1)
}

// BumpWrapped adds to the counter Wrapper promotes through the pointer the struct Shared spells embeds.
func BumpWrapped(wrapper *Wrapper) {
	atomic.AddInt64(&wrapper.counter, 1)
}

// BumpShared adds to the counter through SharedPointer, a defined pointer to Shared.
func BumpShared(shared SharedPointer) {
	atomic.AddInt64(&shared.counter, 1)
}

// Offset is the offset of Steady's second word, 8 in every build.
func Offset(steady *Steady) uintptr {
	return unsafe.Offsetof(steady.second)
}
