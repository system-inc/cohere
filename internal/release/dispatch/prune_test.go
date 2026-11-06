package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// A prune is judged by the exact set it removes. Each test seeds a cache with every kind of file the
// real one holds and asserts that set name by name, because "it removed some files" is the result a
// prune that deletes the wrong ones also produces.

var pruneNow = time.Date(2026, time.October, 2, 12, 0, 0, 0, time.UTC)

func TestPruneRemovesExactlyWhatNoRunCanReach(test *testing.T) {
	test.Parallel()

	paths, ages := seedPruneCache(test)

	plan, planPruneError := PlanPrune(paths, []string{"cohere-" + platformTag() + "-keepme0000000000"}, "", pruneNow)
	if planPruneError != nil {
		test.Fatal(planPruneError)
	}
	if plan.Examined != len(ages)+1 {
		test.Fatalf("examined %d entries, the cache holds %d", plan.Examined, len(ages)+1)
	}

	got := []string{}
	for _, file := range plan.Remove {
		got = append(got, file.Name)
	}
	want := []string{
		// Twelve old cohere binaries, the newest eight kept. The keep-listed one is older than all of
		// them and still survives, so the four removed are the four oldest of the rest.
		"cohere-" + platformTag() + "-old00",
		"cohere-" + platformTag() + "-old01",
		"cohere-" + platformTag() + "-old02",
		"cohere-" + platformTag() + "-old03",
		// Ten old Swift engines, the newest eight kept.
		"cohere-swift-" + platformTag() + "-old00",
		"cohere-swift-" + platformTag() + "-old01",
		// A partial whose writer is long gone.
		"cohere-" + platformTag() + "-stale.partial-123",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		test.Fatalf("would remove\n  %s\nexpected\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}

	wantUnrecognised := []string{"a-subdirectory", "something-else"}
	if !reflect.DeepEqual(plan.Unrecognised, wantUnrecognised) {
		test.Fatalf("unrecognised %v, expected %v kept and reported", plan.Unrecognised, wantUnrecognised)
	}
}

func TestApplyPruneRemovesOnlyThePlanAndLeavesFrozenACandidate(test *testing.T) {
	test.Parallel()

	paths, ages := seedPruneCache(test)
	plan, planPruneError := PlanPrune(paths, nil, "", pruneNow)
	if planPruneError != nil {
		test.Fatal(planPruneError)
	}

	removed, applyPruneError := ApplyPrune(paths, plan)
	if applyPruneError != nil {
		test.Fatal(applyPruneError)
	}
	if len(removed) != len(plan.Remove) {
		test.Fatalf("removed %d of the %d planned", len(removed), len(plan.Remove))
	}
	for name := range ages {
		_, statError := os.Stat(filepath.Join(paths.BinaryDirectory(), name))
		planned := false
		for _, file := range plan.Remove {
			planned = planned || file.Name == name
		}
		if planned != os.IsNotExist(statError) {
			test.Errorf(
				"%s: planned=%v, but on disk the file %s",
				name,
				planned,
				map[bool]string{true: "is gone", false: "is still there"}[os.IsNotExist(statError)],
			)
		}
	}

	// A second apply of the same plan finds every file already gone, which is a concurrent prune, not
	// an error.
	if again, applyPruneError := ApplyPrune(paths, plan); applyPruneError != nil || len(again) != 0 {
		test.Fatalf("re-applying an applied plan: removed %d, err %v", len(again), applyPruneError)
	}

	if _, resolveFrozenError := ResolveFrozen(paths); resolveFrozenError != nil {
		test.Fatalf("after a prune --frozen has nothing to run: %v", resolveFrozenError)
	}
}

func TestApplyPruneRefusesANameThatIsAPath(test *testing.T) {
	test.Parallel()

	paths, _ := seedPruneCache(test)
	outside := filepath.Join(paths.CacheDirectory, "outside")
	writeFile(test, outside, "not in the binary cache\n")

	_, applyPruneError := ApplyPrune(paths, PrunePlan{Remove: []PrunedFile{{Name: "../outside"}}})
	if applyPruneError == nil {
		test.Fatal("a name that walks out of the binary cache was accepted")
	}
	if _, statError := os.Stat(outside); statError != nil {
		test.Fatalf("the file outside the binary cache was removed: %v", statError)
	}
}

// Compiler extractions and leftover snapshots are judged by the same rule as binaries: the current one
// and anything used within the hour stay, the rest go. Ages are real (relative to now) rather than to
// pruneNow, because ApplyPrune asks the clock again at the moment of removal.
func TestPruneRemovesStaleExtractionsAndKeepsWhatABuildCanReach(test *testing.T) {
	test.Parallel()

	paths, _ := seedPruneCache(test)
	now := time.Now()
	directory := func(parent string, name string, age time.Duration) string {
		path := filepath.Join(parent, name)
		writeFile(test, filepath.Join(path, "tsc", "go.mod"), "module "+name+"\n")
		stamp := now.Add(-age)
		if changeTimesError := os.Chtimes(path, stamp, stamp); changeTimesError != nil {
			test.Fatal(changeTimesError)
		}
		return path
	}
	// Older than everything, and kept because the build just made reads it.
	current := directory(paths.CompilerDirectory(), "currentcurrentcurrentcurrentcurrentcurren", 900*time.Hour)
	// Another pin, read by a build within the hour.
	inUse := directory(paths.CompilerDirectory(), "inuseinuseinuseinuseinuseinuseinuseinuse", 10*time.Minute)
	stale := directory(paths.CompilerDirectory(), "stalestalestalestalestalestalestalestale", 5*time.Hour)
	abandoned := directory(paths.CompilerDirectory(), "stalestalestalestalestalestalestalestale.partial-1", 5*time.Hour)
	extracting := directory(
		paths.CompilerDirectory(),
		"freshfreshfreshfreshfreshfreshfreshfresh.partial-2",
		time.Minute,
	)
	killed := directory(paths.SnapshotDirectory(), "58cd5d457ebf-1999002027", 3*time.Hour)
	building := directory(paths.SnapshotDirectory(), "58cd5d457ebf-1130357668", 2*time.Minute)

	plan, planPruneError := PlanPrune(paths, nil, current, now)
	if planPruneError != nil {
		test.Fatal(planPruneError)
	}
	names := func(list []PrunedFile) []string {
		out := []string{}
		for _, entry := range list {
			if entry.Bytes == 0 {
				test.Errorf("%s planned with no size, so the log would understate what was reclaimed", entry.Name)
			}
			out = append(out, entry.Name)
		}
		sort.Strings(out)
		return out
	}
	wantCompilers := []string{filepath.Base(stale), filepath.Base(abandoned)}
	sort.Strings(wantCompilers)
	if got := names(plan.StaleCompilers); !reflect.DeepEqual(got, wantCompilers) {
		test.Fatalf("stale compilers %v, expected %v", got, wantCompilers)
	}
	if got := names(plan.StaleSnapshots); !reflect.DeepEqual(got, []string{filepath.Base(killed)}) {
		test.Fatalf("stale snapshots %v, expected only %s", got, filepath.Base(killed))
	}

	if _, applyPruneError := ApplyPrune(paths, plan); applyPruneError != nil {
		test.Fatal(applyPruneError)
	}
	for _, path := range []string{stale, abandoned, killed} {
		if _, statError := os.Stat(path); !os.IsNotExist(statError) {
			test.Errorf("%s survived the prune: %v", path, statError)
		}
	}
	for _, path := range []string{current, inUse, extracting, building} {
		if _, statError := os.Stat(filepath.Join(path, "tsc", "go.mod")); statError != nil {
			test.Errorf("%s was removed, and a build can still reach it: %v", path, statError)
		}
	}

	// Without the current compiler named, nothing says which one a build reads, so none is planned.
	if unknown, planPruneError := PlanPrune(
		paths,
		nil,
		"",
		now,
	); planPruneError != nil || len(unknown.StaleCompilers) != 0 {
		test.Fatalf("with no current compiler, planned %v (err %v)", unknown.StaleCompilers, planPruneError)
	}
}

// A build that touches an extraction between the plan and its removal keeps it. The plan is made, the
// extraction is marked used the way ensureCompiler marks it, and the apply must leave it.
func TestApplyPruneKeepsAnExtractionTouchedSinceThePlan(test *testing.T) {
	test.Parallel()

	paths, _ := seedPruneCache(test)
	now := time.Now()
	current := filepath.Join(paths.CompilerDirectory(), "current")
	writeFile(test, filepath.Join(current, "tsc", "go.mod"), "module current\n")
	raced := filepath.Join(paths.CompilerDirectory(), "raced")
	writeFile(test, filepath.Join(raced, "tsc", "go.mod"), "module raced\n")
	old := now.Add(-5 * time.Hour)
	if changeTimesError := os.Chtimes(raced, old, old); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}

	plan, planPruneError := PlanPrune(paths, nil, current, now)
	if planPruneError != nil {
		test.Fatal(planPruneError)
	}
	if len(plan.StaleCompilers) != 1 {
		test.Fatalf("expected the raced extraction planned, got %v", plan.StaleCompilers)
	}
	if changeTimesError := os.Chtimes(raced, time.Now(), time.Now()); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
	if _, applyPruneError := ApplyPrune(paths, plan); applyPruneError != nil {
		test.Fatal(applyPruneError)
	}
	if _, statError := os.Stat(filepath.Join(raced, "tsc", "go.mod")); statError != nil {
		test.Fatalf("an extraction a build marked in use after the plan was removed: %v", statError)
	}
}

// The other half of the race: a build reusing an extraction marks it, so the hour counts from that
// build. Without the mark, a pin extracted yesterday is stale to the prune while a build compiles
// against it.
func TestEnsureCompilerMarksAReusedExtractionInUse(test *testing.T) {
	test.Parallel()

	paths := Paths{ModuleDirectory: test.TempDir(), CacheDirectory: test.TempDir()}
	commit := "8d550c837c90bd1805b047b7eeccc2baac2d5e7a"
	extraction := filepath.Join(paths.CompilerDirectory(), commit)
	writeFile(test, filepath.Join(extraction, "tsc", "go.mod"), "module tsc\n")
	old := time.Now().Add(-48 * time.Hour)
	if changeTimesError := os.Chtimes(extraction, old, old); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}

	// Read before the reuse, so a mark the call made cannot be earlier, with a second's slack for a filesystem that
	// keeps times to the second. However slow the call, the mark only lands later.
	started := time.Now()
	got, ensureCompilerError := ensureCompiler(paths, commit)
	if ensureCompilerError != nil {
		test.Fatal(ensureCompilerError)
	}
	if got != extraction {
		test.Fatalf("reused %s, expected %s", got, extraction)
	}
	information, statError := os.Stat(extraction)
	if statError != nil {
		test.Fatal(statError)
	}
	if information.ModTime().Before(started.Add(-time.Second)) {
		test.Fatalf(
			"a reused extraction still reads as last used at %s, so a prune would take it mid-build",
			information.ModTime(),
		)
	}
}

