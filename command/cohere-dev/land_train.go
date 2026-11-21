package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/streams"
)

// A train lands two to four accepted units under one gate: `cohere-dev land --train <head> <head> --task <id>`.
// The Go org's accepted units came faster than one 40-minute gate on Sun2 could land them, nine deep on 2026-11-20,
// and every head in the line merged and gated again each time main moved under it. A train built by hand, 0d6e96146,
// merged several units into one branch and landed them under one gate, two gates saved. Now land builds it.
//
// Every refusal it can make without a gate it makes before the line: the count of heads, the train's own task, a
// head given twice or main already holds, each unit's subjects and its one task, and every merge subject the train
// will write. The train is composed without touching a worktree, a chain of merges from main, unit by unit, so a unit
// that clashes with the units before it is named before any gate. Then a pre-check builds the train and runs the fast
// tier, before the lock, so a train red in minutes never holds the line for a 40-minute gate. Then it lands as a single
// landing lands its head (landOntoMain), with its last merge the head, and each unit's task is closed with the
// landing.

// trainFewestUnits and trainMostUnits bound a train: one unit is a landing, and more than four is a red gate that
// can't say whose it is.
const (
	trainFewestUnits = 2
	trainMostUnits   = 4
)

// trainPreCheckVariable, set to trainPreCheckOff, skips a train's pre-check. A fixture's go is a stand-in that logs
// every run as a gate's and lists no package, so the fixture's trains skip it; unset, every train runs it.
const trainPreCheckVariable = "COHERE_DEV_TRAIN_PRECHECK"

// trainPreCheckOff is the trainPreCheckVariable setting that skips the pre-check.
const trainPreCheckOff = "off"

// trainUnit is one unit of a train, as its head was given and read.
type trainUnit struct {
	// Head is the head as given, a branch or a commit, and Commit the commit it names.
	Head   string
	Commit string
	// Task is the one task the unit's own subjects name.
	Task string
	// What is what the unit brings, for its line in the landing's body.
	What string
	// MergeSubject is the subject of the train's merge of this unit.
	MergeSubject string
}

// trainUnitName is a unit as a refusal names it: its head as given, its commit, and its task.
func trainUnitName(unit trainUnit) string {
	name := shortCommit(unit.Commit)
	if unit.Head != name {
		name = unit.Head + " at " + name
	}
	return name + " (#" + unit.Task + ")"
}

// landTrain lands the train options names from the worktree, and returns land's exit code.
func landTrain(worktree string, mainCheckout string, options landingOptions) int {
	if count := len(options.Heads); count < trainFewestUnits || count > trainMostUnits {
		fmt.Fprintf(streams.Errors, "cohere-dev: a train lands %d to %d heads, and %d were given: --train <head> "+
			"<head> [<head> [<head>]]\n", trainFewestUnits, trainMostUnits, count)
		return 2
	}
	if options.Task == "" {
		fmt.Fprintln(streams.Errors, "cohere-dev: a train names a task of its own with --task <id>, apart from its "+
			"units' tasks, which it closes once it lands")
		return 2
	}
	at, gitOutputError := gitOutput(worktree, "rev-parse", "HEAD")
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading where %s is: %v\n", worktree, gitOutputError)
		return 1
	}
	// Ignored: a detached worktree has no branch, and goes back to its commit alone.
	branch, _ := gitOutput(worktree, "symbolic-ref", "--short", "--quiet", "HEAD")
	mainCommit, gitOutputError := gitOutput(worktree, "rev-parse", "main")
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading main: %v\n", gitOutputError)
		return 1
	}
	units, code := readTrainUnits(worktree, mainCommit, options.Heads)
	if code != 0 {
		return code
	}
	message := options.Message
	if message == "" {
		message = fmt.Sprintf("a train of %d units", len(units))
	}
	subject, landingSubjectError := landingSubject(nil, options.Task, message)
	if landingSubjectError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", landingSubjectError)
		return 2
	}
	train, code := composeTrain(worktree, mainCommit, units)
	if code != 0 {
		return code
	}
	if code = preCheckTrain(worktree, train, branch, at); code != 0 {
		return code
	}
	code, landing := landOntoMain(landingRun{
		Worktree:     worktree,
		MainCheckout: mainCheckout,
		Head:         train,
		Branch:       branch,
		At:           at,
		Task:         options.Task,
		Subject:      subject,
		Message: func(_ string, gated string, resolution string) string {
			return trainMessage(subject, units, gated, resolution)
		},
		RedGate: func() { sayTrainUnitsDirectories(worktree, mainCommit, units) },
	})
	if code != 0 {
		return code
	}
	closeTrainTasks(worktree, landing, options.Task, units)
	return 0
}

