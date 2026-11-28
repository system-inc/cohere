//go:build unix

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The fill's tests, beside the integrate job's (integrate_job_test.go): a fill judges main's own tip and lands
// nothing (#fnrchck), so its refusals, its one line of output, its skipped measurement and its steps are pinned here.

// A fill judges the commit it runs at, so it takes no merge, and no message, since it lands nothing (#fnrchck): either
// is refused at the option parse, in words, exiting 2 as the options' errors do, before anything is laid or run.
func TestAFillRefusesAMergeOrAMessage(test *testing.T) {
	test.Parallel()
	for _, row := range []struct{ name, value string }{{"merge", ""}, {"message", "a module filled"}} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			fixture := newIntegrateJobFixture(test)
			value := row.value
			if value == "" {
				value = fixture.merged()
			}
			ran := fixture.runJob([]string{"--fill", "--" + row.name, value, "--task", fixtureTask})
			want := "integrate-job --fill judges the commit it runs at and lands nothing, so it takes no --" + row.name
			if ran.exit != 2 || !strings.Contains(ran.errors, want) {
				test.Fatalf("a fill handed --%s exited %d, want 2 saying %q:\n%s", row.name, ran.exit, want, ran.errors)
			}
			if _, statError := os.Stat(filepath.Join(fixture.home, integrateJobScratch)); statError == nil {
				test.Errorf("a fill handed --%s made its scratch before it refused", row.name)
			}
			if calls := readMaybeMissing(test, filepath.Join(fixture.replies, "calls")); len(calls) > 0 {
				test.Errorf("a fill handed --%s ran %q", row.name, calls)
			}
		})
	}
}

// A green fill at main's tip, a landing, says the tree it laid, main's own, as its one line of output, `filled <tree>`
// (#fnrchck): none of the finisher's lines, and never `integrated` or `landed`. Its tests are handed main's commit.
func TestAFillPassesMainsTipAndSaysFilled(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	tip := fixture.landedMain()
	ran := fixture.fill()
	if ran.exit != 0 {
		test.Fatalf("the fill exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
	}
	tree := fixture.git(fixture.repository, "rev-parse", tip+"^{tree}")
	if ran.output != "filled "+tree+"\n" || ran.integrated != "" {
		test.Errorf("the fill printed %q, want only `filled %s`", ran.output, tree)
	}
	for _, refused := range []string{"integrated ", "landed", integrateJobMarker + "performance ",
		integrateJobMarker + "tree ", integrateJobMarker + "message "} {
		if strings.Contains(ran.output, refused) || strings.Contains(ran.errors, refused) {
			test.Errorf("the fill says %q:\n%s\n%s", refused, ran.output, ran.errors)
		}
	}
	if tested := fixture.arguments("tests"); len(tested) == 0 || tested[0] != tip {
		test.Errorf("the fill's tests were handed %q, want main's tip %s first", tested, tip)
	}
}

// A fill measures nothing (#fnrchck): performanceScript never runs, so the stand-in bash is called for the tests
// alone, the account says once that the measurement was skipped, never a measure step's seconds, and no record is
// printed for Loom to keep (#6v8r4nn), since a fill prints no landing's lines.
func TestAFillRunsNoMeasurement(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.landedMain()
	ran := fixture.fill()
	if ran.exit != 0 {
		test.Fatalf("the fill exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
	}
	if calls := string(readMaybeMissing(test, filepath.Join(fixture.replies, "calls"))); calls != "tests\n" {
		test.Errorf("the fill's scripts ran %q, want the tests alone", calls)
	}
	want := integrateJobMarker + "measure skipped: a fill measures nothing\n"
	if strings.Count(ran.errors, want) != 1 || slices.Contains(accountSteps(ran.errors), "measure") {
		test.Errorf("the account doesn't say %q once, or says a measure step:\n%s", want, ran.errors)
	}
	if strings.Contains(ran.output+ran.errors, integrateJobMarker+"performance ") {
		test.Errorf("the fill printed a performance line:\n%s\n%s", ran.output, ran.errors)
	}
}

// A fill judges main's own tip, which changes nothing of its own, so it isolates no rule's fixtures, and its account
// says so once, even with the isolation test in its tree and its history shallow at the tip, as Loom's runner lays a
// fill's: there the tip has no first parent, and reading one refused every fill's checks (J-kexdjp4, #e94a6r4).
func TestAFillOnAShallowHistoryIsolatesNothingAndSaysSo(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	// The isolation test's package sits in go_language, a race token's package, which the gate's listing must find.
	fixture.commitOn(fixture.worktree("a"), map[string]string{unitIsolationFile: "package check\n",
		"internal/go_language/go_language.go": "package go_language\n"}, "check: the isolation test")
	tip := fixture.landedMain()
	writeFixtureFile(test, filepath.Join(fixture.historyRepository(), "shallow"), tip+"\n")
	ran := fixture.fill()
	if ran.exit != 0 {
		test.Fatalf("the fill exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
	}
	want := integrateJobMarker + "isolation skipped: " + fillIsolatesNothing + "\n"
	if strings.Count(ran.errors, want) != 1 {
		test.Errorf("the account doesn't say %q once:\n%s", want, ran.errors)
	}
}

// A fill's account says the steps an integration's does at the same tree, in the same order, but the measurement
// (#fnrchck), so the two compare step by step: the lay, the tests and their box's steps, and each check.
func TestAFillSaysTheStepsAnIntegrationDoes(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	merge := fixture.merged()
	integrated := fixture.integrateJob(merge)
	if integrated.exit != 0 {
		test.Fatalf("the integration exited %d:\n%s", integrated.exit, integrated.errors)
	}
	fixture.git(fixture.repository, "update-ref", "refs/heads/main", merge)
	fixture.historyHolds(merge)
	filled := fixture.fill()
	if filled.exit != 0 {
		test.Fatalf("the fill exited %d:\n%s", filled.exit, filled.errors)
	}
	want := slices.DeleteFunc(accountSteps(integrated.errors), func(step string) bool { return step == "measure" })
	if got := accountSteps(filled.errors); !slices.Equal(got, want) || !slices.Contains(got, "self-check") {
		test.Errorf("the fill's steps are %q, want the integration's but the measurement, %q", got, want)
	}
}
