package parity

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// goFiles counts the Go files of the packages loaded, which the planted pair judges, and assemblyFiles their assembly
// files, which it never does and the planted fixtures hold none of.
func goFiles(_ testing.TB, loaded []*packages.Package) int {
	counted := 0
	for _, root := range loaded {
		counted += len(root.GoFiles)
	}
	return counted
}

func assemblyFiles(_ testing.TB, loaded []*packages.Package) int {
	counted := 0
	for _, root := range loaded {
		for _, file := range root.OtherFiles {
			if strings.HasSuffix(file, ".s") {
				counted++
			}
		}
	}
	return counted
}

// Neither side finds a call in silent, whose Go files the two were handed, so the run judged something and passes,
// where RequireIdentical refuses it.
func TestRequireNeitherFindsPassesTwoEmptySidesThatJudged(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireNeitherFinds(recording, original, port, analysistest.TestData(), goFiles, "silent")
	}))
}

// Two empty sides over a load that handed them nothing they judge prove nothing.
func TestRequireNeitherFindsFailsARunThatJudgedNothing(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireNeitherFinds(recording, original, port, analysistest.TestData(), assemblyFiles, "silent")
	}), "judged counts 0 in the packages loaded, so neither side was handed anything to judge, and finding nothing "+
		"proves nothing")
}

// Each finding of either side fails, named, whichever side makes it.
func TestRequireNeitherFindsNamesEveryFinding(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireNeitherFinds(recording, calling(gainedMessages), calling(sharedMessages), analysistest.TestData(),
			goFiles, "same")
	}),
		"the port found same/same.go:7:2: call to shared, where neither may find anything",
		"the port found same/same.go:8:2: call to shared, where neither may find anything",
	)
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireNeitherFinds(recording, calling(sharedMessages), calling(gainedMessages), analysistest.TestData(),
			goFiles, "same")
	}),
		"the original found same/same.go:7:2: call to shared, where neither may find anything",
		"the original found same/same.go:8:2: call to shared, where neither may find anything",
	)
}

// The call to shared is in narrow.go, which only 386 builds, so on amd64 neither finds it and on 386 both do.
func TestRequireNeitherFindsLoadsForTheTargetNamed(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		onAmd64.RequireNeitherFinds(recording, calling(sharedMessages), calling(sharedMessages),
			analysistest.TestData(), goFiles, "targeted")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		on386.RequireNeitherFinds(recording, calling(sharedMessages), calling(sharedMessages),
			analysistest.TestData(), goFiles, "targeted")
	}),
		"the original found targeted/narrow.go:6:2: call to shared, where neither may find anything",
		"the port found targeted/narrow.go:6:2: call to shared, where neither may find anything",
	)
}
