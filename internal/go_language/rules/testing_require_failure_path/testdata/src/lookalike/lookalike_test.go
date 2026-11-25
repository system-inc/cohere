package lookalike

import (
	"os"
	"testing"
)

// runner is a type with a method named like a test, which go test never calls, so it is no test.
type runner struct{}

func (runner) TestCannotFail(test *testing.T) {
	test.Log("nothing checked")
}

// A benchmark, fuzz target and example take something else or are named otherwise, so none is judged.
func BenchmarkCannotFail(benchmark *testing.B) {
	benchmark.Log("nothing checked")
}

func FuzzCannotFail(fuzz *testing.F) {}

func ExampleCannotFail() {}

func TestMain(main *testing.M) {
	os.Exit(main.Run())
}

// go test runs a Test name only when what follows Test isn't a lower-case letter, so this is a helper.
func Testify(test *testing.T) {
	test.Log("nothing checked")
}

// A helper that takes the test and checks nothing is not itself a test.
func helper(test *testing.T) {
	test.Helper()
}

// A test that only calls a helper with the test looks like it checks nothing, and the helper may yet fail it.
func TestOnlyCallsHelper(test *testing.T) {
	helper(test)
}

// A test whose only check hides in a closure assigned to a variable still has that check.
func TestCheckInAClosure(test *testing.T) {
	verify := func(got int) {
		if got != 42 {
			test.Fatalf("got %d", got)
		}
	}
	verify(42)
}
