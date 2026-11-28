//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// A stamped tree reads as on its stamp, nothing named. Then a scratch directory a test writes into it reads as
// untracked, named once with nothing beneath it, the root it was made in as moved, and a tracked file written since
// as moved, each the way Go's test cache sees it (#ntrewm3). A symbolic link is passed over.
func TestATreeOffItsStampIsNamed(test *testing.T) {
	test.Parallel()
	environment := append(append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com"), gitWithoutMaintenance...)
	tree := test.TempDir()
	writeFixtureFile(test, filepath.Join(tree, "a.txt"), "a\n")
	writeFixtureFile(test, filepath.Join(tree, "inner", "b.txt"), "b\n")
	if linkError := os.Symlink("a.txt", filepath.Join(tree, "link")); linkError != nil {
		test.Fatal(linkError)
	}
	for _, arguments := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"commit", "-q", "-m", "a tree"}} {
		git := exec.Command("git", arguments...)
		git.Dir, git.Env = tree, environment
		if output, gitError := git.CombinedOutput(); gitError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, gitError, output)
		}
	}
	if unstamped, stampError := stampTreeTimes(tree); stampError != nil || len(unstamped) > 0 {
		test.Fatalf("stamping the tree: %v, left %v", stampError, unstamped)
	}
	if off, offError := treeOffTheStamp(tree); offError != nil || len(off) > 0 {
		test.Fatalf("a tree just stamped reads %q off its stamp (%v)", off, offError)
	}
	writeFixtureFile(test, filepath.Join(tree, "scratch", "deep", "out.txt"), "out\n")
	now := time.Now()
	if changeError := os.Chtimes(filepath.Join(tree, "a.txt"), now, now); changeError != nil {
		test.Fatal(changeError)
	}
	off, offError := treeOffTheStamp(tree)
	if offError != nil {
		test.Fatal(offError)
	}
	kinds := []string{}
	for _, line := range off {
		fields := strings.Fields(line)
		kinds = append(kinds, fields[0]+" "+fields[1])
	}
	slices.Sort(kinds)
	if want := []string{"moved .", "moved a.txt", "untracked scratch/"}; !slices.Equal(kinds, want) {
		test.Errorf("the tree reads %q off its stamp, want %q", off, want)
	}
}

// The account names each package that ran, rather than replayed, with its test binary's seconds, and every path of the
// laid tree off its stamp before the tests and after (#ntrewm3). A test's scratch written into the tree is refused by
// the kernel, since every directory of it is read-only while the tests run (holdTreeStill, #d49qcjg), so the tree
// reads as still after them, and its directories have their own modes back.
func TestAnIntegrateJobsAccountNamesWhatRanAndWhatMoved(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	fixture.reply("tests.cached", "example.com/fixture/alpha")
	tree := filepath.Join(fixture.home, integrateJobScratch, "tree")
	fixture.reply("tests.grows", filepath.Join(tree, "test-scratch"))
	ran := fixture.integrateJob(fixture.merged())
	if ran.exit != 0 {
		test.Fatalf("the integrate job exited %d:\n%s", ran.exit, ran.errors)
	}
	for _, line := range []string{
		`ran tests example\.com/fixture/[a-z/]+ 1\.0s`,
		`laid tree before tests 0 off the stamp \(a walk, [0-9.]+s\)`,
		`laid tree after tests 0 off the stamp \(a walk, [0-9.]+s\)`,
	} {
		if want := regexp.MustCompile(`(?m)^` + integrateJobMarker + line + `$`); !want.MatchString(ran.errors) {
			test.Errorf("the account doesn't match %s:\n%s", want, ran.errors)
		}
	}
	if strings.Contains(ran.errors, integrateJobMarker+"ran tests example.com/fixture/alpha ") {
		test.Errorf("the account names a package that replayed as one that ran:\n%s", ran.errors)
	}
	if _, statError := os.Lstat(filepath.Join(tree, "test-scratch")); !errors.Is(statError, os.ErrNotExist) {
		test.Errorf("a test wrote its scratch into the laid tree while the tests ran: %v", statError)
	}
	for _, directory := range []string{tree, filepath.Join(tree, "command", "cohere")} {
		information, statError := os.Stat(directory)
		if statError != nil || information.Mode().Perm()&0o200 == 0 {
			test.Errorf("%s wasn't given back its own mode after the tests: %v, %v", directory, information, statError)
		}
	}
}

