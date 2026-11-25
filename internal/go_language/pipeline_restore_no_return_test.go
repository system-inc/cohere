package go_language

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check"
)

// The restore-no-return fixture's packages (#mtrjfyn): m, whose Finish ends the process on windows alone through a
// function each platform's file declares, and r, the same in every build, which calls Finish before a value it
// assigns twice.
const (
	noReturnFinisher = "example.com/restorenoreturn/m"
	noReturnCaller   = "example.com/restorenoreturn/r"
)

// unreadValueInCaller reports whether run found SA4006's value never read in r's r.go.
func unreadValueInCaller(run buildRun) bool {
	return slices.ContainsFunc(run.result.Findings, func(finding check.Finding) bool {
		return filepath.Base(finding.File) == "r.go" && strings.HasSuffix(finding.Rule, "SA4006")
	})
}

// A package windows restores from the host's run whose import closure reaches one it checks from source is asked
// its facts there (#mtrjfyn): r's declarations and files are the host's on windows, so no must-check set holds it,
// but m, which windows checks from source, stops returning in Finish there, so the no-return fact r read of Finish
// differs. Windows declines r at check and checks it from source, where the code after the call is unreachable and
// SA4006 finds nothing, so the merged stream, which keeps SA4006 only where every build reports it, holds no finding
// in r: exactly what every build checked from source gives (walkDifferential). Restored with no decide, or with its
// fact reads never asked, r would keep the host's finding on windows, and the stream would hold it.
func TestARestoredCallerOfACalleeThatStopsOnWindowsIsAskedItsFacts(test *testing.T) {
	test.Parallel()

	fixture := filepath.Join("testdata", "restore-no-return")
	pipeline := walkDifferential(test, fixture, exitClean)
	host, windows := pipeline.buildRuns[0], buildRunOf(test, pipeline, "windows")
	if !unreadValueInCaller(host) || unreadValueInCaller(windows) {
		test.Errorf("want SA4006 in r found by the host alone, got the host %t and windows %t",
			unreadValueInCaller(host), unreadValueInCaller(windows))
	}
	if why := windows.result.FromHost.Declined[noReturnCaller]; !strings.HasPrefix(why, "F ") {
		test.Errorf("windows declined r for %q, want by the fact it read of %s's Finish", why, noReturnFinisher)
	}
	_, stream := runOver(test, copyFixture(test, fixture), exitClean)
	for _, line := range findingLines(stream) {
		if strings.Contains(line, "SA4006") {
			test.Errorf("the merged stream holds SA4006 where windows never reports it: %s", line)
		}
	}
}
