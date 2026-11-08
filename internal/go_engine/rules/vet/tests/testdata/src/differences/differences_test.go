// Package differences holds each finding of the original that is false, which the port does not make, recorded on its
// line with the reason it is false on the line above, the findings the port words differently because of it, and
// the true ones the port alone makes where the original stopped too early.
package differences

import "testing"

func targetIn(directory string) func(*testing.T, string) {
	return func(t *testing.T, value string) {}
}

// The argument of Fuzz is evaluated before Fuzz runs anything, so the *testing.F methods it calls on its way to the
// target are not called by the target, and testing allows them. A call inside a function literal the argument does
// not call on the spot may run in the target, and is still reported.

func FuzzArgumentCalls(f *testing.F) {
	// f.TempDir runs as the argument is evaluated, before Fuzz starts the target.
	f.Fuzz(targetIn(f.TempDir())) // port drops: fuzz target must not call any *F methods
}

func FuzzCalledOnTheSpot(f *testing.F) {
	f.Fuzz(func() func(*testing.T, string) {
		// The literal is called on the spot, so f.TempDir runs before Fuzz starts the target.
		directory := f.TempDir() // port drops: fuzz target must not call any *F methods
		return func(t *testing.T, value string) {
			f.Log(directory)
		}
	}())
}

func FuzzWrapped(f *testing.F) {
	f.Fuzz(wrap(func(t *testing.T, value string) {
		f.Log("may run in the target, so both report it")
	}))
}

func wrap(target func(*testing.T, string)) func(*testing.T, string) {
	return target
}

// Fuzz reads its argument through reflection, so a value of interface type, or of a type parameter's, that holds a
// function is one Fuzz takes.

func FuzzInterfaceTarget(f *testing.F) {
	var target any = func(t *testing.T, value string) {}
	// target holds a function, which Fuzz takes.
	f.Fuzz(target) // port drops: argument to Fuzz must be a function
}

func FuzzTypeParameterTarget[Target func(*testing.T, string)](f *testing.F, target Target) {
	// target's type is a function type, so it holds a function, which Fuzz takes.
	f.Fuzz(target) // port drops: argument to Fuzz must be a function
}

// Fuzz compares the target's first parameter with *testing.T through reflection, which sees the type an alias names.

type testingT = *testing.T

func FuzzAliasedT(f *testing.F) {
	// testingT is *testing.T, so the target takes a *testing.T first.
	f.Fuzz(func(t testingT, value string) {}) // port drops: the first parameter of a fuzz target must be *testing.T
	f.Add("right")
	f.Add(1) // port finds: mismatched type in call to (*testing.F).Add: int, fuzz target expects string
}

// A spread slice hands Add values whose number and types the call does not show.

func FuzzSpread(f *testing.F) {
	seeds := [][]any{{"text", 1}}
	for _, seed := range seeds {
		// seed may hold a string and an int, the two values the target takes.
		f.Add(seed...) // port drops: wrong number of values in call to (*testing.F).Add: 1, fuzz target expects 2
	}
	f.Fuzz(func(t *testing.T, value string, number int) {})
}

func FuzzSpreadOne(f *testing.F) {
	seed := []any{"text"}
	// seed may hold a string, the one value the target takes.
	f.Add(seed...) // port drops: mismatched type in call to (*testing.F).Add: []any, fuzz target expects string
	f.Fuzz(func(t *testing.T, value string) {})
}

// Add records what a value of interface type holds, whose type the call does not show.

func FuzzInterfaceValues(f *testing.F) {
	var value any = "text"
	// value holds a string, the type the target takes.
	f.Add(value) // port drops: mismatched type in call to (*testing.F).Add: any, fuzz target expects string
	f.Fuzz(func(t *testing.T, value string) {})
}

func FuzzInterfaceAmongMismatches(f *testing.F) {
	var value any = "text"
	f.Add(value, 1) // port renames: mismatched types in call to (*testing.F).Add: [any int], fuzz target expects [string int64] => mismatched type in call to (*testing.F).Add: int, fuzz target expects int64
	f.Fuzz(func(t *testing.T, value string, number int64) {})
}

func FuzzTypeParameterValue[Value string](f *testing.F, value Value) {
	// value holds a string, the type the target takes, whatever Value is instantiated with.
	f.Add(value) // port drops: mismatched type in call to (*testing.F).Add: Value, fuzz target expects string
	f.Fuzz(func(t *testing.T, value string) {})
}

// The method expression's first argument is the receiver, not a value Add records.

func FuzzMethodExpression(f *testing.F) {
	// The call hands Add one value, the string the target takes.
	(*testing.F).Add(f, "text") // port drops: wrong number of values in call to (*testing.F).Add: 2, fuzz target expects 1
	(*testing.F).Add(f)         // port renames: mismatched type in call to (*testing.F).Add: *testing.F, fuzz target expects string => wrong number of values in call to (*testing.F).Add: 0, fuzz target expects 1
	(*testing.F).Add(f, 5)      // port renames: wrong number of values in call to (*testing.F).Add: 2, fuzz target expects 1 => mismatched type in call to (*testing.F).Add: int, fuzz target expects string
	f.Fuzz(func(t *testing.T, value string) {})
}
