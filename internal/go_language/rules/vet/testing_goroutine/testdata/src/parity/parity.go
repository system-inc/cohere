// Package parity holds every branch of testinggoroutine's judgment, and the shapes most likely to read differently,
// for the original and the port to judge side by side. Each comment says what both report there, with the subtest
// flag on; with it off, every report on a test "defined outside of the subtest" goes, and nothing else changes.
package parity

import (
	"fmt"
	"testing"
)

// Every forbidden method, from a goroutine a literal starts: each one reported at its call, and the allowed
// methods beside them are clean.
func TestEveryForbiddenMethod(t *testing.T) {
	go func() {
		t.FailNow()           // reported: (*testing.T).FailNow
		t.Fatal("fatal")      // reported: (*testing.T).Fatal
		t.Fatalf("%d", 1)     // reported: (*testing.T).Fatalf
		t.Skip("skip")        // reported: (*testing.T).Skip
		t.Skipf("%d", 1)      // reported: (*testing.T).Skipf
		t.SkipNow()           // reported: (*testing.T).SkipNow
		t.Error("error")      // clean
		t.Errorf("%d", 1)     // clean
		t.Log("log")          // clean
		t.Fail()              // clean
		_ = t.Skipped()       // clean
		_ = t.Failed()        // clean
		(t).Fatal("parens")   // reported: the receiver's parentheses are looked through
		(t.Fatal)("parens")   // reported: the callee's parentheses are looked through
		fatal := t.Fatal      // clean: a method value is no call
		fatal("later")        // clean: called by a name, not a selector
		(*testing.T).Fatal(t) // clean: a method expression's receiver is no variable
	}()
}

// The forbidden calls called directly by the test and deferred by it are its own.
func TestDirectAndDeferred(t *testing.T) {
	defer t.FailNow() // clean
	t.Skip("direct")  // clean
	func() {
		t.Fatal("an immediately called literal runs in the test's goroutine") // clean
	}()
}

// A go statement of the forbidden method itself is reported at the statement.
func TestGoForbidden(t *testing.T) {
	go t.FailNow()           // reported: (*testing.T).FailNow
	go t.Skipf("%s", "skip") // reported: (*testing.T).Skipf
	go t.Log("log")          // clean
}

// named is a named local function a go statement starts.
func named(t *testing.T) {
	t.Fatal("named")
}

// namedTwice is started twice by one test, each start its own finding.
func namedTwice(t *testing.T) {
	t.SkipNow()
}

// cleanNamed calls nothing forbidden.
func cleanNamed(t *testing.T) {
	t.Log("clean")
}

// Named local functions are reported at the go statement, saying what they call.
func TestNamedLocalFunctions(t *testing.T) {
	go named(t)      // reported: (named calls (*testing.T).Fatal)
	go namedTwice(t) // reported
	go namedTwice(t) // reported
	go cleanNamed(t) // clean
	go (named)(t)    // reported: the parentheses are looked through
	go fmt.Println() // clean: a function declared elsewhere is not followed
}

// helper is a type whose methods tests start.
type helper struct{}

func (helper) value(t *testing.T) { t.FailNow() }

func (*helper) pointer(t *testing.T) { t.Skip("pointer") }

// Methods are reported at the go statement, with no name saying what they call, since the callee is a selector.
func TestMethods(t *testing.T) {
	var started helper
	go started.value(t)          // reported: (*testing.T).FailNow
	go started.pointer(t)        // reported: (*testing.T).Skip
	go (*helper).pointer(nil, t) // reported: a method expression is a static call too
	value := started.value
	go value(t) // clean: a method value in a variable is no literal and no static call
}

// box is a generic type whose method an instantiation calls.
type box[Element any] struct{}

func (box[Element]) help(t *testing.T) { t.FailNow() }

// generic is a generic function a go statement instantiates.
func generic[Element any](t *testing.T) {
	t.Fatal("generic")
}

// pair is a generic function with two type parameters.
func pair[First, Second any](t *testing.T) {
	t.Fatal("pair")
}

// An instantiated generic function, and a method of an instantiated generic type, are followed to their declarations,
// since the static callee is the generic origin.
func TestGenerics(t *testing.T) {
	go generic[int](t)      // reported: at the go statement, with no name, since the callee is an index expression
	go pair[int, string](t) // reported
	go box[int]{}.help(t)   // reported: the callee is the declared method
	var values []func(*testing.T)
	go values[0](t) // clean: an index into a slice is no instantiation
}

// genericHelper takes a type parameter constrained by testing.TB, whose forbidden methods are the interface's.
func genericHelper[TB testing.TB](t *testing.T, tb TB) {
	go tb.Fatal("type parameter") // reported: (TB).Fatal
	go t.Log("clean")
}

