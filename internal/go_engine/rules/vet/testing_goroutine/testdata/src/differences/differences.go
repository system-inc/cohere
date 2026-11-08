// Package differences holds the shapes where the port parts from the original, each finding the port changes recorded
// on its line, with the reason on the line above. A go statement's call arguments, and a t.Run's name, are evaluated
// by the goroutine executing the statement, so the port judges them with the region enclosing the statement, and the
// original with the region the statement starts. Every record holds with the subtest flag on, as the tests run it.
package differences

import (
	"fmt"
	"testing"
)

// named is a test helper a t.Run names.
func named(t *testing.T) {}

// At the test's own level, the test's goroutine evaluates every argument, so a forbidden call in one is the test's.
func TestArgumentsAtTestLevel(t *testing.T) {
	// The test's goroutine runs the literal called in place, as it evaluates fmt.Println's argument.
	go fmt.Println(func() int { // port drops: call to (*testing.T).FailNow from a non-test goroutine
		t.FailNow()
		return 0
	}())
	// The same, as a method's argument.
	go t.Log(func() string { // port drops: call to (*testing.T).SkipNow from a non-test goroutine
		t.SkipNow()
		return ""
	}())
	go func(int) {}(func() int {
		// The original reports a literal callee's argument at the call, which the test's goroutine makes.
		t.Fatal("argument") // port drops: call to (*testing.T).Fatal from a non-test goroutine
		return 0
	}())
	// t.Fatal runs in the started goroutine, as both report, and its argument's t.SkipNow in the test's.
	go t.Fatal(func() string { // port drops: call to (*testing.T).SkipNow from a non-test goroutine
		t.SkipNow()
		return ""
	}())
	// A go statement within an argument is evaluated by the test too, and its own argument with it.
	go fmt.Println(func() int {
		// The original reports this at the inner go statement, though the test's goroutine evaluates its argument.
		go fmt.Println(func() int { t.FailNow(); return 0 }()) // port drops: call to (*testing.T).FailNow from a non-test goroutine
		return 0
	}())
}

// A literal handed over as an argument runs where it is called, likely the started goroutine, and both judge it
// there; a literal called in place runs as the test evaluates the arguments.
func TestHandedOverAndCalledInPlace(t *testing.T) {
	var start func(func(), int)
	// The second argument's call is the test's own; the first's is reported by both.
	go start(func() { // port drops: call to (*testing.T).SkipNow from a non-test goroutine (start calls (*testing.T).SkipNow)
		t.Fatal("handed over")
	}, func() int {
		t.SkipNow()
		return 0
	}())
}

// The test evaluates a t.Run's name before the subtest starts, so a forbidden call there is the test's own.
func TestRunNameAtTestLevel(t *testing.T) {
	// The original reports it as the subtest's call on a test outside it.
	t.Run(func() string { // port drops: call to (*testing.T).Fatal on t defined outside of the subtest (named calls (*testing.T).Fatal)
		t.Fatal("name")
		return "name"
	}(), named)
}

// Within a subtest, the subtest's goroutine evaluates a go statement's arguments.
func TestArgumentInASubtest(t *testing.T) {
	t.Run("sub", func(sub *testing.T) {
		go func(int) {}(func() int {
			// The call is on the test outside the subtest, from the subtest's goroutine, not from a started one.
			t.FailNow() // port renames: call to (*testing.T).FailNow from a non-test goroutine => call to (*testing.T).FailNow on t defined outside of the subtest
			// The subtest's own call, in its own goroutine, is fine.
			sub.FailNow() // port drops: call to (*testing.T).FailNow from a non-test goroutine
			return 0
		}())
	})
}

// Within a started literal, the literal's goroutine evaluates a go statement's arguments and a t.Run's name, and it
// is not the test's, so the call is still reported, against that goroutine.
func TestArgumentsInAStartedLiteral(t *testing.T) {
	go func() {
		// The original reports at the inner go statement, which does not evaluate the argument.
		go fmt.Println(func() int { // port drops: call to (*testing.T).FailNow from a non-test goroutine
			// The outer literal's goroutine makes the call, and the port reports it here, as for any call it makes.
			t.FailNow() // port finds: call to (*testing.T).FailNow from a non-test goroutine
			return 0
		}())
		// The original reports the name's call as the subtest's.
		t.Run(func() string { // port drops: call to (*testing.T).Fatal on t defined outside of the subtest (named calls (*testing.T).Fatal)
			// The outer literal's goroutine evaluates the name, before any subtest starts.
			t.Fatal("name") // port finds: call to (*testing.T).Fatal from a non-test goroutine
			return "name"
		}(), named)
	}()
}

// spawner evaluates its go statement's argument in its own goroutine, which a test starts.
func spawner(t *testing.T) {
	// The original reports at this go statement, which does not evaluate the argument; spawner's goroutine does.
	go fmt.Println(func() int { // port drops: call to (*testing.T).FailNow from a non-test goroutine
		t.FailNow()
		return 0
	}())
}

func TestStartsSpawner(t *testing.T) {
	// spawner's goroutine makes the call, and the port reports it where spawner is started, naming what it calls.
	go spawner(t) // port finds: call to (*testing.T).FailNow from a non-test goroutine (spawner calls (*testing.T).FailNow)
}
