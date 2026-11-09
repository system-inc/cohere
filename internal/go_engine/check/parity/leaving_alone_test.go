package parity

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// Loaded for 386, targeted hands over narrow.go, where both find the call to shared, and targeted.go, which holds no
// call. A pair finding the same over files they leave alone passes.
func TestRequireIdenticalLeavingAlonePassesAFileLeftAlone(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireIdenticalLeavingAlone(recording, calling(sharedMessages), calling(sharedMessages),
			analysistest.TestData(), []string{"targeted/targeted.go"}, "targeted")
	}))
}

// Each side's finding in a file named alone fails, named, though the two agree.
func TestRequireIdenticalLeavingAloneFailsAFindingInAFileLeftAlone(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireIdenticalLeavingAlone(recording, calling(sharedMessages), calling(sharedMessages),
			analysistest.TestData(), []string{"targeted/narrow.go"}, "targeted")
	}),
		"the original found targeted/narrow.go:6:2: call to shared, in a file both must leave alone",
		"the port found targeted/narrow.go:6:2: call to shared, in a file both must leave alone",
	)
}

// wide.go is built for every target but 386, so the load for 386 never hands it over, and leaving it alone proves
// nothing.
func TestRequireIdenticalLeavingAloneFailsAFileNeverHanded(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireIdenticalLeavingAlone(recording, calling(sharedMessages), calling(sharedMessages),
			analysistest.TestData(), []string{"targeted/targeted.go", "targeted/wide.go"}, "targeted")
	}), "the load hands neither side targeted/wide.go, so leaving it alone proves nothing")
}

// The empty-sides guard holds as in RequireIdentical: on the machine's own target narrow.go is not loaded, and the
// original finds nothing.
func TestRequireIdenticalLeavingAloneFailsWhenTheOriginalFindsNothing(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalLeavingAlone(recording, calling(sharedMessages), calling(sharedMessages),
			analysistest.TestData(), []string{"targeted/targeted.go"}, "targeted")
	}), "the original found nothing in the fixtures, so the comparison proves nothing")
}
