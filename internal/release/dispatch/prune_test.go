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

func TestPruneRemovesExactlyWhatNoRunCanReach(t *testing.T) {
	paths, ages := seedPruneCache(t)

	plan, err := PlanPrune(paths, []string{"cohere-" + platformTag() + "-keepme0000000000"}, "", pruneNow)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Examined != len(ages)+1 {
		t.Fatalf("examined %d entries, the cache holds %d", plan.Examined, len(ages)+1)
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
		t.Fatalf("would remove\n  %s\nexpected\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}

	wantUnrecognised := []string{"a-subdirectory", "something-else"}
	if !reflect.DeepEqual(plan.Unrecognised, wantUnrecognised) {
		t.Fatalf("unrecognised %v, expected %v kept and reported", plan.Unrecognised, wantUnrecognised)
	}
}

func TestApplyPruneRemovesOnlyThePlanAndLeavesFrozenACandidate(t *testing.T) {
	paths, ages := seedPruneCache(t)
	plan, err := PlanPrune(paths, nil, "", pruneNow)
	if err != nil {
		t.Fatal(err)
	}

	removed, err := ApplyPrune(paths, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != len(plan.Remove) {
		t.Fatalf("removed %d of the %d planned", len(removed), len(plan.Remove))
	}
	for name := range ages {
		_, statErr := os.Stat(filepath.Join(paths.BinaryDirectory(), name))
		planned := false
		for _, file := range plan.Remove {
			planned = planned || file.Name == name
		}
		if planned != os.IsNotExist(statErr) {
			t.Errorf("%s: planned=%v, but on disk the file %s", name, planned, map[bool]string{true: "is gone", false: "is still there"}[os.IsNotExist(statErr)])
		}
	}

	// A second apply of the same plan finds every file already gone, which is a concurrent prune, not
	// an error.
	if again, err := ApplyPrune(paths, plan); err != nil || len(again) != 0 {
		t.Fatalf("re-applying an applied plan: removed %d, err %v", len(again), err)
	}

	if _, err := ResolveFrozen(paths); err != nil {
		t.Fatalf("after a prune --frozen has nothing to run: %v", err)
	}
}

func TestApplyPruneRefusesANameThatIsAPath(t *testing.T) {
	paths, _ := seedPruneCache(t)
	outside := filepath.Join(paths.CacheDirectory, "outside")
	writeFile(t, outside, "not in the binary cache\n")

	_, err := ApplyPrune(paths, PrunePlan{Remove: []PrunedFile{{Name: "../outside"}}})
	if err == nil {
		t.Fatal("a name that walks out of the binary cache was accepted")
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("the file outside the binary cache was removed: %v", statErr)
	}
}

func TestPruneReportsStaleCompilersAndNeverRemovesThem(t *testing.T) {
	paths, _ := seedPruneCache(t)
	current := filepath.Join(paths.CompilerDirectory(), "1f70213d4922-aaaaaaaaaaaaaaaa")
	stale := filepath.Join(paths.CompilerDirectory(), "0000000000aa-bbbbbbbbbbbbbbbb")
	writeFile(t, filepath.Join(current, "tsc", "go.mod"), "module current\n")
	writeFile(t, filepath.Join(stale, "tsc", "go.mod"), "module stale\n")

	plan, err := PlanPrune(paths, nil, current, pruneNow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.StaleCompilers, []string{filepath.Base(stale)}) {
		t.Fatalf("stale compilers %v, expected only %s", plan.StaleCompilers, filepath.Base(stale))
	}
	if _, err := ApplyPrune(paths, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stale, "tsc", "go.mod")); err != nil {
		t.Fatalf("a stale compiler was removed, which is a walk the prune must not take: %v", err)
	}
}

func TestAPruneThatRemovesNothingSaysHowManyItLookedAt(t *testing.T) {
	paths := Paths{ModuleDirectory: t.TempDir(), CacheDirectory: t.TempDir()}
	if err := os.MkdirAll(paths.BinaryDirectory(), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(paths.BinaryDirectory(), "cohere-dev"), "binary\n")

	plan, err := PlanPrune(paths, nil, "", pruneNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordPrune(paths, plan, nil, pruneNow); err != nil {
		t.Fatal(err)
	}
	logged, err := os.ReadFile(paths.PruneLogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logged), "examined 1, removed 0 (0 bytes)") {
		t.Fatalf("the log does not say what an empty prune looked at:\n%s", logged)
	}

	// The other direction: a listing that cannot be read is an error, never an empty plan.
	if _, err := PlanPrune(Paths{CacheDirectory: filepath.Join(t.TempDir(), "absent")}, nil, "", pruneNow); err == nil {
		t.Fatal("a missing binary cache planned as if it were empty")
	}
}

// seedPruneCache fills a binary cache with every kind of entry the real one holds, and returns each
// file's age relative to pruneNow.
func seedPruneCache(t *testing.T) (Paths, map[string]time.Duration) {
	t.Helper()
	paths := Paths{ModuleDirectory: t.TempDir(), CacheDirectory: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(paths.BinaryDirectory(), "a-subdirectory"), 0o755); err != nil {
		t.Fatal(err)
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
	for _, name := range []string{"cohere-dev", "cohere-dev.hash", "cohere-dispatch", "cohere-swift-current", "cohere-swift-current.tmp"} {
		ages[name] = 500 * time.Hour
	}
	ages["something-else"] = 500 * time.Hour
	ages["cohere-"+platformTag()+"-keepme0000000000"] = 900 * time.Hour

	for name, age := range ages {
		path := filepath.Join(paths.BinaryDirectory(), name)
		if err := os.WriteFile(path, []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
		stamp := pruneNow.Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return paths, ages
}

// platformTag is the "<goos>-<goarch>" the binary names carry.
func platformTag() string {
	return strings.TrimSuffix(strings.TrimPrefix(platformBinaryPrefix(), "cohere-"), "-")
}
