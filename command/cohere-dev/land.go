package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/streams"
)

// cohere-dev land: a merge queue, so a green gate can't be starved by main moving under it (#fz6xejy).
//
// Landing used to be: gate the branch, then fast-forward main if it hadn't moved. A whole-module gate took
// 7 to 10 minutes on a loaded machine, and others fast-forwarded main during it, so a branch could gate
// green eight times in an hour and never land (#sycrdr6), and every one of those gates compiled a merge
// that never existed again, much of what churned the Go build cache.
//
// Now a landing takes the machine's one land lock, in arrival order, and holds it from the merge to the
// fast-forward: it makes one merge commit of the worktree's head onto current main (landing_commit.go), checks
// the worktree out at it, gates that tree through a pool token, and fast-forwards main to it. Nothing that
// lands this way can move main during another's gate. If main moved anyway, by a commit outside land, the
// fast-forward is refused by git and land merges onto the moved main and gates again.

// landAttempts is how many times land merges and gates before giving up on a main that keeps moving
// outside it.
const landAttempts = 3

// land lands the commit the working directory's worktree is at onto main, and returns its exit code.
func land(arguments []string) int {
	takeHandedWorktree()
	options, parseLandingOptionsError := parseLandingOptions(arguments)
	if parseLandingOptionsError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", parseLandingOptionsError)
		return 2
	}
	worktree, gitOutputError := landWorktree()
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: land runs inside the worktree to land: %v\n", gitOutputError)
		return 2
	}
	// Tracked changes would be left behind by a landing of commits. Untracked files are the worktree's own.
	// Submodules are checked apart (checkSubmodules): most worktrees are made with TypeScript a symlink to
	// another checkout's, and git status refuses that outright, which used to read here as a dirty tree
	// with no change named. A git that fails says so, as itself.
	changes, gitOutputError := gitOutput(
		worktree,
		"status",
		"--porcelain",
		"--untracked-files=no",
		"--ignore-submodules=all",
	)
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading %s's status: %v\n", worktree, gitOutputError)
		return 1
	}
	if changes != "" {
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: land lands commits, and %s has changes that are not committed:\n%s\n",
			worktree,
			changes,
		)
		return 2
	}
	if checkGitlinksError := checkGitlinks(worktree, "HEAD"); checkGitlinksError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", checkGitlinksError)
		return 2
	}
	if checkSubmodulesError := checkSubmodules(worktree); checkSubmodulesError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", checkSubmodulesError)
		return 2
	}
	mainCheckout, checkoutOfMainError := checkoutOfMain(worktree)
	if checkoutOfMainError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", checkoutOfMainError)
		return 1
	}
	if mainCheckout == worktree {
		fmt.Fprintln(
			streams.Errors,
			"cohere-dev: land runs in the worktree being landed, not in the checkout of main itself",
		)
		return 2
	}
	if isMains, differs := runsMainsLand(worktree); !isMains {
		return runMainsLand(worktree, mainCheckout, differs, arguments)
	}

	head, gitOutputError := gitOutput(worktree, "rev-parse", "HEAD")
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading what to land: %v\n", gitOutputError)
		return 1
	}
	// Ignored: a detached worktree has no branch, and the landing then names its commit alone.
	branch, _ := gitOutput(worktree, "symbolic-ref", "--short", "--quiet", "HEAD")
	// The subjects and the task are read before the line, so a landing they refuse never waits for a lock.
	mainCommit, gitOutputError := gitOutput(worktree, "rev-parse", "main")
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading main: %v\n", gitOutputError)
		return 1
	}
	commits, branchCommitsError := branchCommits(worktree, mainCommit, head)
	if branchCommitsError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", branchCommitsError)
		return 1
	}
	if len(commits) == 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: nothing to land: main already holds %s\n", shortCommit(head))
		return 2
	}
	vocabulary, subjectVocabularyError := subjectVocabulary(worktree, head)
	if subjectVocabularyError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", subjectVocabularyError)
		return 1
	}
	if checkSubjectsError := checkSubjects(commits, vocabulary); checkSubjectsError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", checkSubjectsError)
		return 2
	}
	task, landingTaskError := landingTask(commits, options.Task)
	if landingTaskError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", landingTaskError)
		return 2
	}
	subject, landingSubjectError := landingSubject(commits, task, options.Message)
	if landingSubjectError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", landingSubjectError)
		return 2
	}

	directory, landDirectoryError := landDirectory()
	if landDirectoryError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", landDirectoryError)
		return 1
	}
	what := "landing " + shortCommit(head) + " from " + worktree
	held, waitInLineError := waitInLine(directory, 1, what, "land lock", func() {
		fmt.Fprintln(
			streams.Errors,
			"cohere-dev: waiting to land; one landing runs at a time, merge to fast-forward, and this one holds it:",
		)
	})
	if waitInLineError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", waitInLineError)
		return 1
	}
	defer held.release()

	// The worktree is checked out at each landing commit for its gate, and goes back to where it was if nothing
	// lands, so a refused landing leaves it as land found it.
	// Never with --discard-changes: a worktree the gate left changed says so, and is left for its owner.
	restore := func() {
		back := []string{"switch", "--quiet", "--detach", head}
		if branch != "" {
			back = []string{"switch", "--quiet", branch}
		}
		if output, gitCombinedError := gitCombined(worktree, back...); gitCombinedError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: putting %s back where it was (git %s): %v\n%s\n",
				worktree, strings.Join(back, " "), gitCombinedError, output)
		}
	}
	for attempt := 1; attempt <= landAttempts; attempt++ {
		mainCommit, gitOutputError := gitOutput(worktree, "rev-parse", "main")
		if gitOutputError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: reading main: %v\n", gitOutputError)
			return 1
		}
		tree, mergeOntoMainError := mergeOntoMain(worktree, mainCommit, head)
		resolution := ""
		var conflict *mergeConflict
		switch {
		case errors.As(mergeOntoMainError, &conflict):
			// A conflict confined to generated files is regenerated rather than refused (land_regenerate.go).
			resolved, said, code := resolveGeneratedConflict(worktree, head, conflict)
			if code != 0 {
				return code
			}
			tree, resolution = resolved, said
		case mergeOntoMainError != nil:
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", mergeOntoMainError)
			return 1
		}
		// The commit the gate runs on. The one that lands is made after it, of the same tree and parents, with
		// the gate's account in its message.
		gating, commitTreeError := commitTree(
			worktree,
			tree,
			subject+"\n\nBeing gated by cohere-dev land.",
			mainCommit,
			head,
		)
		if commitTreeError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", commitTreeError)
			return 1
		}
		// Main may be what records a submodule as a symlink, and what lands is the merge.
		if checkGitlinksError := checkGitlinks(worktree, gating); checkGitlinksError != nil {
			fmt.Fprintf(
				streams.Errors,
				"cohere-dev: after merging main at %s: %v\n",
				shortCommit(mainCommit),
				checkGitlinksError,
			)
			return 2
		}
		output, gitCombinedError := gitCombined(worktree, "switch", "--quiet", "--detach", gating)
		if gitCombinedError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: checking %s out at the landing to gate it: %v\n%s\n",
				worktree, gitCombinedError, output)
			restore()
			return 1
		}
		// The merge may have moved a pin, and the gate must build against what it lands.
		if checkSubmodulesError := checkSubmodules(worktree); checkSubmodulesError != nil {
			fmt.Fprintf(
				streams.Errors,
				"cohere-dev: after merging main at %s: %v\n",
				shortCommit(mainCommit),
				checkSubmodulesError,
			)
			restore()
			return 2
		}
		code, gated := gate(worktree, gating, task)
		if code == quietRefusedExitCode {
			// Only the wall exits 75 here (quiet.go): go vet and go test never do. A held landing is not a red one, so
			// it does not say failed, and it keeps 75, try again later, so a script can tell the two apart (#vq5b7fs).
			restore()
			fmt.Fprintf(streams.Errors, "cohere-dev: held by the quiet window, so nothing landed; %s is back where it "+
				"was. Land again once it closes: go run ./command/cohere-dev quiet wait\n", worktree)
			return code
		}
		if code != 0 {
			restore()
			fmt.Fprintf(streams.Errors, "cohere-dev: the gate failed, so nothing landed; %s is back where it was\n",
				worktree)
			return code
		}
		landing, commitTreeError := commitTree(
			worktree,
			tree,
			landingMessage(subject, branch, head, mainCommit, commits, gated, resolution),
			mainCommit,
			head,
		)
		if commitTreeError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", commitTreeError)
			restore()
			return 1
		}
		// git refuses a fast-forward when main has moved since the merge, which is the test that nothing
		// landed outside this lock during the gate.
		if output, gitCombinedError := gitCombined(
			mainCheckout,
			"merge",
			"--ff-only",
			landing,
		); gitCombinedError != nil {
			// Ignored: a main that can't be read reads as moved, and the next attempt reads it again and stops
			// there with git's error.
			moved, _ := gitOutput(worktree, "rev-parse", "main")
			if moved != mainCommit {
				fmt.Fprintf(
					streams.Errors,
					"cohere-dev: main moved outside land during the gate, %s to %s; merging and gating again\n",
					shortCommit(mainCommit),
					shortCommit(moved),
				)
				continue
			}
			fmt.Fprintf(
				streams.Errors,
				"cohere-dev: fast-forwarding main in %s: %v\n%s\n",
				mainCheckout,
				gitCombinedError,
				output,
			)
			restore()
			return 1
		}
		// The same tree as the commit it was gated at, so nothing on disk moves.
		// Ignored: main holds the landing either way, and a worktree left at the gated commit says the same.
		_, _ = gitCombined(worktree, "switch", "--quiet", "--detach", landing)
		fmt.Fprintf(streams.Errors, "cohere-dev: landed %s on main: %s\n", shortCommit(landing), subject)
		left := shortCommit(head)
		if branch != "" {
			left = branch + " stays at " + left
		}
		fmt.Fprintf(streams.Errors, "cohere-dev: %s is at it, detached; %s\n", worktree, left)
		return 0
	}
	restore()
	fmt.Fprintf(
		streams.Errors,
		"cohere-dev: main moved outside land during each of %d gates, so nothing landed\n",
		landAttempts,
	)
	return 1
}