// readTrainUnits reads each head as a unit of a train onto main at mainCommit, in order, refusing with exit 2 one that
// can't be a unit: no commit, given twice, held by main already, conflicting with main itself, its subjects not the
// house's, or naming no one task of its own, and a merge subject over subjectLimit.
func readTrainUnits(worktree string, mainCommit string, heads []string) ([]trainUnit, int) {
	units := []trainUnit{}
	for _, head := range heads {
		commit, gitOutputError := gitOutput(worktree, "rev-parse", "--verify", "--quiet", head+"^{commit}")
		if gitOutputError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: the train's head %q names no commit\n", head)
			return nil, 2
		}
		if index := slices.IndexFunc(units, func(unit trainUnit) bool { return unit.Commit == commit }); index >= 0 {
			fmt.Fprintf(streams.Errors, "cohere-dev: the train names %s twice, as %q and %q, and a unit lands once\n",
				shortCommit(commit), units[index].Head, head)
			return nil, 2
		}
		commits, branchCommitsError := branchCommits(worktree, mainCommit, commit)
		if branchCommitsError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", branchCommitsError)
			return nil, 1
		}
		tree, mergeOntoMainError := mergeOntoMain(worktree, mainCommit, commit)
		var conflict *mergeConflict
		switch {
		case errors.As(mergeOntoMainError, &conflict):
			// A single landing regenerates a conflict confined to generated files (land_regenerate.go); a train
			// leaves that to the unit's own landing, so its merges stay plain merges.
			fmt.Fprintf(streams.Errors, "cohere-dev: the train's unit %s conflicts with main itself, and no gate "+
				"runs: %v\n", head, conflict)
			return nil, 2
		case mergeOntoMainError != nil:
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", mergeOntoMainError)
			return nil, 1
		}
		held, heldError := heldByMain(worktree, mainCommit, commit, tree)
		if heldError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: reading whether main holds %s: %v\n", shortCommit(commit),
				heldError)
			return nil, 1
		}
		if held != "" {
			fmt.Fprintf(streams.Errors, "cohere-dev: the train's unit %s is nothing to land, and no gate runs: %s\n",
				head, held)
			return nil, 2
		}
		vocabulary, subjectVocabularyError := subjectVocabulary(worktree, commit)
		if subjectVocabularyError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", subjectVocabularyError)
			return nil, 1
		}
		if checkSubjectsError := checkSubjects(commits, vocabulary); checkSubjectsError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: the train's unit %s: %v\n", head, checkSubjectsError)
			return nil, 2
		}
		// Each unit names its own task, since the train closes it once it lands.
		task, landingTaskError := landingTask(commits, "")
		if landingTaskError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: each unit of a train names its one task in its own subjects, as "+
				"`(#task)`, and %s at %s doesn't: land it alone, with --task <id>\n", head,
				shortCommit(commit))
			return nil, 2
		}
		unit := trainUnit{
			Head:         head,
			Commit:       commit,
			Task:         task,
			MergeSubject: "Merge " + shortCommit(commit) + " (#" + task + ") into the train",
		}
		for _, own := range commits {
			if !own.Merge {
				unit.What = strings.TrimSpace(strings.ReplaceAll(own.Subject, "(#"+task+")", ""))
				break
			}
		}
		if length := len([]rune(unit.MergeSubject)); length > subjectLimit {
			fmt.Fprintf(streams.Errors, "cohere-dev: the train's merge of %s would read %q, %d characters, over %d: "+
				"land it alone\n", trainUnitName(unit), unit.MergeSubject, length, subjectLimit)
			return nil, 2
		}
		units = append(units, unit)
	}
	return units, 0
}

