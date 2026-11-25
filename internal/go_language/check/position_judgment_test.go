package check

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/load"
)

// An entry encoded without the load's files, as the keep encodes those it holds for the extra builds, reads as not
// judged: asking whether its findings name another package's file is refused, never answered false. One encoded with
// them reads as judged, here independent, since it holds no finding.
func TestAnEntryEncodedWithoutTheLoadsFilesReadsAsNotJudged(test *testing.T) {
	test.Parallel()

	// An analyzer that learns facts and that the entry holds nothing of, so it has no output digest to compute.
	known := &cacheAnalyzers{facts: []*analysis.Analyzer{{Name: "learner"}}}
	saved := &packages.Package{ID: "example.com/judged"}
	unjudged, encodeError := encodeEntry(known, nil, saved, nil, load.PackageWithheld{}, entryDemand{}, nil, nil, nil,
		false)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if dependent, readError := unjudged.positionDependent.read(saved.ID); readError == nil {
		test.Errorf("the entry encoded without the load's files read as judged, dependent %t, want refused", dependent)
	}
	judged, encodeError := encodeEntry(known, nil, saved, nil, load.PackageWithheld{}, entryDemand{}, nil,
		newKnownFiles(), nil, false)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if dependent, readError := judged.positionDependent.read(saved.ID); readError != nil || dependent {
		test.Errorf("the entry encoded with the load's files read dependent %t, error %v, want judged independent",
			dependent, readError)
	}
}