// checkSubmodules requires every submodule of the worktree to be checked out at the commit its tree
// records, so the gate builds against what lands. The checkout may be the submodule itself, a linked
// worktree of it, or a symlink to another checkout's, as most of the house's worktrees are made: whichever,
// its head must be the pin.
func checkSubmodules(worktree string) error {
	links, gitlinksOfError := project.GitlinksOf(worktree)
	if gitlinksOfError != nil {
		return gitlinksOfError
	}
	for _, link := range links {
		pin, path := link.Pin, link.Path
		directory := filepath.Join(worktree, path)
		notCheckedOut := fmt.Errorf(
			"%s's submodule %s is not checked out, so the gate could not build against its pin %s",
			worktree,
			path,
			shortCommit(pin),
		)
		// An empty directory is a submodule never checked out, and git run in it answers for the repository
		// around it, so its top level has to be the submodule's own path, symlinks resolved.
		top, gitOutputError := gitOutput(directory, "rev-parse", "--show-toplevel")
		if gitOutputError != nil {
			return notCheckedOut
		}
		resolvedTop, topError := filepath.EvalSymlinks(top)
		resolvedDirectory, directoryError := filepath.EvalSymlinks(directory)
		if topError != nil || directoryError != nil || resolvedTop != resolvedDirectory {
			return notCheckedOut
		}
		head, gitOutputError := gitOutput(directory, "rev-parse", "HEAD")
		if gitOutputError != nil {
			return fmt.Errorf("reading %s's head: %w", directory, gitOutputError)
		}
		if head != pin {
			return fmt.Errorf("%s's %s is at %s, and the tree being landed pins %s, so the gate would build against "+
				"the wrong one: move it (git -C %s checkout --detach %s), or point it at a checkout that is, and land again",
				worktree, path, shortCommit(head), shortCommit(pin), filepath.Join(worktree, path), pin)
		}
	}
	return nil
}

