package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// An extra build loaded again reaches `--json` with each load's count checked from source, and a build under
// `--timing` with what it held, as the engine's record writes them (#k8gty9e): the front door reads the build into
// checkedBuild and writes it on, so a count the engine names and the front door leaves out would never reach a reader.
func TestABuildsRoundsCheckedFromSourceReachTheJson(test *testing.T) {
	test.Parallel()

	written, writeError := json.Marshal(contract.BuildRecord{Target: "windows/arm64", CheckedFromSource: 7,
		CheckedFromSourceRounds: []int{3, 4}, PackagesLoaded: 9, HeldAtLoadEndBytes: 64, HeldAtLintPeakBytes: 128,
		LintPeakOverLoadEnd: 2})
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
	if reread.CheckedFromSource != 7 || !slices.Equal(reread.CheckedFromSourceRounds, []int{3, 4}) ||
		reread.HeldAtLoadEndBytes != 64 || reread.HeldAtLintPeakBytes != 128 || reread.LintPeakOverLoadEnd != 2 {
		test.Errorf("the front door wrote %s, want the engine's 7 checked from source over the rounds [3 4], and 64 "+
			"and 128 bytes held, a ratio of 2", forwarded)
	}
}

// Every field the engine's build record writes reaches checkedBuild by its json name, so `--json` drops none of what
// the engine says of a build, and a field added to the record without the front door fails here rather than going
// missing in silence (#k8gty9e).
func TestCheckedBuildCarriesEveryFieldOfTheBuildRecord(test *testing.T) {
	test.Parallel()

	jsonNames := func(of reflect.Type) []string {
		var names []string
		for field := range of.Fields() {
			if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); name != "" && name != "-" {
				names = append(names, name)
			}
		}
		return names
	}
	carried := jsonNames(reflect.TypeFor[checkedBuild]())
	for _, name := range jsonNames(reflect.TypeFor[contract.BuildRecord]()) {
		if !slices.Contains(carried, name) {
			test.Errorf("the build record writes %q, which checkedBuild never reads, so --json drops it", name)
		}
	}
}
