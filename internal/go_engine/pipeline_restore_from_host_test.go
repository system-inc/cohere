package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
)

// The restore-from-host fixture's packages: platform, whose files differ by build as os's do, and four packages
// importing it, each reading it one way.
const (
	restoreReader = "example.com/restore/reader"
	restoreSizer  = "example.com/restore/sizer"
	restoreOpener = "example.com/restore/opener"
	restoreLogger = "example.com/restore/logger"
)

// windowsRun is engine's run of the windows build, failing the test when it has none.
func windowsRun(test *testing.T, engine *pipeline) buildRun {
	test.Helper()
	at := slices.IndexFunc(engine.buildRuns, func(run buildRun) bool { return run.build.OperatingSystem == "windows" })
	if at < 1 {
		test.Fatalf("the run checked no windows build beside the host's: %d builds", len(engine.buildRuns))
	}
	return engine.buildRuns[at]
}

// foundIn reports whether run found anything in file.
func foundIn(run buildRun, file string) bool {
	return slices.ContainsFunc(run.result.Findings, func(finding check.Finding) bool {
		return filepath.Base(finding.File) == file
	})
}

// An extra build restores from the host's run a package whose own inputs are the host's and whose reads hold there,
// and checks every other itself (#b2t9xmf). Over a platform package whose files differ by build, windows restores
// reader, which reads only a variable every build declares alike, and declines the rest: its load declines sizer,
// whose type Shared has a method of another signature on windows, and its check declines logger, whose Logf forwards
// its format on the host and not on windows, which the printf wrapper fact it read says, and opener, whose Open never
// returns a nil interface on the host and returns nil on windows, which the answer of its nilness it read says. Those
// two are loaded again from source, once, and windows finds neither the printf finding nor SA4023's the host finds in
// them, which restoring them would have brought.
func TestAnExtraBuildRestoresWhatReadsTheSameAndChecksTheRest(test *testing.T) {
	test.Parallel()

	engine, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "restore-from-host")), exitFindings,
		"--other-builds")
	host, windows := engine.buildRuns[0], windowsRun(test, engine)
	fromHost := windows.result.FromHost
	if !slices.Equal(fromHost.Restored, []string{restoreReader}) {
		test.Errorf("windows restored %q from the host, want %s alone", fromHost.Restored, restoreReader)
	}
	for identifier, want := range map[string]string{restoreSizer: "T ", restoreLogger: "F ", restoreOpener: "A "} {
		if why := fromHost.Declined[identifier]; !strings.HasPrefix(why, want) {
			test.Errorf("windows declined %s for %q, want a reason of row %q", identifier, why, strings.TrimSpace(want))
		}
	}
	if fromHost.DeclinedAtLoad != 1 || fromHost.DeclinedAtCheck != 2 || fromHost.ReloadRounds != 1 {
		test.Errorf("windows declined %d at its load and %d at its check over %d reloads, want 1, 2 and 1",
			fromHost.DeclinedAtLoad, fromHost.DeclinedAtCheck, fromHost.ReloadRounds)
	}
	for _, file := range []string{"logger.go", "opener.go"} {
		if !foundIn(host, file) {
			test.Errorf("the host's build found nothing in %s, which the fixture plants a finding in", file)
		}
		if foundIn(windows, file) {
			test.Errorf("windows found the host's finding in %s, whose callee differs on windows", file)
		}
	}
}

// The merged verdict is the same whether the extra builds restore from the host's run or check every package
// themselves, byte for byte but for each build's counts of the restore, without the findings cache and with it.
func TestTheMergeIsTheSameWhetherTheBuildsRestoreFromTheHostOrNot(test *testing.T) {
	test.Parallel()

	fixture := filepath.Join("testdata", "restore-from-host")
	restoringNothing := func(engine *pipeline) { engine.keepsHostRestoreOff = true }
	offRoot := copyFixture(test, fixture)
	_, offRaw := runOverConfigured(test, offRoot, exitFindings, restoringNothing, "--other-builds", "--no-cache")
	off := normalize(offRaw, offRoot)
	_, on := runOver(test, copyFixture(test, fixture), exitFindings, "--other-builds")
	if withoutHostRestore(on) != withoutHostRestore(off) {
		test.Errorf("restoring from the host changed the stream:\nrestoring\n%s\nchecking every package\n%s", on, off)
	}
	if on == off {
		test.Error("the run restoring from the host wrote no count of it, so nothing tells the two runs apart")
	}
	_, cached := runOverWith(test, copyFixture(test, fixture), exitFindings, "--other-builds")
	if withoutCache(cached) != withoutCache(off) {
		test.Errorf("restoring from the host with the findings cache on changed the stream:\ncached\n%s\nchecking "+
			"every package\n%s", cached, off)
	}
}