// Local variables first assigned a function literal are followed, by := and by var, and reported at the go
// statement, saying the variable's name.
func TestLocalLiterals(t *testing.T) {
	short := func(inner *testing.T) {
		inner.Fatal("short")
	}
	go short(t) // reported: (short calls (*testing.T).Fatal)
	var long = func(inner *testing.T) {
		inner.SkipNow()
	}
	go long(t) // reported: (long calls (*testing.T).SkipNow)
	var typed func(*testing.T) = func(inner *testing.T) {
		inner.FailNow()
	}
	go typed(t) // reported
	var declared func(*testing.T)
	declared = func(inner *testing.T) { inner.FailNow() }
	go declared(t) // clean: the variable was declared with no literal, and a later assignment is not followed
	clean := func(inner *testing.T) { inner.Log("clean") }
	go clean(t) // clean
	first, second := 1, func(inner *testing.T) { inner.Skip("second") }
	_ = first
	go second(t) // reported: the second of two names
	{
		short := func(inner *testing.T) { inner.Log("shadowed") }
		go short(t) // clean: the inner short is the one called
	}
	go (short)(t) // reported: the parentheses are looked through
}

// packageLevel is a package-level variable first assigned a literal, which a test in this file starts.
var packageLevel = func(t *testing.T) {
	t.Fatal("package level")
}

// A package-level literal declared in the same file is followed, as the parser resolved it.
func TestPackageLevel(t *testing.T) {
	go packageLevel(t)     // reported: (packageLevel calls (*testing.T).Fatal)
	go otherFileLiteral(t) // clean: the parser does not resolve a name declared in another file
}

// A closure passed along to a function value is judged where the go statement holds it.
func spawnWith(t *testing.T, start func(func())) {
	go start(func() {
		t.Fatal("passed along") // reported: at the go statement, (start calls (*testing.T).Fatal)
	})
}

// A closure handed to a local literal that starts it is not followed into that literal.
func TestClosurePassedAlong(t *testing.T) {
	run := func(started func()) {
		go started() // clean: a parameter is no literal
	}
	run(func() {
		t.Fatal("passed into a closure") // clean: not started by a go statement the pass sees
	})
	go run(func() {
		t.Fatal("passed into a started closure") // clean: run's literal is the region, and this literal is not in it
	})
	spawnWith(t, func(started func()) { started() })
}

// Nested goroutines are each judged once, by the innermost go statement.
func TestNestedGoroutines(t *testing.T) {
	go func() {
		go func() {
			t.Fatal("inner") // reported once
		}()
		t.SkipNow() // reported
	}()
}

// A go statement's argument is evaluated by the goroutine executing the statement. Within a started literal, that
// goroutine is the literal's, and a go statement whose callee is a literal reports at the forbidden call, so the
// original, which judges the argument with the inner go statement, and the port, which judges it with the outer
// literal, report the same. The differences fixture holds the shapes where the two part.
func TestArgumentsOfAGoStatement(t *testing.T) {
	go func() {
		go func(int) {}(func() int {
			t.FailNow() // reported: (*testing.T).FailNow from a non-test goroutine
			return 0
		}())
		go fmt.Println(t.Name()) // clean: no forbidden call in the argument
	}()
}

// A subtest's literal, a variable first assigned one, and a named function, judged against the test declared outside.
func TestSubtests(t *testing.T) {
	t.Run("literal", func(sub *testing.T) {
		t.FailNow()   // reported: on t defined outside of the subtest
		sub.FailNow() // clean
	})
	subtest := func(sub *testing.T) {
		t.Skip("outer") // reported once, at the t.Run below, (subtest calls (*testing.T).Skip)
		sub.Skip("own")
	}
	t.Run("variable", subtest)
	subtest(t)                           // clean: a plain call
	t.Run("named", named)                // clean: the named function's body is not followed
	t.Run("method", (&helper{}).pointer) // clean
	t.Run("paren", (func(sub *testing.T) {
		t.Fatal("parenthesized") // reported
	}))
	go t.Run("started", func(sub *testing.T) {
		t.Fatal("started") // reported: on t defined outside of the subtest, and not from a non-test goroutine
	})
}

// Nested subtests judge each test against the subtest it is called in.
func TestNestedSubtests(t *testing.T) {
	t.Run("outer", func(outer *testing.T) {
		outer.Run("inner", func(inner *testing.T) {
			t.Fatal("the test")      // reported: on t
			outer.Fatal("the outer") // reported: on outer
			inner.Fatal("its own")   // clean
		})
		t.FailNow()     // reported: on t
		outer.FailNow() // clean
		go func() {
			outer.FailNow() // reported: from a non-test goroutine
		}()
	})
}

