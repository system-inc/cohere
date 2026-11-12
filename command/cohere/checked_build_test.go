package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// An extra build loaded again reaches `--json` with each load's count checked from source, as the engine's record
// writes it (#k8gty9e): the front door reads the build into checkedBuild and writes it on, so a count the engine names
// and the front door leaves out would never reach a reader.
func TestABuildsRoundsCheckedFromSourceReachTheJson(test *testing.T) {
	test.Parallel()

	written, writeError := json.Marshal(contract.BuildRecord{Target: "windows/arm64", CheckedFromSource: 7,
		CheckedFromSourceRounds: []int{3, 4}, PackagesLoaded: 9})
	if writeError != nil {
		test.Fatal(writeError)
	}
	var read checkedBuild
	if readError := json.Unmarshal(written, &read); readError != nil {
		test.Fatal(readError)
	}
	forwarded, forwardError := json.Marshal(read)
	if forwardError != nil {
		test.Fatal(forwardError)
	}
	var reread contract.BuildRecord
	if rereadError := json.Unmarshal(forwarded, &reread); rereadError != nil {
		test.Fatal(rereadError)
	}
	if reread.CheckedFromSource != 7 || !slices.Equal(reread.CheckedFromSourceRounds, []int{3, 4}) {
		test.Errorf("the front door wrote %s, want the engine's 7 checked from source over the rounds [3 4]", forwarded)
	}
}
