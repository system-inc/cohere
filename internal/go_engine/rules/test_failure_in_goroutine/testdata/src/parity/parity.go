// Package parity holds the shapes SA2002 depends on, each with a comment saying whether the two are expected to find
// it: what the go statement starts, a literal, a named function, a closure held in a variable, a method, a method
// value, an interface's method, and what the started function calls, each stopping method of T, B and F, the other
// methods of testing's common type, and calls through interfaces, defers, nested goroutines and helpers.
package parity

import "testing"

// Found by both: each stopping method, one finding per call, each with its call as related entry. Each is in a case
// of its own, since a call that never returns ends its block (see sequentialStops).
func everyStoppingMethod(t *testing.T, kind int) {
	go func() {
		switch kind {
		case 0:
			t.FailNow()
		case 1:
			t.Fatal("failed")
		case 2:
			t.Fatalf("failed %d", 1)
		case 3:
			t.SkipNow()
		case 4:
			t.Skip("skipped")
		default:
			t.Skipf("skipped %d", 1)
		}
	}()
}

// Found by both, once: staticcheck knows the stopping methods never return, so it builds nothing of the goroutine
// after the first, and the second is never read.
func sequentialStops(t *testing.T) {
	go func() {
		t.Fatal("first")
		t.SkipNow()
	}()
}

// Found by neither: methods that fail or log without stopping the goroutine.
func nonStoppingMethods(t *testing.T) {
	go func() {
		t.Error("failed")
		t.Errorf("failed %d", 1)
		t.Fail()
		t.Log("logged")
		t.Helper()
	}()
}

// Found by both: a benchmark's B shares testing's common type.
func benchmark(b *testing.B) {
	go func() {
		b.Fatal("failed")
	}()
}

// Found by both: a fuzz test's F shares testing's common type.
func fuzz(f *testing.F) {
	go func() {
		f.SkipNow()
	}()
}

// Found by neither: a call through testing.TB is a call through an interface.
func throughInterface(tb testing.TB) {
	go func() {
		tb.Fatal("failed")
	}()
}

// Found by neither: the goroutine calls the method itself, whose body, in package testing, staticcheck does not
// build.
func directStoppingCall(t *testing.T) {
	go t.FailNow()
}

// Found by both: a named function started with the test.
func named(t *testing.T) {
	go fails(t)
}

func fails(t *testing.T) {
	if t != nil {
		t.Fatalf("failed %d", 2)
	}
}

// Found by neither: the started function calls a helper that stops the test, one level deeper than either reads.
func throughHelper(t *testing.T) {
	go func() {
		fails(t)
	}()
}

// Found by both: a closure held in a variable.
func closureInVariable(t *testing.T) {
	stop := func() {
		t.Skip("skipped")
	}
	go stop()
}

// Found by neither: the closure is called in place, in the test's goroutine.
func calledInPlace(t *testing.T) {
	func() {
		t.Fatal("failed")
	}()
}

// Found by neither: a deferred call is no call instruction of the goroutine.
func deferredInGoroutine(t *testing.T) {
	go func() {
		defer t.FailNow()
	}()
}

// Found by neither: a goroutine started inside the goroutine starts the method itself.
func nestedGoStatement(t *testing.T) {
	go func() {
		go t.FailNow()
	}()
}

// Found by both, at the inner go statement alone: the outer goroutine starts a literal that starts another.
func nestedLiteral(t *testing.T) {
	go func() {
		go func() {
			t.Fatal("failed")
		}()
	}()
}

// Found by neither: the stopping call is in a literal the goroutine calls, not in the goroutine's own body.
func literalInGoroutine(t *testing.T) {
	go func() {
		func() {
			t.Fatal("failed")
		}()
	}()
}

// Found by both: the call in a branch and a loop of the goroutine.
func branchesAndLoops(t *testing.T, rows int) {
	go func() {
		for row := 0; row < rows; row++ {
			if row > 2 {
				t.Fatalf("row %d", row)
			}
		}
	}()
}

type suite struct {
	*testing.T
	name string
}

// Found by both: a method of a type embedding T reaches testing's common type.
func (value *suite) embedded() {
	go func() {
		value.Fatal(value.name)
	}()
}

// Found by both: a method started as the goroutine.
func (value *suite) startsMethod() {
	go value.stops()
}

func (value *suite) stops() {
	value.T.SkipNow()
}

// Found by both, without a related entry: a method value is a closure of a wrapper staticcheck builds, whose body
// calls the method by a call with no syntax, so staticcheck's report leaves the related entry out.
func methodValue(t *testing.T) {
	stop := t.FailNow
	go stop()
}

type stopper interface {
	stops()
}

// Found by neither: a method started through an interface.
func interfaceMethod(value stopper) {
	go value.stops()
}

type ownTest struct{ failed bool }

func (test *ownTest) Fatal(arguments ...any) { test.failed = true }
func (test *ownTest) FailNow()               { test.failed = true }

// Found by neither: methods named as testing's on another type.
func ownMethods(test *ownTest) {
	go func() {
		test.Fatal("failed")
		test.FailNow()
	}()
}

// Found by neither: a subtest's function runs in its own goroutine, which testing starts.
func subtest(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		t.Fatal("failed")
	})
}

// Found by neither: staticcheck builds the instance of a generic function as a wrapper that calls the generic body,
// one level deeper than either reads.
func genericStarted(t *testing.T) {
	go genericFails(t, 1)
}

func genericFails[Value any](t *testing.T, value Value) {
	t.Fatal(value)
}

// Found by both: the same named function started twice, each go statement its own finding.
func startedTwice(t *testing.T) {
	go fails(t)
	go fails(t)
}

// Found by neither: the goroutine closes over the test and only reads it.
func readsOnly(t *testing.T) {
	go func() {
		_ = t.Name()
	}()
}