// A tracked file a test rewrites in place, which a read-only directory still lets it write, is named moved, its new
// bytes are said as the trees' difference from their `HEAD`, so the account reads a rewrite apart from a touch
// (go.work.sum moved on Shackler with nothing saying what it gained), and the landing is refused, naming it (#d49qcjg).
func TestAnIntegrateJobsAccountSaysWhatATestRewrote(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	tree := filepath.Join(fixture.home, integrateJobScratch, "tree")
	fixture.reply("tests.grows", filepath.Join(tree, "command", "cohere", "main.go"))
	ran := fixture.integrateJob(fixture.merged())
	refused := "cohere-dev: the tests moved 1 path(s) of the laid tree off its stamp, so nothing integrates: moved " +
		"command/cohere/main.go "
	if ran.exit == 0 || !strings.Contains(ran.errors, refused) {
		test.Fatalf("the integrate job exited %d, want it refused saying %q:\n%s", ran.exit, refused, ran.errors)
	}
	if strings.Contains(ran.output, "integrated ") {
		test.Errorf("a refused integration printed its landing:\n%s", ran.output)
	}
	for _, line := range []string{
		`laid tree after tests moved command/cohere/main\.go [0-9T:.Z-]+`,
		`laid tree after tests difference \+\+\+ b/command/cohere/main\.go`,
		`laid tree after tests difference \+entry`,
		`laid tree after tests difference -func main\(\) \{\}`,
	} {
		if want := regexp.MustCompile(`(?m)^` + integrateJobMarker + line + `$`); !want.MatchString(ran.errors) {
			test.Errorf("the account doesn't match %s:\n%s", want, ran.errors)
		}
	}
	if strings.Contains(ran.errors, integrateJobMarker+"laid tree before tests difference") {
		test.Errorf("a tree on its stamp before the tests says a difference:\n%s", ran.errors)
	}
}

// A job killed while its tests hold the laid tree still, by `SIGKILL` to its own pid, never runs its release, so the
// tree stays read-only. The next job lays the same path over it and integrates, since its lay gives the tree back first
// (giveTreeBack): before, every landing after one killed job was broken until a hand repaired the box (the Go Square's
// must on #d49qcjg).
func TestAJobKilledWhileItsTreeIsHeldLeavesTheNextOneItsLay(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	fixture.reply("tests.wait", "")
	merge := fixture.merged()
	tree := filepath.Join(fixture.home, integrateJobScratch, "tree")
	job := exec.Command(fixture.wrapper, "integrate-job", "--merge", merge, "--task", fixtureTask, "--message",
		"a module whose job is killed")
	job.Dir = fixture.job
	job.Env = append(fixture.environment, integrateJobCommitVariable+"="+fixture.git(fixture.repository, "rev-parse",
		"main"))
	if startError := job.Start(); startError != nil {
		test.Fatal(startError)
	}
	// The stand-in's tests wait for tests.go once they've started, so they end whatever happens to the job.
	defer func() {
		if writeError := os.WriteFile(filepath.Join(fixture.replies, "tests.go"), nil, 0o644); writeError != nil {
			test.Error(writeError)
		}
	}()
	for deadline := time.Now().Add(2 * time.Minute); ; time.Sleep(20 * time.Millisecond) {
		if _, statError := os.Stat(filepath.Join(fixture.replies, "tests.started")); statError == nil {
			break
		}
		if time.Now().After(deadline) {
			// Ignored: the job is stopped because the test is ending, and its exit says nothing more.
			_ = job.Process.Kill()
			// Ignored: as above, it's the job this test killed, and how it ended is the kill.
			_ = job.Wait()
			test.Fatal("the job's tests never started")
		}
	}
	if killError := job.Process.Kill(); killError != nil {
		test.Fatal(killError)
	}
	// Ignored: a job killed by its signal exits with it, which is the point.
	_ = job.Wait()
	information, statError := os.Stat(filepath.Join(tree, "command"))
	if statError != nil || information.Mode().Perm()&0o200 != 0 {
		test.Fatalf("the killed job's tree isn't held still, so this shows nothing: %v, %v", information, statError)
	}
	if removeError := os.Remove(filepath.Join(fixture.replies, "tests.wait")); removeError != nil {
		test.Fatal(removeError)
	}
	ran := fixture.integrateJob(merge)
	if ran.exit != 0 || ran.integrated == "" {
		test.Fatalf("the job after the killed one exited %d and integrated %q:\n%s", ran.exit, ran.integrated,
			ran.errors)
	}
}
