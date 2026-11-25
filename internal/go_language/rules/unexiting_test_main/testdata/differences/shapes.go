package differences

import (
	"os"
	"testing"
)

type directExit struct{}

// Found by neither: the code Run returns, passed to os.Exit.
func (directExit) TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}

type literalExit struct{}

// Found by neither: os.Exit inside a function literal the TestMain calls is its exit.
func (literalExit) TestMain(m *testing.M) {
	code := m.Run()
	func() { os.Exit(code) }()
}

type deferredExit struct{}

// Found by neither: a deferred os.Exit is an exit, judged as a call.
func (deferredExit) TestMain(m *testing.M) {
	code := m.Run()
	defer os.Exit(code)
}

type aliasExit struct{}

// Found by both: os.Exit through a local alias is no call to os.Exit, the original's false positive, which the port
// keeps.
func (aliasExit) TestMain(m *testing.M) {
	exit := os.Exit
	exit(m.Run())
}
