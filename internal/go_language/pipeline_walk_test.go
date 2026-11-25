package go_language

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/project"
)

// The walk-square fixture's packages (#qbvtkfm): s1, whose Inner holds a lock on windows alone, s2, whose Outer holds
// it, h, whose Make returns an Outer, u, which imports h alone and copies the Outer, leaf, whose Size differs under the
// cohere_extra tag, needsleaf, which reads it, other and dormant, which read nothing of the module, lonely, which
// imports nothing, and hasher, which imports hash, so the build lists it.
const (
	walkLonely    = "example.com/walksquare/lonely"
	walkHasher    = "example.com/walksquare/hasher"
	walkS1        = "example.com/walksquare/s1"
	walkS2        = "example.com/walksquare/s2"
	walkH         = "example.com/walksquare/h"
	walkU         = "example.com/walksquare/u"
	walkLeaf      = "example.com/walksquare/leaf"
	walkNeedsLeaf = "example.com/walksquare/needsleaf"
	walkOther     = "example.com/walksquare/other"
	walkDormant   = "example.com/walksquare/dormant"
)

// walkDifferential is the walk's differential, the unit's bar (#154dr43): fixture run four ways, every build checked,
// with the walk, with every package the host kept visited and judged as before it (keepsWalkOff),
// restoring nothing from the host's run (keepsHostRestoreOff), and with the walk and the findings cache on, each over a
// copy of its own. Their streams must be byte for byte the same but for the counts of the restore, of the cache, and of
// an extra build's packages analyzed, which its untouched packages leave out. wantExit is the exit every run makes. It
// returns the walk's pipeline.
func walkDifferential(test *testing.T, fixture string, wantExit int) *pipeline {
	test.Helper()

	walking, walked := runOver(test, copyFixture(test, fixture), wantExit)
	configured := func(configure func(*pipeline)) string {
		root := copyFixture(test, fixture)
		_, raw := runOverConfigured(test, root, wantExit, configure, "--no-cache")
		return normalize(raw, root)
	}
	_, cached := runOverWith(test, copyFixture(test, fixture), wantExit)
	for name, stream := range map[string]string{
		"visiting every package":    configured(func(pipeline *pipeline) { pipeline.keepsWalkOff = true }),
		"checking every package":    configured(func(pipeline *pipeline) { pipeline.keepsHostRestoreOff = true }),
		"walking with the cache on": cached,
	} {
		if withoutCache(stream) != withoutCache(walked) {
			test.Errorf("over %s, %s changed the stream:\nwalking\n%s\n%s\n%s", fixture, name, walked, name, stream)
		}
	}
	return walking
}

// lintBuilds are the build records of stream's lint record, the host's first, failing the test when it has none.
func lintBuilds(test *testing.T, stream string) []contract.BuildRecord {
	test.Helper()
	for _, line := range strings.Split(stream, "\n") {
		if !strings.HasPrefix(line, `{"kind":"lint"`) {
			continue
		}
		var lint contract.LintRecord
		if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
			test.Fatal(decodeError)
		}
		return lint.Builds
	}
	test.Fatalf("the stream wrote no lint record:\n%s", stream)
	return nil
}

// The walk holds the merge of every fixture of the restore from the host's run, and of its own (walkDifferential).
func TestTheWalkHoldsTheMergeOfEveryFixture(test *testing.T) {
	test.Parallel()

	for _, step := range []struct {
		fixture  string
		wantExit int
	}{
		{"walk-square", exitFindings},
		{"walk-reload", exitFindings},
		{"walk-rejudge", exitFindings},
		{"walk-redecline", exitFindings},
		{"restore-from-host", exitFindings},
		{"restore-instance", exitFindings},
		{"restore-bodiless", exitClean},
		{"other-builds", exitFindings},
	} {
		test.Run(step.fixture, func(test *testing.T) {
			test.Parallel()

			walkDifferential(test, filepath.Join("testdata", step.fixture), step.wantExit)
		})
	}
}

