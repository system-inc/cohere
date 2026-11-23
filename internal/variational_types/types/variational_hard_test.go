package types_test

import "testing"

// hardFiles hold what the census fixture doesn't: methods in only some builds and with differing signatures, an
// interface satisfied differently, aliases that differ, iota across builds' files, closures in a forked body, a field
// promoted through a type whose underlying differs, generics over a differing type, and an n:1 variable.
var hardFiles = []fixtureFile{
	{"shared.go", everyBuild, `package fork

import "unsafe"

type Closer interface{ Close() error }

type Handle struct{ h handle }

func (handle Handle) Close() error { return nil }

func UsesFd(handle Handle) uintptr { return handle.Fd() }

func Width() uintptr { return unsafe.Sizeof(Word(0)) }

func Kinds() [3]Kind { return [3]Kind{KindA, KindB, KindC} }

func LastKind() int { return int(KindC) }

func Closure() func() int {
	return func() int {
		inner := func() int { return N + 1 }
		return inner()
	}
}

type Outer struct {
	Inner
	name string
}

func Promoted(outer Outer) uint16 { return uint16(outer.Value) }

func Generic[T any](value T) T { return value }

func UsesGeneric() Word { return Generic(Word(3)) }

type Box[T any] struct{ item T }

func Boxed() Box[Word] { return Box[Word]{item: 1} }

func Unbox(box Box[Word]) Word { return box.item }

var first, second = pair()

func UsesPair() Word { return first + second }

func Each() int {
	total := 0
	for index := range N {
		total += index
	}
	return total
}

func Switch(value any) int {
	switch value.(type) {
	case Word:
		return 1
	}
	return 0
}

func Shift() int { return 1 << N }
`},
	{"unix.go", buildDarwin | buildLinux, `package fork

type handle struct{ fd int }

func (handle Handle) Fd() uintptr { return uintptr(handle.h.fd) }

type Word = uint64

type Kind int

const (
	KindA Kind = iota
	KindB
	KindC
)

const N = 4

type Inner struct{ Value uint32 }

func pair() (Word, Word) { return 1, 2 }
`},
	{"windows.go", buildWindows, `package fork

type handle struct {
	h    uintptr
	name string
}

func (handle Handle) Fd() uintptr { return handle.h.h }

type Word = uint32

type Kind uint8

const (
	_ Kind = iota
	KindA
	KindB
	KindC
)

const N = 2

type Inner struct{ Value uint16 }

func pair() (Word, Word) { return 3, 4 }
`},
}

// TestVariationalCheckProjectsTheHardCases is the projection test over hardFiles.
func TestVariationalCheckProjectsTheHardCases(t *testing.T) {
	// Not parallel: the fork holds a variational check's unit state in package variables, one check at a time.
	forked := projectAndCompare(t, hardFiles)
	if forked["UsesFd"] {
		t.Errorf("UsesFd forked, though Fd's signature is one in every build")
	}
}