// gitlinkMode is the mode git records a submodule's pin under.
const gitlinkMode = "160000"

// checkGitlinks refuses a commit that records a submodule its `.gitmodules` names as anything but a gitlink.
// Most of the house's worktrees have TypeScript a symlink on disk, so a `git add` of it there stages the
// symlink, mode 120000, in the submodule's place, and a merge committed one on 2026-11-05 (ee1660af). With no
// gitlink nothing marked the fork as a submodule: a bare cohere at the root walked into it, and a whole-module
// test run took in its packages, which panic under -trimpath. No gate failed, since each tests a disk where the
// symlink resolves, so this reads the commit, never the disk, and runs before the tests, in seconds.
func checkGitlinks(repository string, commit string) error {
	resolved, gitOutputError := gitOutput(repository, "rev-parse", "--verify", commit+"^{commit}")
	if gitOutputError != nil {
		return fmt.Errorf("reading the commit %s to check its submodules: %w", commit, gitOutputError)
	}
	declared, gitOutputError := gitOutput(repository, "ls-tree", resolved, "--", ".gitmodules")
	if gitOutputError != nil {
		return fmt.Errorf("reading %s's .gitmodules: %w", shortCommit(resolved), gitOutputError)
	}
	if declared == "" {
		return nil
	}
	// -z, so a path is read whole: each setting is its key, a newline, and its value.
	settings, gitOutputError := gitOutput(repository, "config", "--blob", resolved+":.gitmodules", "--list", "-z")
	if gitOutputError != nil {
		return fmt.Errorf("reading %s's .gitmodules: %w", shortCommit(resolved), gitOutputError)
	}
	for _, setting := range strings.Split(settings, "\x00") {
		key, path, found := strings.Cut(setting, "\n")
		if !found || !strings.HasPrefix(key, "submodule.") || !strings.HasSuffix(key, ".path") {
			continue
		}
		mode, treeModeError := treeMode(repository, resolved, path)
		if treeModeError != nil {
			return treeModeError
		}
		if mode == gitlinkMode {
			continue
		}
		recorded := "nothing"
		if mode != "" {
			recorded = "mode " + mode
		}
		return fmt.Errorf("%s records %s, which .gitmodules names a submodule, as %s, not a gitlink (mode %s): "+
			"restore the gitlink in the index with `git update-index --cacheinfo %s,%s,%s` and commit, "+
			"never `git add` the symlink", shortCommit(resolved), path, recorded, gitlinkMode, gitlinkMode,
			parentPin(repository, resolved, path), path)
	}
	return nil
}

