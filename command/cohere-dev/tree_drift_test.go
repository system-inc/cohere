//go:build unix

package main

import (
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
// laid tree off its stamp before the tests and after: here none before, and after the scratch a test wrote into the
// tree, with the root it was written in (#ntrewm3).
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
		`laid tree after tests 2 off the stamp \(a walk, [0-9.]+s\)`,
		`laid tree after tests moved \. [0-9T:.Z-]+`,
		`laid tree after tests untracked test-scratch`,
		`laid tree after tests difference none: no tracked file's bytes differ from its HEAD`,
	} {
		if want := regexp.MustCompile(`(?m)^` + integrateJobMarker + line + `$`); !want.MatchString(ran.errors) {
			test.Errorf("the account doesn't match %s:\n%s", want, ran.errors)
		}
	}
	if strings.Contains(ran.errors, integrateJobMarker+"ran tests example.com/fixture/alpha ") {
		test.Errorf("the account names a package that replayed as one that ran:\n%s", ran.errors)
	}
}

// A tracked file a test rewrites is named moved, and its new bytes are said as the trees' difference from their `HEAD`,
// so the account reads a rewrite apart from a touch: go.work.sum moved on Shackler with nothing saying what it gained
// (#d49qcjg).
func TestAnIntegrateJobsAccountSaysWhatATestRewrote(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	tree := filepath.Join(fixture.home, integrateJobScratch, "tree")
	fixture.reply("tests.grows", filepath.Join(tree, "command", "cohere", "main.go"))
	ran := fixture.integrateJob(fixture.merged())
	if ran.exit != 0 {
		test.Fatalf("the integrate job exited %d:\n%s", ran.exit, ran.errors)
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