// buildRunOf is pipeline's run of the extra build that is or sets operatingSystemOrTag, failing the test when it has
// none.
func buildRunOf(test *testing.T, pipeline *pipeline, operatingSystemOrTag string) buildRun {
	test.Helper()
	at := slices.IndexFunc(pipeline.buildRuns, func(run buildRun) bool {
		return run.build.OperatingSystem == operatingSystemOrTag && !slices.Contains(run.build.Tags, "cohere_extra") ||
			slices.Contains(run.build.Tags, operatingSystemOrTag)
	})
	if at < 1 {
		test.Fatalf("the run checked no %s build beside the host's: %d builds", operatingSystemOrTag,
			len(pipeline.buildRuns))
	}
	return pipeline.buildRuns[at]
}

// copiesALock reports whether run found copylocks' copy of a lock in u's copy.go.
func copiesALock(run buildRun) bool {
	return slices.ContainsFunc(run.result.Findings, func(finding check.Finding) bool {
		return filepath.Base(finding.File) == "copy.go" && strings.HasSuffix(finding.Rule, "copylocks")
	})
}

// The Square's fixture (Square's edge 4 on #154dr43): u imports h alone, and copies the Outer h's Make returns, whose
// s1.Inner holds a lock on windows alone. Nothing u imports directly differs on windows, but u's dependence set holds
// s1 and s2, which the copy of Outer whole reads, so windows visits u, judges it, declines it at its load by Outer, and
// finds the copy of the lock, which the host never finds. Under the walk's superset, a visited package's path joining
// what changed, a walk that went by direct imports would visit u here too, by h, which reads s2: the mutant that
// restores the direct imports is killed at the level of the set, by
// TestADependenceSetHoldsWhatAWholeUseReachesPastTheDirectImports: at cohere-go's, every package between u and what
// changed reads what it exposes, and is visited.
func TestTheSquaresUserIsVisitedAndJudged(test *testing.T) {
	test.Parallel()

	pipeline, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "walk-square")), exitFindings)
	host, windows := pipeline.buildRuns[0], buildRunOf(test, pipeline, "windows")
	if slices.Contains(windows.result.FromHost.Untouched, walkU) {
		test.Errorf("windows left u untouched, %q, want it visited", windows.result.FromHost.Untouched)
	}
	if why := windows.result.FromHost.Declined[walkU]; !strings.HasPrefix(why, "T object "+walkS2+".Outer") {
		test.Errorf("windows declined u for %q, want by s2's Outer, read whole", why)
	}
	if copiesALock(host) || !copiesALock(windows) {
		test.Errorf("want the copy of the lock found by windows alone, got the host %t and windows %t",
			copiesALock(host), copiesALock(windows))
	}
}

// A Closure lookup that found nothing at a package outside the reader's import closure is no read: lonely imports
// nothing, and a rule's Closure lookup of hash.Hash, which every package records, is all it reads of another package,
// so its set is hash as an unfound path and its closure's membership. On windows, where hash's closure reaches what
// windows changed, the walk visits hasher, which imports hash and reads it, and still leaves lonely untouched, as the
// cohere_extra tag does; were the unfound path read as hash's own, windows would visit lonely for an absent package.
func TestAClosureLookupOfAnAbsentPackageReadsNothing(test *testing.T) {
	test.Parallel()

	pipeline, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "walk-square")), exitFindings)
	windows, tagged := buildRunOf(test, pipeline, "windows"), buildRunOf(test, pipeline, "cohere_extra")
	if slices.Contains(windows.result.FromHost.Untouched, walkHasher) {
		test.Fatal("windows left hasher untouched, so nothing of hash changed there and the case proves nothing")
	}
	for _, run := range []buildRun{windows, tagged} {
		if !slices.Contains(run.result.FromHost.Untouched, walkLonely) {
			test.Errorf("%s visited lonely, want it untouched, since it reads no package of hash", run.build)
		}
	}
}