// treeMode is the mode a commit records a path under, or empty when it records nothing there.
func treeMode(repository string, commit string, path string) (string, error) {
	entry, gitOutputError := gitOutput(repository, "ls-tree", commit, "--", path)
	if gitOutputError != nil {
		return "", fmt.Errorf("reading %s at %s: %w", path, shortCommit(commit), gitOutputError)
	}
	if fields := strings.Fields(entry); len(fields) > 0 {
		return fields[0], nil
	}
	return "", nil
}

// parentPin is the commit a submodule is pinned at in the first of a commit's parents to record it as a gitlink,
// what a refusal tells its reader to restore, or a placeholder naming what goes there when none does.
func parentPin(repository string, commit string, path string) string {
	// Ignored: a parent that can't be read leaves the placeholder, and the refusal itself is already certain.
	parents, _ := gitOutput(repository, "rev-parse", commit+"^@")
	for _, parent := range strings.Fields(parents) {
		// Ignored: as above.
		entry, _ := gitOutput(repository, "ls-tree", parent, "--", path)
		if fields := strings.Fields(entry); len(fields) >= 3 && fields[0] == gitlinkMode {
			return fields[2]
		}
	}
	return "<the commit it pins>"
}

// landDirectory holds the land lock and its line, beside the pool's.
func landDirectory() (string, error) {
	pool, poolDirectoryError := poolDirectory()
	if poolDirectoryError != nil {
		return "", poolDirectoryError
	}
	directory := filepath.Join(pool, "land")
	return directory, os.MkdirAll(directory, 0o755)
}

// checkoutOfMain is the worktree that has main checked out, whose working tree a fast-forward must move
// along with the branch.
func checkoutOfMain(worktree string) (string, error) {
	listing, gitOutputError := gitOutput(worktree, "worktree", "list", "--porcelain")
	if gitOutputError != nil {
		return "", fmt.Errorf("listing the repository's worktrees: %w", gitOutputError)
	}
	path := ""
	for _, line := range strings.Split(listing, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/main":
			return path, nil
		}
	}
	return "", errors.New("no worktree of this repository has main checked out, so there is no main to land on")
}

