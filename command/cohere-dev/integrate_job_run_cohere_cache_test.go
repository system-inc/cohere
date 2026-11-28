//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// laidCohereCache is the cohere cache of the tree the fixture's integrate job lays, `<tree>/.cache/cohere`.
func (fixture *integrateJobFixture) laidCohereCache() string {
	return filepath.Join(fixture.home, integrateJobScratch, "tree", ".cache", "cohere")
}

// With runnerCohereCacheVariable naming a folder not yet made (#0g9pbgt), the self-check's `<tree>/.cache/cohere` is a
// link to it, made with it, and the account says so. What cohere wrote there is kept between integrations: a second
// job over the same fixture, whose lay cleans the tree, still reads it through the tree's link, and the folder holds
// it.
func TestTheSelfCheckRunsOnTheRunnersKeptCohereCache(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	merge := fixture.merged()
	kept := filepath.Join(test.TempDir(), "cohere cache")
	declared := runnerCohereCacheVariable + "=" + kept
	place := fixture.laidCohereCache()
	want := integrateJobMarker + "cohere cache the runner's kept cohere cache, " + kept + ", linked at " + place +
		" and kept between integrations\n"
	for _, run := range []struct {
		name        string
		environment []string
	}{
		{"the first", []string{declared, "INTEGRATE_JOB_COHERE_KEEPS=written by the first"}},
		{"the second", []string{declared}},
	} {
		ran := fixture.integrateJob(merge, run.environment...)
		if ran.exit != 0 {
			test.Fatalf("%s integrate job exited %d:\n%s", run.name, ran.exit, ran.errors)
		}
		if linked, readLinkError := os.Readlink(place); readLinkError != nil || linked != kept {
			test.Errorf("after %s job %s links to %q (%v), want %s", run.name, place, linked, readLinkError, kept)
		}
		if !strings.Contains(ran.errors, want) {
			test.Errorf("%s job's account doesn't say %q:\n%s", run.name, want, ran.errors)
		}
	}
	for _, path := range []string{filepath.Join(place, "kept"), filepath.Join(kept, "kept")} {
		if said := string(readMaybeMissing(test, path)); said != "written by the first\n" {
			test.Errorf("after the second job %s holds %q, want the first job's line", path, said)
		}
	}
}

// With no runnerCohereCacheVariable the self-check runs on the tree's own cohere cache as it always has: nothing is
// linked, what cohere writes lands in a directory of the tree's own, and the account says the tree's own.
func TestTheSelfCheckWithNoKeptCohereCacheRunsOnTheTreesOwn(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	ran := fixture.integrateJob(fixture.merged(), runnerCohereCacheVariable+"=", "INTEGRATE_JOB_COHERE_KEEPS=its own")
	if ran.exit != 0 {
		test.Fatalf("the integrate job exited %d:\n%s", ran.exit, ran.errors)
	}
	place := fixture.laidCohereCache()
	if information, statError := os.Lstat(place); statError != nil || !information.IsDir() {
		test.Errorf("%s isn't a directory of the tree's own: %v", place, statError)
	}
	want := integrateJobMarker + "cohere cache the tree's own, " + place + ", emptied by the lay, so the self-check " +
		"runs cold\n"
	if !strings.Contains(ran.errors, want) || strings.Contains(ran.errors, "the runner's kept cohere cache") {
		test.Errorf("the account doesn't say %q alone:\n%s", want, ran.errors)
	}
}

// A merge whose tree holds a directory of its own at `.cache/cohere` is refused at the self-check when the runner
// declares a kept cohere cache, naming it: the tree's own is never replaced, and cohere never runs.
func TestTheSelfCheckRefusesATreesOwnCohereCacheDirectory(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, ".cache", "cohere", "committed"), "the tree's own\n")
	fixture.git(worktree, "add", "-f", ".cache/cohere/committed")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a cohere cache of its own"))
	kept := filepath.Join(test.TempDir(), "cohere cache")
	ran := fixture.integrateJob(fixture.merged(), runnerCohereCacheVariable+"="+kept)
	place := fixture.laidCohereCache()
	want := "cohere-dev: " + place + " is the tree's own, not a link, so the self-check won't replace it with the " +
		"runner's cohere cache " + kept + " (" + runnerCohereCacheVariable + ")\n"
	if ran.exit != 1 || !strings.Contains(ran.errors, want) {
		test.Fatalf("the integrate job exited %d, want 1 saying %q:\n%s", ran.exit, want, ran.errors)
	}
	if information, statError := os.Lstat(place); statError != nil || !information.IsDir() {
		test.Errorf("the tree's own %s didn't stay a directory: %v", place, statError)
	}
	if arguments := readMaybeMissing(test, fixture.cohereArguments); len(arguments) > 0 {
		test.Errorf("cohere ran on the tree after the refusal, handed %q", arguments)
	}
}