// composeTrain chains the units' merges from main at mainCommit, touching no worktree: each merge's first parent the
// one before it, main first, and its second the unit. It returns the last merge, the train's head, or refuses with
// exit 2 a unit that clashes with the units before it, naming them and the paths.
func composeTrain(worktree string, mainCommit string, units []trainUnit) (string, int) {
	train := mainCommit
	for index, unit := range units {
		tree, mergeOntoMainError := mergeOntoMain(worktree, train, unit.Commit)
		var conflict *mergeConflict
		if errors.As(mergeOntoMainError, &conflict) {
			before := []string{}
			for _, earlier := range units[:index] {
				before = append(before, trainUnitName(earlier))
			}
			fmt.Fprintf(streams.Errors, "cohere-dev: the train's unit %d, %s, clashes with the units before it, %s, "+
				"and no gate runs. It conflicts with them in:\n%s\nLand it in a later train, or after them\n",
				index+1, trainUnitName(unit), strings.Join(before, ", "), strings.Join(conflict.Paths, "\n"))
			return "", 2
		}
		if mergeOntoMainError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: composing the train: %v\n", mergeOntoMainError)
			return "", 1
		}
		merge, commitTreeError := commitTree(worktree, tree, unit.MergeSubject, train, unit.Commit)
		if commitTreeError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: composing the train: %v\n", commitTreeError)
			return "", 1
		}
		train = merge
	}
	return train, 0
}

// preCheckTrain builds the train at head and runs the fast tier on it, as `cohere-dev test --fast` runs it, in the
// worktree checked out at it, before the train takes a place in the land line, and puts the worktree back at branch
// or at either way. A red returns exit 1, a quiet window's hold 75.
func preCheckTrain(worktree string, head string, branch string, at string) int {
	if os.Getenv(trainPreCheckVariable) == trainPreCheckOff {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s=%s, so the train's pre-check is skipped\n", trainPreCheckVariable,
			trainPreCheckOff)
		return 0
	}
	output, gitCombinedError := gitCombined(worktree, "switch", "--quiet", "--detach", head)
	if gitCombinedError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: checking %s out at the train to pre-check it: %v\n%s\n", worktree,
			gitCombinedError, output)
		putWorktreeBack(worktree, branch, at)
		return 1
	}
	defer putWorktreeBack(worktree, branch, at)
	// A unit may move a pin, and the build must be against what lands.
	if checkSubmodulesError := checkSubmodules(worktree); checkSubmodulesError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: in the train: %v\n", checkSubmodulesError)
		return 2
	}
	failed := ""
	code := withSteppedToken("the pre-check of the train "+shortCommit(head)+" in "+worktree,
		func(step func(string) []string) int {
			build := exec.Command("go", "build", "-buildvcs=false", "./...")
			build.Dir, build.Env = worktree, step("go build")
			build.Stdout, build.Stderr = os.Stdout, os.Stderr
			// The command writes to the terminal itself, so what this run said first is delivered first.
			streams.Show(streams.Output)
			streams.Show(streams.Errors)
			if runError := build.Run(); runError != nil {
				failed = "go build ./..."
				return exitCodeOf(runError)
			}
			code := fastTest(worktree, withoutVcsStamping(step("the fast tier")), withEveryVetCheck(nil), nil)
			if code != 0 {
				failed = "the fast tier"
			}
			return code
		})
	if code == quietRefusedExitCode {
		fmt.Fprintf(streams.Errors, "cohere-dev: the train's pre-check was held by the quiet window, so nothing "+
			"landed; %s is back where it was. Land again once it closes: go run ./command/cohere-dev quiet wait\n",
			worktree)
		return code
	}
	if code != 0 {
		if failed == "" {
			failed = "its token"
		}
		fmt.Fprintf(streams.Errors, "cohere-dev: the train's pre-check is red at %s, so it never took a place in the "+
			"land line; %s is back where it was\n", failed, worktree)
		return 1
	}
	fmt.Fprintln(streams.Errors, "cohere-dev: the train's pre-check passed: go build ./... and the fast tier")
	return 0
}

