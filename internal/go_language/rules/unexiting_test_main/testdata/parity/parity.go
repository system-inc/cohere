// Package parity holds TestMains, functions and methods of that name, that run the tests and exit or do not, and
// declarations that only look like TestMains, in a module of Go 1.14, whose test framework does not exit for them,
// where the port and staticcheck's SA3000 still agree finding for finding. The differential test runs both over each
// and compares.
package parity

import (
	"os"
	system "os"
	"runtime"
	"testing"
)

func helper() {}

// Found by both: the tests run, and nothing exits.
func TestMain(m *testing.M) {
	m.Run()
}

type renamed struct{}

// Found by both: a method named TestMain, its parameter named otherwise.
func (renamed) TestMain(tests *testing.M) {
	tests.Run()
}

type kept struct{}

// Found by both: the code Run returns kept and dropped.
func (kept) TestMain(m *testing.M) {
	code := m.Run()
	_ = code
}

type inLiteral struct{}

// Found by both: Run called inside a function literal of the TestMain.
func (inLiteral) TestMain(m *testing.M) {
	func() {
		m.Run()
	}()
}

type exitValue struct{}

// Found by both: os.Exit taken as a value and called through it is no call to os.Exit.
func (exitValue) TestMain(m *testing.M) {
	exit := os.Exit
	exit(m.Run())
}

type goexit struct{}

// Found by both: runtime.Goexit is not os.Exit.
func (goexit) TestMain(m *testing.M) {
	m.Run()
	runtime.Goexit()
}

type branch struct{}

// Found by both: Run inside a branch.
func (branch) TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		m.Run()
	}
}

type exits struct{}

// Found by neither: the exit code passed to os.Exit.
func (exits) TestMain(m *testing.M) {
	os.Exit(m.Run())
}

type exitsAfter struct{}

// Found by neither: os.Exit after Run.
func (exitsAfter) TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}

type aliased struct{}

// Found by neither: os.Exit through an import's other name.
func (aliased) TestMain(m *testing.M) {
	m.Run()
	system.Exit(0)
}

type deferred struct{}

// Found by neither: os.Exit in a deferred literal.
func (deferred) TestMain(m *testing.M) {
	defer func() { os.Exit(0) }()
	m.Run()
}

type noRun struct{}

// Found by neither: the tests never run.
func (noRun) TestMain(m *testing.M) {
	helper()
}

type parenthesized struct{}

// Found by neither: Run on the parameter in parentheses, which is no name.
func (parenthesized) TestMain(m *testing.M) {
	(m).Run()
}

type shadowed struct{}

// Found by neither: Run on another variable of the same name.
func (shadowed) TestMain(m *testing.M) {
	{
		m := &testing.M{}
		m.Run()
	}
}

type value struct{}

// Found by neither: a testing.M, not a pointer to one.
func (value) TestMain(m testing.M) {
	m.Run()
}

type twoParameters struct{}

// Found by neither: two parameters.
func (twoParameters) TestMain(m *testing.M, extra int) {
	m.Run()
}

type twoNames struct{}

// Found by neither: two names in the one parameter field.
func (twoNames) TestMain(m, other *testing.M) {
	m.Run()
}

type unnamed struct{}

// Found by neither: an unnamed parameter.
func (unnamed) TestMain(*testing.M) {
	helper()
}

type otherName struct{}

// Found by neither: another name.
func (otherName) TestMainly(m *testing.M) {
	m.Run()
}