// Under the cohere_extra tag only leaf differs, and only needsleaf reads it: the tag build leaves every other package
// the host kept untouched, visits needsleaf alone, and declines it at its load by leaf's Size, and every extra build's
// record counts what its walk left untouched and visited.
func TestATagBuildLeavesUntouchedAllButTheLeafsReaders(test *testing.T) {
	test.Parallel()

	// The walk judges what each build checks from source, the standard library's packages among them, so every run
	// computes them rather than restore them from the template, as the warm system cache's case below does.
	coldSystemCacheFor(test)
	pipeline, streamed := runOver(test, copyFixture(test, filepath.Join("testdata", "walk-square")), exitFindings)
	tagged := buildRunOf(test, pipeline, "cohere_extra")
	want := []string{walkDormant, walkH, walkHasher, walkLonely, walkOther, walkS1, walkS2, walkU}
	slices.Sort(want)
	// The standard library's packages are left untouched too, nothing of them changing under the tag.
	got := slices.DeleteFunc(slices.Clone(tagged.result.FromHost.Untouched), func(identifier string) bool {
		return !strings.HasPrefix(identifier, "example.com/")
	})
	if !slices.Equal(got, want) {
		test.Errorf("%s left %q untouched, want %q", tagged.build, got, want)
	}
	if tagged.result.FromHost.Visited != 1 || !strings.HasPrefix(tagged.result.FromHost.Declined[walkNeedsLeaf],
		"T object "+walkLeaf+".Size") {
		test.Errorf("%s visited %d and declined needsleaf for %q, want needsleaf alone, by leaf's Size",
			tagged.build, tagged.result.FromHost.Visited, tagged.result.FromHost.Declined[walkNeedsLeaf])
	}
	for index, record := range lintBuilds(test, streamed) {
		run := pipeline.buildRuns[index]
		if index == 0 {
			continue
		}
		if record.Untouched != len(run.result.FromHost.Untouched) || record.Visited != run.result.FromHost.Visited {
			test.Errorf("%s's record counts %d untouched and %d visited, want %d and %d", run.build,
				record.Untouched, record.Visited, len(run.result.FromHost.Untouched), run.result.FromHost.Visited)
		}
	}
}

// warmSystemCacheRun is a `--no-cache` run of every build over a copy of the walk-square fixture whose system cache a
// cold run filled first, so the host restores the standard library from it and keeps no entry of any of it, and each
// extra build hits its own there (#syscache).
func warmSystemCacheRun(test *testing.T) *pipeline {
	test.Helper()
	sharing := sharingSystemCache(test.TempDir())
	runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "walk-square")), exitFindings, sharing,
		"--no-cache")
	warm, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "walk-square")), exitFindings,
		sharing, "--no-cache")
	if restored := warm.buildRuns[0].result.Cache.FromSystemCache; restored == 0 {
		test.Fatal("the warm run's host restored nothing from the system cache, so it kept the standard library and " +
			"the case proves nothing")
	}
	return warm
}

// A standard-library package a build hits in its own system store, whose own inputs are the host's, is no change,
// though the host restored it from its own system cache and so kept no entry of it: with the system cache warm, the
// tag build leaves every package the host kept untouched but needsleaf, as it does cold. Were each such package in D,
// as one the host kept nothing of is, the build would visit every package that reads one, hasher among them, by hash.
func TestATagBuildOverAWarmSystemCacheLeavesUntouchedAllButTheLeafsReaders(test *testing.T) {
	test.Parallel()

	tagged := buildRunOf(test, warmSystemCacheRun(test), "cohere_extra")
	want := []string{walkDormant, walkH, walkHasher, walkLonely, walkOther, walkS1, walkS2, walkU}
	slices.Sort(want)
	if got := tagged.result.FromHost.Untouched; !slices.Equal(got, want) {
		test.Errorf("%s left %q untouched, want %q", tagged.build, got, want)
	}
}

// A standard-library package a build hits in its own system store, whose own inputs differ from the host's, is in D:
// with the system cache warm, windows visits hasher, as it does cold, since hash's closure reaches the packages whose
// files differ on windows. Were each package windows hit taken as unchanged, hasher, which reads nothing else that
// windows changes, would be left untouched and handed the host's findings.
func TestAWarmSystemCacheHitWithOtherInputsThanTheHostsIsAChange(test *testing.T) {
	test.Parallel()

	windows := buildRunOf(test, warmSystemCacheRun(test), "windows")
	if slices.Contains(windows.result.FromHost.Untouched, walkHasher) {
		test.Errorf("windows left hasher untouched, %q, want it visited, hash's closure changed",
			windows.result.FromHost.Untouched)
	}
}

