package main

import (
	"regexp"
	"testing"
)

// Adamic readiness through the caches (#drbrp8c): every number a cached run prints is the number a `--no-cache`
// run prints over the same tree. A replayed file that was never measured, or one replayed as ready when its
// imports moved, would read as Adamic-ready with nothing behind it, the cache's one failure that looks like a
// success. Each test runs the cold truth on the tree the cached run saw and compares the segments whole.

// readinessPattern is the footer's readiness segment, measured or not.
var readinessPattern = regexp.MustCompile(`\d+% Adamic-ready \([^)]*\)|Adamic readiness not measured: [^\n•]*`)

// readinessIn is a run's readiness segment, failing the test when the run printed none.
func readinessIn(t *testing.T, output string) string {
	t.Helper()
	segment := readinessPattern.FindString(output)
	if segment == "" {
		t.Fatalf("the run printed no readiness segment:\n%s", output)
	}
	return segment
}

// readinessFixture is the run cache's fixture with one file ready, one an adamic rule fails, and one that fails
// only through what it imports: an `any` arriving from types.ts is unsafe to assign, which no-unsafe-assignment
// (type-aware, in cohere:adamic) reports.
func readinessFixture(t *testing.T) *runCacheFixture {
	t.Helper()
	fixture := newRunCacheFixture(t, buildCohere(t))
	fixture.write("source/definite.ts", "export class Account {\n    balance!: number;\n}\n")
	fixture.write("source/types.ts", "export type Value = number;\n")
	fixture.write("source/use.ts", "import type { Value } from \"./types\";\n\ndeclare const value: Value;\nexport const doubled: number = value;\n")
	fixture.commit("readiness")
	return fixture
}

// coldReadiness is the readiness a `--no-cache` run reads over the tree as it is now.
func (fixture *runCacheFixture) coldReadiness() string {
	fixture.t.Helper()
	output, _ := fixture.run(false)
	return readinessIn(fixture.t, output)
}

// (a) A whole-run replay prints the readiness its recording run measured, and that is the cold number.
func TestAReplayedRunReadsTheReadinessACheckedOneDoes(t *testing.T) {
	t.Parallel()
	fixture := readinessFixture(t)
	fixture.establishHit()
	replayed, _ := fixture.run(true)
	if !isRunCacheReplay(replayed) {
		t.Fatalf("an unchanged tree did not replay:\n%s", replayed)
	}
	cold := fixture.coldReadiness()
	if got := readinessIn(t, replayed); got != cold {
		t.Errorf("replayed readiness %q, cold %q", got, cold)
	}
	if !regexp.MustCompile(`^\d+% `).MatchString(cold) || cold[:4] == "100%" {
		t.Errorf("the cold run reads %q: the fixture's failing files did not fail, so the comparison proves nothing", cold)
	}
}

// (b) A run the whole-run cache misses, by an edit to one file, replays every other file from the findings cache,
// and readiness counted from those entries is the cold number: each entry carries its file's counts.
func TestReadinessFromTheFindingsCacheIsTheCheckedReadiness(t *testing.T) {
	t.Parallel()
	fixture := readinessFixture(t)
	fixture.establishHit()
	fixture.write("source/a.ts", "export const a: number = 2;\n")
	warm, _ := fixture.run(true)
	if isRunCacheReplay(warm) {
		t.Fatalf("an edited tree replayed the whole run:\n%s", warm)
	}
	if got, cold := readinessIn(t, warm), fixture.coldReadiness(); got != cold {
		t.Errorf("readiness from the findings cache %q, cold %q", got, cold)
	}
}

// (c) Readiness counts before suppression, so a file whose only adamic finding a comment disables, or one the
// project's chain turns the rule off for, is not ready, warm as cold. The suppressed finding never reaches the
// report, which is how a cache that kept only reported findings would replay the file as ready.
func TestASuppressedAdamicFindingKeepsItsFileUnreadyWarm(t *testing.T) {
	t.Parallel()
	fixture := readinessFixture(t)
	fixture.write("source/definite.ts", "export class Account {\n    // eslint-disable-next-line adamic/no-definite-assignment\n    balance!: number;\n}\n")
	fixture.write("source/chained.ts", "export let total!: number;\n")
	fixture.write("CohereSettings.json", `{"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":"error","no-var":"error","adamic/no-definite-assignment":"off"}}`)
	fixture.commit("suppressed")
	cold := fixture.coldReadiness()

	fixture.establishHit()
	replayed, _ := fixture.run(true)
	if got := readinessIn(t, replayed); got != cold {
		t.Errorf("replayed readiness %q, cold %q", got, cold)
	}
	fixture.write("source/a.ts", "export const a: number = 3;\n")
	warm, _ := fixture.run(true)
	if got, coldNow := readinessIn(t, warm), fixture.coldReadiness(); got != coldNow {
		t.Errorf("readiness from the findings cache %q, cold %q", got, coldNow)
	}

	// The control: with the suppressed files' findings gone from the source, readiness rises, so the cold
	// number above counted them.
	fixture.write("source/definite.ts", "export class Account {\n    balance = 0;\n}\n")
	fixture.write("source/chained.ts", "export const total = 0;\n")
	if fixed := fixture.coldReadiness(); fixed == cold {
		t.Errorf("readiness reads %q with and without the suppressed findings, so it never counted them", cold)
	}
}

// (d) An unchanged file whose import's shape changed is measured again: types.ts's Value becomes any, use.ts's
// bytes stay, and its unsafe assignment makes it unready, warm as cold. A findings cache keyed on use.ts's bytes
// alone would replay it ready.
func TestAnImportsShapeChangeIsMeasuredAgainWarm(t *testing.T) {
	t.Parallel()
	fixture := readinessFixture(t)
	fixture.establishHit()
	before := fixture.coldReadiness()

	// `any` without the keyword, so no-explicit-any stays quiet and types.ts stays as ready as it was: only
	// use.ts can move.
	fixture.write("source/types.ts", "export type Value = ReturnType<typeof JSON.parse>;\n")
	warm, _ := fixture.run(true)
	if isRunCacheReplay(warm) {
		t.Fatalf("an edited tree replayed the whole run:\n%s", warm)
	}
	cold := fixture.coldReadiness()
	if got := readinessIn(t, warm); got != cold {
		t.Errorf("readiness after an imported shape changed %q, cold %q", got, cold)
	}
	if cold == before {
		t.Errorf("readiness reads %q before and after use.ts's import became any, so the fixture does not move it", cold)
	}
}
