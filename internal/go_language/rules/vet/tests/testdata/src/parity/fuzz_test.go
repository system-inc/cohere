package parity

import (
	"testing"
)

type text = string

type named string

type target func(*testing.T, string)

type holder struct{}

func (holder) Target(t *testing.T, value string) {}

func (holder) Bad(t *testing.T, value complex64) {}

type wrapper struct {
	*testing.F
}

type fuzzPointer = *testing.F

type fuzzState = testing.F

// A fuzz target fuzzing can run, and calls of Add that match it and do not.

func FuzzAdds(f *testing.F) {
	f.Add("seed", []byte("seed"), int64(1), 'r', byte(2), uint(3), true, 1.5, float32(2), int8(1), int16(1), int32(1))
	f.Add("seed", []byte("seed"), 1, 'r', byte(2), uint(3), true, 1.5, float32(2), int8(1), int16(1), int32(1))
	f.Add(text("seed"), []uint8("seed"), int64(1), rune(1), uint8(2), uint(3), false, 1.5, float32(2), int8(1), int16(1), int32(1))
	f.Add(named("seed"), nil, 1, 1, 2, 3, true, 1, 2, 1, 1, 1)
	f.Add()
	f.Add("one")
	f.Add(nil, []byte("seed"), int64(1), 'r', byte(2), uint(3), true, 1.5, float32(2), int8(1), int16(1), int32(1))
	f.Fuzz(func(t *testing.T, value string, data []byte, number int64, letter rune, small byte, unsigned uint, flag bool, real float64, single float32, tiny int8, short int16, medium int32) {
		f.Add("inside")
	})
	f.Add(Celsius(1), []byte{}, int64(1), 'r', byte(2), uint(3), true, 1.5, float32(2), int8(1), int16(1), int32(1))
	later := func() {
		f.Add(1)
	}
	later()
}

// Calls of Add reached only through the field of an embedded *testing.F, an alias of testing.F, an alias of
// *testing.F, a method value, and a helper.

func FuzzReceivers(f *testing.F) {
	embedded := wrapper{f}
	embedded.Add(1)
	embedded.F.Add(2)
	var state *fuzzState = f
	state.Add(3)
	var pointer fuzzPointer = f
	pointer.Add(4)
	add := f.Add
	add(5)
	seed(f)
	f.Fuzz(func(t *testing.T, value string) {})
}

func seed(f *testing.F) {
	f.Add(6)
}

// Arguments of Fuzz that are not functions.

func FuzzNotFunctions(f *testing.F) {
	f.Fuzz(0)
	f.Fuzz("target")
	f.Fuzz(nil)
	f.Fuzz(holder{})
	f.Add(1)
}

// Targets that return, take nothing, or both, and one that is right after them, whose parameters Add is held to.

func FuzzShapes(f *testing.F) {
	f.Fuzz(func(t *testing.T, value string) error { return nil })
	f.Fuzz(func() {})
	f.Fuzz(func() (int, error) { return 0, nil })
	f.Fuzz(func(t *testing.T, value int) {})
	f.Fuzz(func(t *testing.T, value string) {})
	f.Add("text")
	f.Add(1)
}

// First parameters that are not *testing.T, named and not, in a literal and not.

func FuzzFirstParameters(f *testing.F) {
	f.Fuzz(func(b *testing.B, value string) {})
	f.Fuzz(func(t testing.T, value string) {})
	f.Fuzz(func(*testing.F, string) {})
	f.Fuzz(func(_ *testing.M) {})
	notT := func(value int, other string) {}
	f.Fuzz(notT)
	f.Add(1)
}

// Later parameters fuzzing does not generate: named one by one, named in groups, unnamed, after a wrong first,
// variadic, of a defined type, of a type parameter, and in a target that is not a literal or is a parenthesized one.