// fakeRunner has a Run method of its own, which is no subtest.
type fakeRunner struct{}

func (fakeRunner) Run(name string, function func(*testing.T)) bool { return true }

func TestNotTestingRun(t *testing.T) {
	var runner fakeRunner
	runner.Run("fake", func(sub *testing.T) {
		t.Fatal("not a subtest") // clean
	})
}

// Benchmarks are judged as tests are, with their own subtests.
func BenchmarkGoroutines(b *testing.B) {
	go func() {
		b.Fatal("benchmark") // reported: (*testing.B).Fatal
	}()
	b.Run("sub", func(sub *testing.B) {
		b.SkipNow()   // reported: on b defined outside of the subtest
		sub.SkipNow() // clean
	})
	go b.FailNow() // reported
}

// A fuzz test's declaration takes neither *testing.T nor *testing.B, so nothing it starts is judged, a gap the
// original has and the port keeps.
func FuzzGoroutines(f *testing.F) {
	go f.Fatal("fuzz") // clean
	f.Fuzz(func(t *testing.T, input []byte) {
		go t.Fatal("fuzzed") // clean
	})
}

// A declaration that takes a *testing.T and a *testing.F is judged, and the fuzz test's methods are forbidden too.
func fuzzHelper(t *testing.T, f *testing.F) {
	go f.Skip("fuzz helper") // reported: (*testing.F).Skip
	go func() {
		f.Fatal("fuzz helper") // reported: (*testing.F).Fatal
	}()
}

// wrapper embeds a test, whose promoted methods are the testing package's.
type wrapper struct {
	*testing.T
}

// holder holds a test in a field.
type holder struct {
	test *testing.T
}

// An embedded test's promoted method is named on the wrapper; a field's receiver is no variable.
func TestEmbedded(t *testing.T) {
	wrapped := wrapper{t}
	go wrapped.Fatal("embedded") // reported: (parity.wrapper).Fatal
	held := holder{t}
	go held.test.Fatal("field") // clean: the receiver is a selector
	go wrapped.T.Fatal("named") // clean: the receiver is a selector
}

// pointerAlias names the pointer to a test.
type pointerAlias = *testing.T

// testAlias names a test.
type testAlias = testing.T

// An alias of the pointer is seen through when the method is named.
func TestAliases(t *testing.T) {
	var aliased pointerAlias = t
	go aliased.Fatal("pointer alias") // reported: (*testing.T).Fatal
	var throughType *testAlias = t
	go throughType.Fatal("type alias") // reported: (*parity.testAlias).Fatal, the alias named as written
}

// A parameter written through an alias is no *testing.T by how it is written, so the declaration is not judged.
func TestWrittenThroughAlias(t pointerAlias) {
	go t.Fatal("alias") // clean
}

// global is a test no declaration takes as a parameter.
var global *testing.T

// A declaration that takes no test or benchmark is not descended into.

func noTestParameter() {
	go global.Fatal("global") // clean: the declaration takes no test
}

// A go statement outside any declaration is judged.
var _ = func() bool {
	go global.Fatal("package level") // reported: (*testing.T).Fatal
	return true
}()

// testsAsResults returns a test, which is no variable when it is the receiver.
func testsAsResults(t *testing.T) *testing.T { return t }

func TestCallResultReceiver(t *testing.T) {
	go testsAsResults(t).Fatal("result") // clean
	go func() {
		testsAsResults(t).FailNow() // clean
	}()
}

// A test helper started from two tests is one region with two starts.
func sharedHelper(t *testing.T) {
	t.FailNow()
}

func TestSharedFirst(t *testing.T) {
	go sharedHelper(t) // reported
}

func TestSharedSecond(t *testing.T) {
	go sharedHelper(t)            // reported
	t.Run("shared", sharedHelper) // clean: a named function in t.Run is not followed
}

// A recursive helper's go statement of itself is reported, and its own start is judged too.
func recursive(t *testing.T, depth int) {
	if depth > 0 {
		go recursive(t, depth-1) // reported: (recursive calls (*testing.T).Fatal)
	}
	t.Fatal("recursive")
}

// A go statement within a started helper is its own region, not the helper's.
func spawner(t *testing.T) {
	go func() {
		t.Fatal("spawned") // reported once, by the inner go statement
	}()
}

func TestSpawner(t *testing.T) {
	go spawner(t) // clean: the helper's own forbidden calls are in another region
}

// A method declared with a test parameter is descended into.
func (helper) TestMethodDeclaration(t *testing.T) {
	go t.FailNow() // reported
}

// A test parameter among others, unnamed, or named blank, still counts.
func TestManyParameters(name string, _ *testing.T, b *testing.B) {
	go b.Fatal("many") // reported
}