// An untouched package's files are the host's checked files (Square's condition (b) on #154dr43): the tag build has no
// say on dormant, so the host's unused finding of helper there stands, though unused code stands only where every
// build with a say on its file finds it; no build counts dormant's file as compiled and not linted; and the tag build
// carries it among its untouched files.
func TestAnUntouchedPackagesFilesAreTheHostsChecked(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "walk-square"))
	pipeline, streamed := runOver(test, root, exitFindings)
	dormantFile := filepath.Join(root, "dormant", "dormant.go")
	tagged := buildRunOf(test, pipeline, "cohere_extra")
	if !slices.Contains(tagged.result.FromHost.Untouched, walkDormant) || tagged.said[dormantFile] ||
		!tagged.untouchedFiles[dormantFile] {
		test.Errorf("%s left dormant untouched %t, says on its file %t and carries it untouched %t, want "+
			"untouched, no say, carried", tagged.build, slices.Contains(tagged.result.FromHost.Untouched, walkDormant),
			tagged.said[dormantFile], tagged.untouchedFiles[dormantFile])
	}
	if !strings.Contains(streamed, `"file":"/project/dormant/dormant.go","line":9`) {
		test.Errorf("the merge dropped the host's finding of helper in dormant.go:\n%s", streamed)
	}
	for _, record := range lintBuilds(test, streamed) {
		if slices.Contains(record.NotLinted, "/project/dormant/dormant.go") {
			test.Errorf("%s counts dormant.go as compiled and not linted", record.Target)
		}
	}
}

// The walk-reload fixture's packages: platform, whose Logf forwards its format but under the cohere_extra tag, logger,
// which hands it a format, and blank, which imports logger for its side effects alone. No package of it imports hash,
// so blank's Closure lookup of hash.Hash, which every package records, found nothing at a path the build does not list,
// which is no read.
const (
	walkReloadLogger = "example.com/walkreload/logger"
	walkReloadBlank  = "example.com/walkreload/blank"
)

// A package the walk left untouched above one the check declines stays untouched across the reload (#qbvtkfm): under
// the cohere_extra tag, platform changes its Logf, with the host's imports and name, logger is visited and declined at
// its check by the printf fact it read of Logf, and blank, which reads nothing of logger and whose closure's
// membership did not change, is left untouched by the reload's walk too, which only logger's readers' sets meet.
func TestAnUntouchedPackageAboveACheckDeclineStaysUntouched(test *testing.T) {
	test.Parallel()

	pipeline, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "walk-reload")), exitFindings)
	tagged := buildRunOf(test, pipeline, "cohere_extra")
	if !strings.HasPrefix(tagged.result.FromHost.Declined[walkReloadLogger], "F ") ||
		tagged.result.Cache.ReloadRoundsBy["hostDecline"] != 1 {
		test.Fatalf("%s declined logger for %q over %v reloads, want at its check, by its fact, once", tagged.build,
			tagged.result.FromHost.Declined[walkReloadLogger], tagged.result.Cache.ReloadRoundsBy)
	}
	if !slices.Contains(tagged.result.FromHost.Untouched, walkReloadBlank) {
		test.Errorf("%s's last load left %q untouched, want blank among them", tagged.build,
			tagged.result.FromHost.Untouched)
	}
}

// A warm run after a cold one restores what the cold run's walk left untouched from each build's own store, where the
// cold run wrote it through on the walk's induction, and the standard library from the system cache: no build restores
// or leaves untouched anything of the host's run then, none checks a package from source or misses, and the stream is
// a run's under --no-cache but for the counts of the cache and the restore.
func TestAWarmRunRestoresTheUntouchedFromItsOwnStore(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "walk-square"))
	// One system cache for both runs, as a person's: the standard library each build computed cold is kept there, and
	// never in the build's own store.
	sharing := sharingSystemCache(test.TempDir())
	cold, _ := runOverConfigured(test, root, exitFindings, sharing)
	if untouched := buildRunOf(test, cold, "cohere_extra").result.FromHost.Untouched; len(untouched) == 0 {
		test.Fatal("the cold run's tag build left nothing untouched, so nothing tells the warm run's restore apart")
	}
	warm, warmRaw := runOverConfigured(test, root, exitFindings, sharing)
	warmStream := normalize(warmRaw, root)
	for _, run := range warm.buildRuns[1:] {
		fromHost, counts := run.result.FromHost, run.result.Cache
		if len(fromHost.Untouched)+len(fromHost.Restored) != 0 || run.checkedFromSource != 0 || counts.Missed != 0 {
			test.Errorf("%s's warm run left %d untouched, restored %d from the host, checked %d from source and "+
				"missed %d, want every package hit in its own store", run.build, len(fromHost.Untouched),
				len(fromHost.Restored), run.checkedFromSource, counts.Missed)
		}
	}
	_, unstored := runOver(test, copyFixture(test, filepath.Join("testdata", "walk-square")), exitFindings)
	if withoutCache(warmStream) != withoutCache(unstored) {
		test.Errorf("the warm run's stream differs from --no-cache's:\nwarm\n%s\nno cache\n%s", warmStream, unstored)
	}
}