// gate runs the landing gate on the worktree through a pool token: go vet, then the rule CPU budget
// (checkRuleCpu), then the tests of every module go.work names outside a submodule, each in one go
// invocation (workspace.go), saying which it covered, then the unit isolation of the rules the landing changes
// (checkUnitIsolation), then the docs data check (checkDocsData), then cohere on the tree itself (checkCohere).
// task is the landing's, which a land-gate job serves. With its exit code it returns the line a landing commit carries
// about it, which says what ran and for how long.
func gate(worktree string, commit string, task string) (int, string) {
	covered, readGatedWorkspaceError := readGatedWorkspace(worktree)
	if readGatedWorkspaceError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading the modules to gate: %v\n", readGatedWorkspaceError)
		return 2, ""
	}
	reportWorkspace(covered, streams.Errors)
	patterns := covered.Patterns()
	started := time.Now()
	// The modules by directory, as the gate's report names them, not the package patterns, which in cohere run to
	// about 900 characters of shim paths.
	tested := "go test of the module"
	if covered.FromWorkspace {
		directories := make([]string, 0, len(covered.Modules))
		for _, module := range covered.Modules {
			directories = append(directories, module.Directory)
		}
		tested = fmt.Sprintf("go test of %d modules from go.work (%s)", len(directories),
			strings.Join(directories, ", "))
	}
	ran := []string{"go vet", tested}
	// Each command takes its share as the pool is when it starts (tokenRun.step): a gate that waited for its
	// token on a busy pool widens for its tests or its docs check once the pool goes idle.
	code := withSteppedToken("land gate on "+shortCommit(commit)+" in "+worktree, func(step func(string) []string) int {
		vet := exec.Command("go", append([]string{"vet"}, patterns...)...)
		vet.Dir, vet.Env = worktree, gateEnvironment(step("go vet"))
		vet.Stdout, vet.Stderr = os.Stdout, os.Stderr
		// The command writes to the terminal itself, so what this run said first is delivered first.
		streams.Show(streams.Output)
		streams.Show(streams.Errors)
		if runError := vet.Run(); runError != nil {
			return exitCodeOf(runError)
		}
		code, checked := checkRuleCpu(
			worktree,
			func() []string { return gateEnvironment(step("the rule CPU budget check")) },
		)
		if code != 0 {
			return code
		}
		if checked {
			ran = append(ran, "the rule CPU budget")
		}
		// The same environment cohere-dev test gives the module's tests, corpora included (corpora.go).
		testing, uncovered, testEnvironmentError := testEnvironment(gateEnvironment(step("the test run")))
		if testEnvironmentError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", testEnvironmentError)
			return 2
		}
		defer printUncovered(streams.Errors, uncovered)
		// On Home, with what Home could not run on the Mac, or all of it here when Home gives no verdict
		// (home_verdict.go), or as a land-gate job, whose no verdict refuses (land_gate_job.go).
		testCode, where := runModuleTests(worktree, commit, task, covered, testing, time.Now())
		if testCode != 0 {
			return testCode
		}
		ran[1] += " " + where
		code, checked, said := checkUnitIsolation(worktree, commit,
			func() []string { return gateEnvironment(step("the unit isolation")) })
		if code != 0 {
			return code
		}
		if checked {
			ran = append(ran, said)
		}
		code, checked = checkDocsData(worktree, func() []string { return gateEnvironment(step("the docs data check")) })
		if checked {
			ran = append(ran, "the docs data")
		}
		if code != 0 {
			return code
		}
		code, checked = checkCohere(
			worktree,
			func() []string { return gateEnvironment(step("building cohere")) },
			func() []string { return gateEnvironment(step("cohere on the tree")) },
		)
		if checked {
			ran = append(ran, "cohere on the tree, 0 findings and no gaps")
		}
		return code
	})
	return code, fmt.Sprintf("Gated by cohere-dev land in %s: %s.", time.Since(started).Round(time.Second),
		strings.Join(ran, ", "))
}

// ruleCpuCheck is the repository-local, generated stress corpus and its budget test.
const ruleCpuCheck = "./internal/lint/registry/rule_budget_test.go"

// ruleCpuTest is the budget test in ruleCpuCheck that the gate runs by name. A -run pattern that names no test runs
// none and passes, so TestCohereHasTheRuleCpuCheckTheGateRuns holds the file to this name.
const ruleCpuTest = "TestRegisteredRuleCpuBudget"

// docsDataGenerator is the repository's generator of the website's data, relative to its root.
const docsDataGenerator = "./internal/docs_data/tools/generate"

// checkRuleCpu runs uncached on each land attempt, before tests and fast-forward.
// Repositories without this cohere-specific check say so, like the docs data check.
//
// The boolean reports whether the check ran: a repository without it passes, having checked nothing.
func checkRuleCpu(worktree string, environment func() []string) (int, bool) {
	if _, statError := os.Stat(filepath.Join(worktree, ruleCpuCheck)); statError != nil {
		if !os.IsNotExist(statError) {
			fmt.Fprintf(streams.Errors, "cohere-dev: reading the rule CPU check: %v\n", statError)
			return 1, false
		}
		fmt.Fprintf(streams.Errors, "cohere-dev: no %s here, so no rule CPU budget to check\n", ruleCpuCheck)
		return 0, false
	}
	started := time.Now()
	// Uncached: the budget is thread CPU measured on this machine now, which the cache does not key on, so a
	// cached pass would replay an old measurement as this landing's.
	check := exec.Command(
		"go",
		"test",
		"-count=1",
		"-timeout=30s",
		"-run=^"+ruleCpuTest+"$",
		"-v",
		"./"+filepath.ToSlash(filepath.Dir(ruleCpuCheck)),
	)
	check.Dir, check.Env = worktree, environment()
	check.Stdout, check.Stderr = os.Stdout, os.Stderr
	// The command writes to the terminal itself, so what this run said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	if runError := check.Run(); runError != nil {
		return exitCodeOf(runError), true
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: rule CPU budget passed (%s)\n",
		time.Since(started).Round(time.Millisecond))
	return 0, true
}

