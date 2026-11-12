package go_engine

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
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
	if rounds := windows.result.Cache; rounds.ReloadRounds != 1 ||
		!maps.Equal(rounds.ReloadRoundsBy, map[string]int{"hostDecline": 1}) {
		test.Errorf("windows was loaded again %d times, by %v, want once, for its host declines",
			rounds.ReloadRounds, rounds.ReloadRoundsBy)
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

// A build loaded again after a check-time decline counts what each load checked from source, and its record sums
// them (#k8gty9e): over the fixture whose windows build declines two packages at its check and reloads once, the
// windows record names both rounds, the reload's checking the declined packages from source, and its
// checkedFromSource is their sum, not the first load's alone.
func TestAReloadsChecksFromSourceReachTheRecord(test *testing.T) {
	test.Parallel()

	// The stream as written, since normalize blanks each build's target.
	_, stream := runOverRaw(test, copyFixture(test, filepath.Join("testdata", "restore-from-host")), exitFindings,
		"--no-cache", "--other-builds")
	var lint contract.LintRecord
	for line := range strings.SplitSeq(stream, "\n") {
		if strings.HasPrefix(line, `{"kind":"lint"`) {
			if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
				test.Fatal(decodeError)
			}
		}
	}
	index := slices.IndexFunc(lint.Builds, func(build contract.BuildRecord) bool {
		return strings.HasPrefix(build.Target, "windows/")
	})
	if index < 0 {
		test.Fatalf("the lint record names no windows build:\n%s", stream)
	}
	windows := lint.Builds[index]
	if len(windows.CheckedFromSourceRounds) != 2 || windows.CheckedFromSourceRounds[1] == 0 {
		test.Fatalf("windows records the rounds %v checked from source, want two, the reload's above zero",
			windows.CheckedFromSourceRounds)
	}
	sum := windows.CheckedFromSourceRounds[0] + windows.CheckedFromSourceRounds[1]
	if windows.CheckedFromSource != sum {
		test.Errorf("windows records %d checked from source, want %d, the sum of its rounds %v",
			windows.CheckedFromSource, sum, windows.CheckedFromSourceRounds)
	}
	for _, build := range lint.Builds {
		if build.Target != windows.Target && len(build.CheckedFromSourceRounds) != 0 {
			test.Errorf("%s, loaded once, records the rounds %v, want none", build.Target,
				build.CheckedFromSourceRounds)
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

// The restore-instance fixture's package instantiating another package's generic with a type argument it inferred.
const restoreInferrer = "example.com/restoreinstance/inferrer"

// An extra build declines a package whose instance of another package's generic has a type argument it inferred and
// never named, when that type's definition differs in the build: inferrer passes platform.Get's Handle through
// generic.Identity, and only windows' Handle holds a lock, so only windows finds copylocks' copy of it. Get itself is
// read as a member, by its own line, which every build writes alike; the copy of its result into handle, and the
// instance's type argument, are uses of Handle whole (#mjdf7ah), so windows declines inferrer at its load by Handle,
// finds the copy, and the merge is the one a run checking every package itself writes (#b2t9xmf).
func TestAnExtraBuildDeclinesAnInstanceWhoseInferredTypeArgumentDiffers(test *testing.T) {
	test.Parallel()

	fixture := filepath.Join("testdata", "restore-instance")
	offRoot := copyFixture(test, fixture)
	_, offRaw := runOverConfigured(test, offRoot, exitFindings,
		func(engine *pipeline) { engine.keepsHostRestoreOff = true }, "--other-builds", "--no-cache")
	off := normalize(offRaw, offRoot)
	engine, on := runOver(test, copyFixture(test, fixture), exitFindings, "--other-builds")
	host, windows := engine.buildRuns[0], windowsRun(test, engine)
	if why := windows.result.FromHost.Declined[restoreInferrer]; !strings.HasPrefix(why,
		"T object example.com/restoreinstance/platform.Handle renders differently") {
		test.Errorf("windows declined %s for %q, want by Handle, the type argument inferred and copied whole",
			restoreInferrer, why)
	}
	if foundIn(host, "inferrer.go") || !foundIn(windows, "inferrer.go") {
		test.Errorf("want the copy of Handle found by windows alone, got the host %t and windows %t",
			foundIn(host, "inferrer.go"), foundIn(windows, "inferrer.go"))
	}
	if withoutHostRestore(on) != withoutHostRestore(off) {
		test.Errorf("restoring from the host changed the stream:\nrestoring\n%s\nchecking every package\n%s", on, off)
	}
}

// The restore-bodiless fixture's package declaring a function without a body, which every build declares alike.
const restoreIntrinsic = "example.com/restorebodiless/intrinsic"

// An extra build answers a question of a function declared without a body, in a package it restored from the host's
// run, from the function's types, since the host recorded which of the package's functions have no body, where it
// once loaded the build again to tell such a function from one whose body it never loaded (#b2t9xmf). Only windows
// compiles caller's call of doubled, whose purity discarded_pure_call asks, and judging doubled asks whether
// intrinsic.Raw, which has no body, is pure: the host never asked of Raw, and windows restores intrinsic from it and is
// loaded once, and the merge is the one a run checking every package itself writes. Without the record, windows loads
// again for Raw, one round. With the findings cache on, the host's entry holds intrinsic computed whole, which answers
// Raw with no record, and the run must still merge alike.
func TestAnExtraBuildAnswersABodilessFunctionWithoutLoadingAgain(test *testing.T) {
	test.Parallel()

	fixture := filepath.Join("testdata", "restore-bodiless")
	offRoot := copyFixture(test, fixture)
	_, offRaw := runOverConfigured(test, offRoot, exitClean,
		func(engine *pipeline) { engine.keepsHostRestoreOff = true }, "--other-builds", "--no-cache")
	off := normalize(offRaw, offRoot)
	for name, run := range map[string]func() (*pipeline, string){
		"without the findings cache": func() (*pipeline, string) {
			return runOver(test, copyFixture(test, fixture), exitClean, "--other-builds")
		},
		"with the findings cache": func() (*pipeline, string) {
			return runOverWith(test, copyFixture(test, fixture), exitClean, "--other-builds")
		},
	} {
		engine, on := run()
		windows := windowsRun(test, engine)
		if !slices.Contains(windows.result.FromHost.Restored, restoreIntrinsic) {
			test.Errorf("%s, windows restored %q from the host, want %s among them", name,
				windows.result.FromHost.Restored, restoreIntrinsic)
		}
		if rounds := windows.result.Cache; rounds.ReloadRounds != 0 {
			test.Errorf("%s, windows was loaded again %d times, by %v, want never", name, rounds.ReloadRounds,
				rounds.ReloadRoundsBy)
		}
		if withoutCache(on) != withoutCache(off) {
			test.Errorf("%s, answering Raw from its types changed the stream:\nrestoring\n%s\nchecking every "+
				"package\n%s", name, on, off)
		}
	}
}

// Each build says where its time went: over the restore fixture, the host's load, lint and keeping its packages for
// the others, and windows' load, lint and deciding which packages restore, each measured, and each written into the
// build's record as its milliseconds.
func TestEachBuildsRecordSaysWhereItsTimeWent(test *testing.T) {
	test.Parallel()

	engine, stream := runOverRaw(test, copyFixture(test, filepath.Join("testdata", "restore-from-host")),
		exitFindings, "--no-cache", "--other-builds")
	host, windows := engine.buildRuns[0], windowsRun(test, engine)
	for name, measured := range map[string]time.Duration{
		"the host's load":    host.loading,
		"the host's lint":    host.linting,
		"the host's keeping": host.result.Host.Keeping(),
		"windows' load":      windows.loading,
		"windows' lint":      windows.linting,
		"windows' deciding":  windows.result.FromHost.Deciding,
	} {
		if measured <= 0 {
			test.Errorf("%s took %s, want it measured", name, measured)
		}
	}
	var builds []contract.BuildRecord
	for line := range strings.SplitSeq(stream, "\n") {
		var record contract.LintRecord
		if !strings.HasPrefix(line, `{"kind":"lint"`) || json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		builds = record.Builds
	}
	if len(builds) != len(engine.buildRuns) {
		test.Fatalf("the lint record holds %d builds, want %d", len(builds), len(engine.buildRuns))
	}
	for index, run := range engine.buildRuns {
		var want contract.BuildRecord
		recordTimes(&want, run)
		got := builds[index]
		if got.LoadMilliseconds != want.LoadMilliseconds || got.LintMilliseconds != want.LintMilliseconds ||
			got.DecideMilliseconds != want.DecideMilliseconds || got.KeepMilliseconds != want.KeepMilliseconds ||
			got.WaitMilliseconds != want.WaitMilliseconds {
			test.Errorf("build %d's record says %+v, want the times %+v", index, got, want)
		}
		if got.LoadMilliseconds == 0 || got.LintMilliseconds == 0 {
			test.Errorf("build %d's record says it loaded in %d ms and linted in %d ms, want both written", index,
				got.LoadMilliseconds, got.LintMilliseconds)
		}
	}
}

// recordTimes writes each of a build's times into its record, the result's deciding and keeping included, so a time
// measured is never left out of the record.
func TestEveryTimeOfABuildIsRecorded(test *testing.T) {
	test.Parallel()

	var record contract.BuildRecord
	recordTimes(&record, buildRun{loading: 4 * time.Millisecond, linting: 5 * time.Millisecond,
		waited: 7 * time.Millisecond,
		result: &check.Result{FromHost: check.HostRestoreCounts{Deciding: 6 * time.Millisecond}}})
	if record.LoadMilliseconds != 4 || record.LintMilliseconds != 5 || record.DecideMilliseconds != 6 ||
		record.WaitMilliseconds != 7 {
		test.Errorf("want 4, 5, 6 and 7 ms recorded, got %+v", record)
	}
}
