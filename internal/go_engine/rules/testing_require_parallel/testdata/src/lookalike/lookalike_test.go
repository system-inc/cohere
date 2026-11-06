package lookalike

import (
	"os"
	"testing"
)

// runner has Parallel and Run methods of its own, with testing.T's shapes, which go test never calls.
type runner struct{}

func (runner) Parallel() {}

func (runner) Run(name string, function func(test *testing.T)) bool { return true }

// A method named like a test is no test, since go test runs only functions.
func (runner) TestSerial(test *testing.T) {
	runner{}.Parallel()
}

// A Run on the lookalike starts no subtest, so its literal is not judged.
func TestLookalikeRun(test *testing.T) {
	test.Parallel()
	runner{}.Run("lookalike", func(subtest *testing.T) {
		subtest.Log("serial")
	})
}

// A benchmark's Run takes a *testing.B, and benchmarks are not judged.
func BenchmarkSerial(benchmark *testing.B) {
	benchmark.Run("serial", func(inner *testing.B) {})
}

func FuzzSerial(fuzz *testing.F) {}

func ExampleSerial() {}

func TestMain(main *testing.M) {
	os.Exit(main.Run())
}

// go test runs a Test name only when what follows Test isn't a lower-case letter.
func Testify(test *testing.T) {}

func helper(test *testing.T) {
	test.Helper()
}

// A Parallel on the lookalike after the real one changes nothing.
func TestLookalikeAfterParallel(test *testing.T) {
	test.Parallel()
	runner{}.Parallel()
	helper(test)
}