// docsDataRecapture is what a landing whose docs data went stale runs, then commits.
const docsDataRecapture = "go run " + docsDataGenerator + " -capture"

// checkDocsData refuses a landing that leaves the website's data stale, where the change that made it stale
// is (#h31rcf0). The release checked it and the gate did not, so 0b892cc9 changed no_concatenated_classes,
// landed green without recapturing its examples, and the 1.0 dry run would have failed at its first step.
// -check recaptures every example from the lint tests rather than reading the committed ones back, so it
// can find one stale. A repository without the generator has no docs data, and the gate says so. The
// boolean reports whether the check ran.
func checkDocsData(worktree string, environment func() []string) (int, bool) {
	if _, statError := os.Stat(filepath.Join(worktree, docsDataGenerator)); statError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: no %s here, so no docs data to check\n", docsDataGenerator)
		return 0, false
	}
	started := time.Now()
	check := exec.Command("go", "run", docsDataGenerator, "-check")
	check.Dir, check.Env = worktree, environment()
	check.Stdout, check.Stderr = os.Stdout, os.Stderr
	// The command writes to the terminal itself, so what this run said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	if runError := check.Run(); runError != nil {
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: the docs data is stale, so nothing landed: recapture it with `%s`, commit docs/data, "+
				"and land again\n",
			docsDataRecapture,
		)
		return exitCodeOf(runError), true
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: the docs data is current (%s)\n", time.Since(started).Round(time.Second))
	return 0, true
}

// gateEnvironment is the budget's environment with `GOFLAGS` reduced to the pool's own -p, -buildvcs=false and
// -trimpath, so every landing is gated the same way, whatever its caller's shell has set. A caller's -tags or
// the like once changed what a gate tested. -trimpath is the pool's own (budgetEnvironment): it once failed
// tests that found their fixtures from their own source path (2026-10-05), and since they find them from
// the module instead, it lets every gate share compiles with every other worktree.
//
// -buildvcs=false because most worktrees are made with TypeScript a symlink to another checkout's, and git
// status refuses that tree, so every go build a test runs failed "error obtaining VCS status": cache's land
// at 04:15 on 2026-10-06, after a full gate. Callers used to pass it themselves, and the reduction to -p
// dropped it. Nothing the gate runs reads its own stamp: a build with none says so (packaging's version.go).
func gateEnvironment(budget []string) []string {
	environment := make([]string, 0, len(budget))
	for _, variable := range budget {
		value, isFlags := strings.CutPrefix(variable, "GOFLAGS=")
		if !isFlags {
			environment = append(environment, variable)
			continue
		}
		kept := []string{}
		for _, flag := range strings.Fields(value) {
			if strings.HasPrefix(flag, "-p=") {
				kept = append(kept, flag)
			}
		}
		environment = append(environment, "GOFLAGS="+strings.Join(append(kept, "-buildvcs=false", trimpathFlag), " "))
	}
	return environment
}

func exitCodeOf(runError error) int {
	var exited *exec.ExitError
	if errors.As(runError, &exited) {
		return exited.ExitCode()
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", runError)
	return 1
}

// gitOutput runs git in a directory, the working directory when it is empty, and returns its trimmed output.
func gitOutput(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, outputError := command.Output()
	if outputError != nil {
		return strings.TrimSpace(string(output)), fmt.Errorf(
			"git %s: %w: %s",
			strings.Join(arguments, " "),
			outputError,
			strings.TrimSpace(standardError.String()),
		)
	}
	return strings.TrimSpace(string(output)), nil
}

// gitCombined runs git in a directory and returns everything it printed.
func gitCombined(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, outputError := command.CombinedOutput()
	return strings.TrimSpace(string(output)), outputError
}

func shortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