// The re-judge fixtures' packages (#axbw09w): walk-rejudge's logger, whose printf fact of Logf windows declines, and
// reader, which reads only logger's Line; walk-redecline's opener, whose answer of Open windows declines, and user,
// which reads the answer of opener's Get.
const (
	rejudgeLogger   = "example.com/walkrejudge/logger"
	rejudgeReader   = "example.com/walkrejudge/reader"
	redeclineOpener = "example.com/walkredecline/opener"
	redeclineUser   = "example.com/walkredecline/user"

	// The third load's test adds them to its copy: platform's Size, which windows declares otherwise, and sizer,
	// which reads it.
	redeclinePlatform = "example.com/walkredecline/platform"
	redeclineSizer    = "example.com/walkredecline/sizer"
)

// A check-time decline re-judges what is above it rather than excluding it (#axbw09w): windows declines logger at its
// check by the printf fact it read of Logf, and loads again with logger checked from source. reader, above it, reads
// only Line, whose own line windows declares alike, so the reload judges it again against logger as checked there,
// and restores it from the host's run.
func TestACheckDeclinesImporterIsRejudgedAndRestored(test *testing.T) {
	test.Parallel()

	pipeline, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "walk-rejudge")), exitFindings)
	windows := buildRunOf(test, pipeline, "windows")
	fromHost := windows.result.FromHost
	if !strings.HasPrefix(fromHost.Declined[rejudgeLogger], "F ") ||
		windows.result.Cache.ReloadRoundsBy["hostDecline"] != 1 {
		test.Fatalf("windows declined logger for %q over %v reloads, want at its check, by its fact, once",
			fromHost.Declined[rejudgeLogger], windows.result.Cache.ReloadRoundsBy)
	}
	if !slices.Contains(fromHost.Restored, rejudgeReader) {
		test.Errorf("windows' reload restored %q and left %q untouched, want reader restored on its re-judgment",
			fromHost.Restored, fromHost.Untouched)
	}
}

// A package re-judged above a check-time decline declines where the declined package's answer it read changed
// (#axbw09w), and a second round of such declines turns the restore from the host off, as before: windows declines
// opener at its check by the answer of Open it read; in the reload, with opener checked from source, user's read of
// Get's answer, which windows answers otherwise, does not hold, so user is declined too; and the third load restores
// nothing from the host, checking again, which reads user's Again, from source with user, where a restore left on
// would decline it in a third round. Windows finds no comparison always true in user or again.
func TestARejudgedImporterOfAChangedAnswerDeclines(test *testing.T) {
	test.Parallel()

	pipeline, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "walk-redecline")), exitFindings)
	host, windows := pipeline.buildRuns[0], buildRunOf(test, pipeline, "windows")
	fromHost := windows.result.FromHost
	if !strings.HasPrefix(fromHost.Declined[redeclineOpener], "A ") ||
		!strings.HasPrefix(fromHost.Declined[redeclineUser], "A ") {
		test.Errorf(
			"windows declined opener for %q and user for %q, want both by an answer",
			fromHost.Declined[redeclineOpener],
			fromHost.Declined[redeclineUser],
		)
	}
	if rounds := windows.result.Cache.ReloadRoundsBy["hostDecline"]; rounds != 2 || len(fromHost.Restored) != 0 {
		test.Errorf("windows loaded again %d times for declines and restored %q last, want twice and nothing, the "+
			"restore turned off", rounds, fromHost.Restored)
	}
	for _, file := range []string{"user.go", "again.go"} {
		if !foundIn(host, file) || foundIn(windows, file) {
			test.Errorf("want the comparison in %s found by the host alone, got the host %t and windows %t", file,
				foundIn(host, file), foundIn(windows, file))
		}
	}
}