func TestApplyPruneRefusesADirectoryNameThatIsAPath(test *testing.T) {
	test.Parallel()

	paths, _ := seedPruneCache(test)
	outside := filepath.Join(paths.CacheDirectory, "keep")
	writeFile(test, filepath.Join(outside, "file"), "not in the compiler cache\n")

	_, applyPruneError := ApplyPrune(paths, PrunePlan{StaleCompilers: []PrunedFile{{Name: "../keep"}}})
	if applyPruneError == nil {
		test.Fatal("a directory name that walks out of the compiler cache was accepted")
	}
	if _, statError := os.Stat(filepath.Join(outside, "file")); statError != nil {
		test.Fatalf("the directory outside the compiler cache was removed: %v", statError)
	}
}

func TestAPruneThatRemovesNothingSaysHowManyItLookedAt(test *testing.T) {
	test.Parallel()

	paths := Paths{ModuleDirectory: test.TempDir(), CacheDirectory: test.TempDir()}
	if createDirectoryError := os.MkdirAll(paths.BinaryDirectory(), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	writeFile(test, filepath.Join(paths.BinaryDirectory(), "cohere-dev"), "binary\n")

	plan, planPruneError := PlanPrune(paths, nil, "", pruneNow)
	if planPruneError != nil {
		test.Fatal(planPruneError)
	}
	if recordPruneError := RecordPrune(paths, plan, nil, pruneNow); recordPruneError != nil {
		test.Fatal(recordPruneError)
	}
	logged, readError := os.ReadFile(paths.PruneLogPath())
	if readError != nil {
		test.Fatal(readError)
	}
	if !strings.Contains(string(logged), "examined 1, removed 0 (0 bytes)") {
		test.Fatalf("the log does not say what an empty prune looked at:\n%s", logged)
	}

	// The other direction: a listing that cannot be read is an error, never an empty plan.
	if _, planPruneError := PlanPrune(
		Paths{CacheDirectory: filepath.Join(test.TempDir(), "absent")},
		nil,
		"",
		pruneNow,
	); planPruneError == nil {
		test.Fatal("a missing binary cache planned as if it were empty")
	}
}

// seedPruneCache fills a binary cache with every kind of entry the real one holds, and returns each
// file's age relative to pruneNow.
func seedPruneCache(test *testing.T) (Paths, map[string]time.Duration) {
	test.Helper()
	paths := Paths{ModuleDirectory: test.TempDir(), CacheDirectory: test.TempDir()}
	if createDirectoryError := os.MkdirAll(
		filepath.Join(paths.BinaryDirectory(), "a-subdirectory"),
		0o755,
	); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}

	ages := map[string]time.Duration{}
	for index := 0; index < 12; index++ {
		// old00 is the oldest. Each is at least two hours old, outside the in-flight window.
		ages[fmt.Sprintf("cohere-%s-old%02d", platformTag(), index)] = time.Duration(100-index) * time.Hour
	}
	for index := 0; index < 10; index++ {
		ages[fmt.Sprintf("cohere-swift-%s-old%02d", platformTag(), index)] = time.Duration(100-index) * time.Hour
	}
	// Recent enough to be in use, so kept however many there are.
	ages["cohere-"+platformTag()+"-fresh0"] = 10 * time.Minute
	ages["cohere-"+platformTag()+"-fresh1"] = 20 * time.Minute
	ages["cohere-"+platformTag()+"-writing.partial-456"] = 2 * time.Minute
	ages["cohere-"+platformTag()+"-stale.partial-123"] = 5 * time.Hour
	// Named keepers, older than everything, so only their names protect them. Listed here rather than
	// read from prunedByName: a fixture built from the map under test changes with it, so dropping a
	// keeper from the map also dropped it from the fixture and the test passed. That happened.
	for _, name := range []string{
		"cohere-dev",
		"cohere-dev.hash",
		"cohere-dispatch",
		"cohere-swift-current",
		"cohere-swift-current.tmp",
	} {
		ages[name] = 500 * time.Hour
	}
	ages["something-else"] = 500 * time.Hour
	ages["cohere-"+platformTag()+"-keepme0000000000"] = 900 * time.Hour

	for name, age := range ages {
		path := filepath.Join(paths.BinaryDirectory(), name)
		if writeError := os.WriteFile(path, []byte(name), 0o755); writeError != nil {
			test.Fatal(writeError)
		}
		stamp := pruneNow.Add(-age)
		if changeTimesError := os.Chtimes(path, stamp, stamp); changeTimesError != nil {
			test.Fatal(changeTimesError)
		}
	}
	return paths, ages
}

// platformTag is the "<goos>-<goarch>" the binary names carry.
func platformTag() string {
	return strings.TrimSuffix(strings.TrimPrefix(platformBinaryPrefix(), "cohere-"), "-")
}