func FuzzLaterParameters(f *testing.F) {
	f.Fuzz(func(t *testing.T, value complex64) {})
	f.Fuzz(func(t *testing.T, first, second string, third complex128, fourth, fifth []int, sixth bool) {})
	f.Fuzz(func(*testing.T, complex64, string, []int) {})
	f.Fuzz(func(*testing.B, complex64, string, []int) {})
	f.Fuzz(func(b *testing.B, value complex64, _ string, other map[string]int) {})
	f.Fuzz(func(t *testing.T, values ...string) {})
	f.Fuzz(func(t *testing.T, value named, celsius Celsius) {})
	bad := func(t *testing.T, value complex64, other []string) {}
	f.Fuzz(bad)
	f.Fuzz((func(t *testing.T, value complex64) {}))
	f.Fuzz(holder{}.Bad)
	f.Add(1)
}

func FuzzTypeParameter[T any](f *testing.F) {
	f.Fuzz(func(t *testing.T, value T) {})
}

// Targets that are not literals and are right: a variable, a method value, a value of a defined function type, an
// element, and a call's result.

func FuzzNotLiterals(f *testing.F) {
	variable := func(t *testing.T, value string) {}
	f.Fuzz(variable)
	f.Fuzz(holder{}.Target)
	var defined target = func(t *testing.T, value string) {}
	f.Fuzz(defined)
	targets := []func(*testing.T, string){variable}
	f.Fuzz(targets[0])
	f.Fuzz(made())
	f.Add("text")
	f.Add(1)
}

func made() func(*testing.T, string) {
	return func(t *testing.T, value string) {}
}

// Calls of *testing.F methods inside the target: allowed ones, reported ones, ones nested in closures and goroutines,
// one whose argument is an allowed call, and a call of Fuzz, which is reported and not judged as a Fuzz call.

func FuzzTargetCalls(f *testing.F) {
	f.Name()
	f.Log("outside the target")
	f.Fuzz(func(t *testing.T, value string) {
		f.Name()
		f.Failed()
		f.Log("inside")
		f.Skip()
		f.Helper()
		t.Log(f.Name())
		f.Log(f.Name())
		f.Error(f.Skipped())
		func() {
			f.Cleanup(func() {})
		}()
		go func() {
			f.Fatal("in a goroutine")
		}()
		later := func() {
			f.TempDir()
		}
		later()
		f.Add(1)
		f.Fuzz(0)
		var embedded wrapper
		embedded.Log("not through a *testing.F")
		embedded.F.Log("through one")
	})
}

// Fuzz called in a closure and from a method expression, which the original does not judge as a call of Fuzz but
// walks into, so the target's Fuzz inside it is judged.

func FuzzElsewhere(f *testing.F) {
	run := func() {
		f.Fuzz(func(t *testing.T, value complex64) {})
	}
	run()
	(*testing.F).Fuzz(f, func(t *testing.T, value string) {
		f.Fuzz(func(t *testing.T, number int) { f.Log() })
		f.Add(1)
	})
	f.Add("text")
	f.Add(1)
}

// The first target fuzzing can run decides what Add is held to, even when a later one differs.

func FuzzFirstWins(f *testing.F) {
	f.Fuzz(func(t *testing.T, value int) {})
	f.Fuzz(func(t *testing.T, value string) {})
	f.Add(1)
	f.Add("text")
}

// No target fuzzing can run, so no call of Add is judged.

func FuzzNoTarget(f *testing.F) {
	f.Fuzz(func(t *testing.T, value complex64) {})
	f.Add()
	f.Add(1, 2, 3)
}

// A function named for fuzzing without go test's signature is still read for its calls of Fuzz.

func FuzzHelper(t *testing.T, f *testing.F) {
	f.Fuzz(func(t *testing.T, value complex64) {})
}

// Methods are never judged.

func (holder) FuzzMethod(f *testing.F) {
	f.Fuzz(0)
}

// Calls of Add with types listed, of the package's own named types and generic ones.

func FuzzListedTypes(f *testing.F) {
	f.Add(Celsius(1), List[int]{}, named("x"))
	f.Fuzz(func(t *testing.T, value float64, list string, text string) {})
}
