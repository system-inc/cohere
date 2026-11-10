package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/format_files"
)

// A format walk that read no index says so, in the scope line `--verbose` prints and in the `--json` summary's gaps,
// and one that read it says nothing (#14skmqy).
// Not parallel: noteFormatWalkGaps writes the package-level activeSummary, which it reads back.
func TestAWalkThatReadNoIndexSaysWhy(test *testing.T) {
	savedSummary := activeSummary
	defer func() { activeSummary = savedSummary }()
	fallback := "/project/.git/index is a split index, whose entries live partly in a shared index this reader " +
		"does not merge"
	walked := format_files.Enumeration{Root: "/project", Walked: 3, IndexFallback: fallback}

	want := "git's index not read (" + fallback + "), so a file git tracks under a pattern that matches it was " +
		"counted ignored"
	if description := describeEnumeration(walked, 2); !strings.Contains(description, want) {
		test.Errorf("the scope line does not say the index went unread:\n%s", description)
	}
	activeSummary = runSummary{}
	noteFormatWalkGaps(walked)
	if activeSummary.Gaps.IndexFallback != fallback {
		test.Errorf("the gaps hold %q, want the walk's fallback", activeSummary.Gaps.IndexFallback)
	}
	encoded, encodeError := json.Marshal(summaryAsJson(activeSummary))
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if !strings.Contains(string(encoded), `"indexFallback":`) {
		test.Errorf("the --json summary does not carry the fallback:\n%s", encoded)
	}

	read := format_files.Enumeration{Root: "/project", Walked: 3}
	activeSummary = runSummary{}
	noteFormatWalkGaps(read)
	if description := describeEnumeration(read, 2); strings.Contains(description, "index not read") ||
		activeSummary.Gaps.IndexFallback != "" {
		test.Errorf("a walk that read its index says it did not: %q, %q", description, activeSummary.Gaps.IndexFallback)
	}
}
