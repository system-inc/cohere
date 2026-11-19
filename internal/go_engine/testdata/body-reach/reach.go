// Package reach holds a body for each way windows's difference reaches one of the host's bodies, and two it never
// reaches, for the body reach census (#w72zxtq): platformName, platformInteger and platformPart are each build's own,
// and windows declares a package-level len.
package reach

// Gadget spins in every build.
type Gadget struct{}

// Spin spins, the same in every build, so its body is never reached.
func (Gadget) Spin() { /* A gadget spins by being called; there is nothing more to it. */ }

// Number's underlying type is each build's own, and nothing else of it differs.
type Number platformInteger

// Holder reaches Spin through inner and Gadget on the host, and on windows through platformPart, whose windows file
// declares a Spin of its own.
type Holder struct {
	inner
	platformPart
}

// inner embeds Gadget.
type inner struct{ Gadget }

// holder is the Holder Spinner takes Spin from.
var holder Holder

// settings is of a struct type with no name, whose fields no identity reaches.
var settings struct {
	A int
	B [2]int
}

// Box holds a field of platformPart, each build's own type.
type Box struct{ Part platformPart }

// box is the Box Parted selects its Part from.
var box Box

// PlatformName calls a function only each build's own file declares.
func PlatformName() string {
	return platformName()
}

// Accepted takes a Number, whose underlying type differs on windows, and reads nothing of it: only its signature
// reaches it.
func Accepted(value Number) bool {
	return true
}

// Shifted shifts a Number past an int32's width, which vet's shift check reports where Number is one, and not on
// windows, where it is an int64.
func Shifted(value Number) Number {
	return value >> 40
}

// Positive converts to Number, whose underlying type alone differs on windows.
func Positive(value int) bool {
	return Number(value) > 0
}

// Spinner takes the method value Spin from holder, whose type gains it on windows through platformPart.
func Spinner() func() {
	return holder.Spin
}

// Size reads the universe's len, which windows's file declares at package level.
func Size(values []int) int {
	return len(values)
}

// Joined uses nothing any build changes.
func Joined(first string, second string) string {
	return first + "/" + second
}

// Configured reads settings's field, whose identity is unknown.
func Configured() int {
	return settings.A
}

// High shifts by half a word on a 64-bit build and by all of one on a 32-bit build, which only the sizes reach.
func High(value uint) uint {
	return value >> 32
}

// Paired's signature sizes an array by settings.B, whose identity is unknown.
func Paired(values [cap(settings.B)]int) int {
	return 0
}

// Parted selects box's Part, a field whose own type is each build's own.
func Parted() any {
	return box.Part
}

// Limit reads platformLimit, a constant each build's own file declares.
func Limit() int {
	return platformLimit
}

// Sized counts nothing of its type argument.
func Sized[T any]() int {
	return 0
}

// Typed instantiates Sized with Number, whose underlying type differs on windows.
func Typed() int {
	return Sized[Number]()
}

// Bytes takes a byte, which windows declares at package level in place of the universe's.
func Bytes(value byte) int {
	return 0
}