// A load the restore from the host is off for reports its own counts, and none of the load before it (#78tynz4):
// windows declines opener at its check, then user in the reload, which turns the restore off, so its third load
// restores nothing from the host. In the copy, sizer reads platform's Size, another on windows, so the first load and
// the reload each decline it at their load. The third load visits, leaves untouched, restores and declines at its load
// nothing, and what is declined is check's alone, opener and user by their answers, kept over every load. A load that
// returned before clearing the last load's counts would report the reload's visits, and its load declines and their
// reasons, sizer's, as its own. The reload's decline of sizer is read as it happens (pipeline.declinedAtLoad), so a
// copy in which it stopped proves nothing and fails as such.
func TestAThirdLoadReportsItsOwnHostRestoreCounts(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "walk-redecline"))
	for name, contents := range map[string]string{
		"platform/size_windows.go": "package platform\n\n// Size is a size, another on windows.\nconst Size = 2\n",
		"platform/size_other.go":   "//go:build !windows\n\npackage platform\n\n// Size is a size.\nconst Size = 1\n",
		"sizer/sizer.go": "// Package sizer reads platform's Size.\npackage sizer\n\n" +
			"import \"example.com/walkredecline/platform\"\n\n// Twice is twice platform's Size.\n" +
			"func Twice() int { return platform.Size * 2 }\n",
	} {
		if makeError := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	// What each of windows' loads declined, by ID, the first load's first, told from the build's goroutine.
	var telling sync.Mutex
	var windowsDeclines []map[string]string
	isWindows := func(extra project.Build) bool {
		return extra.OperatingSystem == "windows" && !slices.Contains(extra.Tags, "cohere_extra")
	}
	pipeline, _ := runOverConfigured(test, root, exitFindings, func(pipeline *pipeline) {
		pipeline.extraBuildLoads = func(extra project.Build) {
			if isWindows(extra) {
				telling.Lock()
				defer telling.Unlock()
				windowsDeclines = append(windowsDeclines, map[string]string{})
			}
		}
		pipeline.declinedAtLoad = func(extra project.Build, identifier string, why string) {
			if isWindows(extra) {
				telling.Lock()
				defer telling.Unlock()
				windowsDeclines[len(windowsDeclines)-1][identifier] = why
			}
		}
	}, "--no-cache")
	windows := buildRunOf(test, pipeline, "windows")
	fromHost := windows.result.FromHost
	if fromHost.ReloadRounds != 2 || windows.result.Cache.ReloadRoundsBy["hostDecline"] != 2 {
		test.Fatalf("windows' check declines asked %d loads and the build loaded again %v, want two of each, or the "+
			"third load never ran with the restore off and the case proves nothing", fromHost.ReloadRounds,
			windows.result.Cache.ReloadRoundsBy)
	}
	if len(windowsDeclines) != 3 || !strings.HasPrefix(windowsDeclines[1][redeclineSizer], "T object "+
		redeclinePlatform+".Size") {
		test.Fatalf("windows' loads declined %v at their load, want three loads, the second declining sizer by "+
			"platform's Size, or the third load has no load decline of the second's to keep and the case proves "+
			"nothing", windowsDeclines)
	}
	if fromHost.Visited != 0 || len(fromHost.Untouched) != 0 || len(fromHost.Restored) != 0 {
		test.Errorf("windows' third load visited %d, left %q untouched and restored %q, want nothing, the restore off",
			fromHost.Visited, fromHost.Untouched, fromHost.Restored)
	}
	// What the load declined is the load's alone: its count, and its reasons among Declined, which holds check's too.
	declined := slices.Sorted(maps.Keys(fromHost.Declined))
	if want := []string{redeclineOpener, redeclineUser}; fromHost.DeclinedAtLoad != 0 ||
		!slices.Equal(declined, want) {
		test.Errorf("windows' third load declined %d at its load, and %q in all, want none at its load and %q, "+
			"check's alone", fromHost.DeclinedAtLoad, declined, want)
	}
	if fromHost.DeclinedAtCheck != 2 || !maps.Equal(fromHost.DeclinedBy, map[string]int{"answers": 2}) {
		test.Errorf("windows declined %d at check, %v by reason, want opener and user by their answers alone",
			fromHost.DeclinedAtCheck, fromHost.DeclinedBy)
	}
}
