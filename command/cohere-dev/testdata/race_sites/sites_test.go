package sites

import (
	"testing"

	"github.com/system-inc/cohere/internal/testing/needs"
)

// A fixture for the -race list's guard: two tests start goroutines of their own, one through two helpers.

func TestDirect(test *testing.T) {
	go func() {}()
}

func TestThroughHelpers(test *testing.T) {
	outer()
}

func outer() { inner() }

func inner() {
	go func() {}()
}

func TestFansOut(test *testing.T) { fanOut([]int{1, 2}) }

func TestRuns(test *testing.T) { (&pool{}).run(&pool{}) }

func TestPlain(test *testing.T) { plain() }

func TestSkips(test *testing.T) {
	test.Skip("skips on some boxes")
	fanOut([]int{1, 2})
}

func TestBesides(test *testing.T) { besides() }

func TestOptsIn(test *testing.T) {
	needs.OptIn(test, "COHERE_FIXTURE", "skips unless asked")
	fanOut([]int{1, 2})
}

type starter struct{}

func (starter) start() { go func() {}() }

func TestThroughAMethod(test *testing.T) { starter{}.start() }