// trainMessage is a train's landing commit's whole message: its subject, each unit in order with its task and what
// it brings, the sentence a resolved conflict carries, and the gate's account.
func trainMessage(subject string, units []trainUnit, gated string, resolution string) string {
	var message strings.Builder
	fmt.Fprintf(&message, "%s\n\nA train of %d units, gated once:\n\n", subject, len(units))
	for _, unit := range units {
		fmt.Fprintf(&message, "- %s #%s: %s\n", shortCommit(unit.Commit), unit.Task, unit.What)
	}
	if resolution != "" {
		fmt.Fprintf(&message, "\n%s\n", resolution)
	}
	fmt.Fprintf(&message, "\n%s\n", gated)
	return message.String()
}

// sayTrainUnitsDirectories says, of a red gate on a train, the directories each unit's diff from main at mainCommit
// touches. The gate's output goes to the terminal unread, so land can't tell which unit's package failed, and says so
// rather than guess.
func sayTrainUnitsDirectories(worktree string, mainCommit string, units []trainUnit) {
	fmt.Fprintln(streams.Errors, "cohere-dev: land doesn't read which packages the gate failed, so it can't say "+
		"whose unit they are; above is what failed, and each unit's diff touches:")
	for _, unit := range units {
		files, gitOutputError := gitOutput(worktree, "diff", "--name-only", mainCommit+"..."+unit.Commit)
		if gitOutputError != nil {
			fmt.Fprintf(streams.Errors, "  %s: its diff could not be read: %v\n", trainUnitName(unit), gitOutputError)
			continue
		}
		directories := []string{}
		for _, file := range strings.Split(files, "\n") {
			if file == "" {
				continue
			}
			if directory := path.Dir(file); !slices.Contains(directories, directory) {
				directories = append(directories, directory)
			}
		}
		slices.Sort(directories)
		fmt.Fprintf(streams.Errors, "  %s: %s\n", trainUnitName(unit), strings.Join(directories, ", "))
	}
}

// closeTrainTasks closes each unit's task with the train's landing through ahra, one line said per unit. A close that
// fails is said with the command to run by hand, and never fails the land: the landing is on main already.
func closeTrainTasks(worktree string, landing string, trainTask string, units []trainUnit) {
	for index, unit := range units {
		others := []string{}
		for otherIndex, other := range units {
			if otherIndex != index {
				others = append(others, "#"+other.Task)
			}
		}
		note := fmt.Sprintf("Landed in %s, train #%s, gated once with %s", shortCommit(landing), trainTask,
			spokenList(others))
		byHand := fmt.Sprintf("ahra tasks done %s %q", unit.Task, note)
		command, loomError := loomCommand(worktree, "tasks", "done", unit.Task, note)
		if loomError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: #%s is not closed (%v); the landing is on main, so close it by "+
				"hand: %s\n", unit.Task, loomError, byHand)
			continue
		}
		output, runError := command.CombinedOutput()
		if runError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: #%s is not closed (%s); the landing is on main, so close it by "+
				"hand: %s\n", unit.Task, lastWords(output, describeEnd(runError)), byHand)
			continue
		}
		fmt.Fprintf(streams.Errors, "cohere-dev: closed #%s: %s\n", unit.Task, note)
	}
}

// spokenList is a list as a sentence says it: `#a`, `#a and #b`, `#a, #b and #c`.
func spokenList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
