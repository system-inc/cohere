package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A Go run's --json summary forwards the live heap the extra builds' width was decided from, beside buildsAtOnce, so
// a reader sees why the width was chosen without running the engine alone (#qwb6ns8), and writes none when the lint
// record carries none.
// Not parallel: acceptLint sets the package-level activeSummary.
func TestASummaryForwardsTheHeapTheBuildsWidthWasDecidedFrom(test *testing.T) {
	saved := activeSummary
	defer func() { activeSummary = saved }()
	summaryOf := func(lint string) string {
		test.Helper()
		activeSummary = runSummary{}
		var out bytes.Buffer
		run := newEngineRun(bufio.NewWriter(&out), engineModeCheck, goLanguage("module"), "", time.Now())
		var record engineLintRecord
		if decodeError := json.Unmarshal([]byte(lint), &record); decodeError != nil {
			test.Fatal(decodeError)
		}
		if acceptError := run.acceptLint(&record); acceptError != nil {
			test.Fatal(acceptError)
		}
		encoded, encodeError := json.Marshal(summaryAsJson(activeSummary))
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		return string(encoded)
	}
	builds := `"builds":[{"target":"darwin/arm64 cgo","host":true},{"target":"linux/arm64"},{"target":"windows/arm64"}]`
	decided := summaryOf(`{"kind":"lint","findings":0,"rulesRun":5,` + builds +
		`,"buildsAtOnce":2,"buildsWidthLiveBytes":2147483648,"offReasons":{}}`)
	if !strings.Contains(decided, `"buildsAtOnce":2,"buildsWidthLiveBytes":2147483648`) {
		test.Errorf("the summary of a width decided from the heap lost the heap: %s", decided)
	}
	undecided := summaryOf(`{"kind":"lint","findings":0,"rulesRun":5,` + builds + `,"buildsAtOnce":1,"offReasons":{}}`)
	if strings.Contains(undecided, "buildsWidthLiveBytes") {
		test.Errorf("the summary of a width that needed no heap names one: %s", undecided)
	}
}
