package allowed

import (
	"os"
	"testing"
)

func TestParallelFirst(test *testing.T) {
	test.Parallel()
	test.Log("parallel")
}

func Test(test *testing.T) {
	test.Parallel()
}

// Parentheses around the receiver or the call change nothing about which test is marked.
func TestParenthesized(test *testing.T) {
	((test).Parallel())
}

// Not parallel: it sets the process's working directory, which every test in the package shares.
func TestReason(test *testing.T) {
	_ = os.Chdir(".")
}

// TestReasonInADocComment says what it does first.
//
// Not parallel: test.Setenv refuses a parallel test, and the environment is the process's.
func TestReasonInADocComment(test *testing.T) {
	test.Setenv("COHERE_FIXTURE", "set")
}

/* Not parallel: a block comment may say it too. */
func TestReasonInABlockComment(test *testing.T) {}

func TestParallelSubtests(test *testing.T) {
	test.Parallel()
	test.Run("literal", func(subtest *testing.T) {
		subtest.Parallel()
		// A subtest of a subtest is judged the same way, against its own parameter.
		subtest.Run("nested", func(nested *testing.T) {
			nested.Parallel()
		})
	})
	test.Run("named", parallelSubtest)
	for _, name := range []string{"first", "second"} {
		test.Run(name, func(subtest *testing.T) {
			subtest.Parallel()
		})
	}
}

func parallelSubtest(subtest *testing.T) {
	subtest.Parallel()
}

// A subtest started in a helper that is no test is judged, and this one runs in parallel.
func runCases(test *testing.T) {
	test.Run("case", func(subtest *testing.T) {
		subtest.Parallel()
	})
}

// A generic function or method resolves to its declaration, whose parameter is the one its body calls Parallel on.
func TestGenericParallelSubtests(test *testing.T) {
	test.Parallel()
	test.Run("one type argument", parallelGeneric[int])
	test.Run("two type arguments", parallelGenericPair[int, string])
	suite := &genericSuite[int]{}
	test.Run("generic method", suite.parallel)
}

func parallelGeneric[Value any](subtest *testing.T) {
	subtest.Parallel()
}

func parallelGenericPair[Key comparable, Value any](subtest *testing.T) {
	subtest.Parallel()
}

type genericSuite[Value any] struct{}

func (suite *genericSuite[Value]) parallel(subtest *testing.T) {
	subtest.Parallel()
}

// A subtest that shadows the outer name is still its own parameter.
func TestShadowedSubtest(test *testing.T) {
	test.Parallel()
	test.Run("shadowed", func(test *testing.T) {
		test.Parallel()
	})
}

func TestSubtestReasons(test *testing.T) {
	test.Parallel()
	// Not parallel: it sets a package variable its siblings read.
	test.Run("reasoned", func(subtest *testing.T) {
		subtest.Log("serial")
	})
	// Not parallel: its result is read before the next subtest starts.
	if !test.Run("in a condition", func(subtest *testing.T) {}) {
		test.Log("failed")
	}
	test.Run("named with a reason", reasonedSubtest)
}

// Not parallel: it swaps a package variable for the duration.
func reasonedSubtest(subtest *testing.T) {}

// A subtest whose function is held in a variable or built by a call has a body the rule cannot see, so it is not
// judged.
func TestSubtestsTheRuleCannotSee(test *testing.T) {
	test.Parallel()
	held := func(subtest *testing.T) {}
	test.Run("held", held)
	test.Run("built", build())
}

func build() func(*testing.T) {
	return func(subtest *testing.T) {}
}
