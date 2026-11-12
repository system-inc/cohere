// Command cohere-dev runs the house's heavy Go work within the machine's budget (#3kr3x59, #qhg0ntb).
//
//	go run ./command/cohere-dev test ./...            every package: the landing gate
//	go run ./command/cohere-dev test ./internal/edit  one package
//	go run ./command/cohere-dev test --fast           the edit loop: all but the landing gate's own (fast.go)
//	go run ./command/cohere-dev build|vet [packages]  go build or go vet
//	go run ./command/cohere-dev exec -- <command>     any other heavy command: a bench, a cohere run, clang
//	go run ./command/cohere-dev test --bounded <pkg>  under a process limit and a deadline, for a mutant (bound.go)
//	go run ./command/cohere-dev land                  one merge commit onto main, gated, one landing at a time
//	go run ./command/cohere-dev status                the pool: its tokens, their holders, the line, the cache
//	go run ./command/cohere-dev quiet on|off|wait     a quiet window: no job starts but the one it is for (quiet.go)
//	go run ./command/cohere-dev worktree <name>       a hand's worktree, made or moved to main by git (worktree.go)
//	go run ./command/cohere-dev worktree remove <x>   retire one, refusing as a move does
//
// Every one of them but status, quiet and worktree takes a token from the machine's pool, in arrival order,
// and runs with Go held to the token's share of the cores (pool.go). A dozen members each running go on all
// sixteen cores held the machine at load 110 to 180, and made every run slower, not only later. When a
// token's run ends, the Go build cache is looked at, and trimmed under its cap with the pool held (cache.go).
//
// A whole-module run never passes -count=1. Go's test cache skips every package whose inputs did not
// change, which on a one-file change is most of the module, and -count=1 throws that away. A flaky test
// is fixed, not re-run around; name its package to re-run it alone.
//
// Every run is nice 10, and so is everything it starts, so heavy work uses the cores no one else is using
// and yields to a person typing. --full-priority keeps the priority the run was started with, for a
// measurement.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/streams"
)

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode runs the command line and returns the code cohere-dev exits with: every verb returns its exit
// code here, and streams.Finish delivers what was printed and turns a clean exit into 1 when it did not arrive.
func finishedExitCode() int {
	// Deferred calls run while a panic unwinds, though not on os.Exit, so this delivers what was written before
	// the panic's own report, and main exits only after it has run.
	defer func() {
		streams.Show(streams.Errors)
		streams.Show(streams.Output)
	}()
	return streams.Finish(runCommandLine())
}

// runCommandLine runs the verb the command line names and returns its exit code.
func runCommandLine() int {
	if len(os.Args) < 2 {
		return usage()
	}
	switch os.Args[1] {
	case "status":
		return status()
	case trimCacheVerb:
		return trimCache()
	case "quiet":
		return quiet(os.Args[2:])
	case "worktree":
		return namedWorktree(os.Args[2:])
	case "test", "build", "vet", "exec", "land":
		return run(os.Args[1], os.Args[2:])
	}
	return usage()
}

// usage says how cohere-dev is run, and returns the exit code of a command line it refused.
func usage() int {
	fmt.Fprintln(streams.Errors, "usage: cohere-dev test [--fast] [--full-priority] [bounded] [go test flags] "+
		"[packages]\n"+
		"       cohere-dev build|vet [--full-priority] [bounded] [go flags] [packages]\n"+
		"       cohere-dev exec [--full-priority] [bounded] -- <command> [arguments]\n"+
		"         bounded: --bounded, --bounded-margin <processes> (512), --bounded-deadline <duration> (15m)\n"+
		"       cohere-dev land [--task <id>] [--message <what landed>]\n"+
		"       cohere-dev status\n"+
		"       cohere-dev quiet [on [--for <duration>] [--through <name>] [why] | off | wait]\n"+
		"         every job but status, quiet and worktree takes --through <name>, for the run a window lets through\n"+
		"       cohere-dev worktree <name> [--branch <branch>]   "+
		"a name per hand, not per member: system_cohere_tools_1\n"+
		"       cohere-dev worktree remove <name>")
	return 2
}

// fullPriorityFlag keeps a run at the priority it was started with, for a timed measurement. Without it a
// run lowers itself to testNiceness first.
const fullPriorityFlag = "--full-priority"

// testNiceness is how nice a run makes itself, and so every go command and test binary it starts. Tests
// compiling and running on every core held the machine at load 168 on 16 cores and made Kirk's typing lag
// (2026-10-05); at nice 10 they still take every idle cycle and yield to what a person is doing.
const testNiceness = 10

// nicenessVariable sets another niceness than testNiceness, from 0 to 19, for whoever wants their tests
// to yield more, and for the test that shows a run is niced whatever priority it was started at.
const nicenessVariable = "COHERE_DEV_NICENESS"

// niceness is how nice a run makes itself: testNiceness, or what nicenessVariable says.
func niceness() (int, error) {
	value := os.Getenv(nicenessVariable)
	if value == "" {
		return testNiceness, nil
	}
	parsed, parseError := strconv.Atoi(value)
	if parseError != nil || parsed < 0 || parsed > 19 {
		return 0, fmt.Errorf("%s is %q, want a whole number from 0 to 19", nicenessVariable, value)
	}
	return parsed, nil
}

// run runs a heavy command, niced, through a token: go test, go build, go vet, or any command with exec.
func run(verb string, arguments []string) int {
	commandArguments, fullPriority := withoutFlag(arguments, fullPriorityFlag)
	commandArguments, through, throughError := withoutThroughFlag(commandArguments, verb == "exec")
	if throughError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", throughError)
		return 2
	}
	if through != "" {
		if setError := os.Setenv(quietThroughVariable, through); setError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: naming this run %s: %v\n", through, setError)
			return 1
		}
	}
	// A quiet window is a wall where a job starts (quiet.go), met before anything else is done.
	directory, poolDirectoryError := poolDirectory()
	if poolDirectoryError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", poolDirectoryError)
		return 1
	}
	if refusal := quietRefusal(
		directory,
		time.Now(),
		os.Getenv(heldTokenVariable),
		os.Getenv(quietThroughVariable),
	); refusal != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s\n", refusal)
		return quietRefusedExitCode
	}
	if !fullPriority {
		level, nicenessError := niceness()
		if nicenessError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", nicenessError)
			return 2
		}
		if lowerPriorityError := lowerPriority(level); lowerPriorityError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: lowering this run's priority: %v\n", lowerPriorityError)
			return 1
		}
	}

	if verb == "land" {
		return land(commandArguments)
	}
	commandArguments, limit, boundedError := withoutBoundedFlags(commandArguments, verb == "exec")
	if boundedError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", boundedError)
		return 2
	}
	// The bound begins once the run holds its token, so the account's count is read as its work starts, and the
	// deadline does not run while it waits in line.
	bounded := func(work func() int) int {
		if beginError := limit.begin(); beginError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", beginError)
			return 1
		}
		return work()
	}

	switch verb {
	case "exec":
		commandLine := commandArguments
		if len(commandArguments) > 0 && commandArguments[0] == "--" {
			commandLine = commandArguments[1:]
		}
		if len(commandLine) == 0 {
			return usage()
		}
		return withToken(strings.Join(commandLine, " "), func(environment []string) int {
			return bounded(func() int { return runAttached(limit, environment, commandLine[0], commandLine[1:]) })
		})
	case "build":
		return withToken("go build "+strings.Join(commandArguments, " "), func(environment []string) int {
			return bounded(func() int {
				return runAttached(limit, environment, "go", append([]string{"build"}, commandArguments...))
			})
		})
	case "vet":
		return withToken("go vet "+strings.Join(commandArguments, " "), func(environment []string) int {
			return bounded(func() int {
				return runAttached(
					limit,
					withoutVcsStamping(environment),
					"go",
					append([]string{"vet"}, commandArguments...),
				)
			})
		})
	}

	fast := len(commandArguments) > 0 && commandArguments[0] == "--fast"
	unvettedArguments := commandArguments
	if fast {
		unvettedArguments = commandArguments[1:]
	}
	testArguments := withEveryVetCheck(unvettedArguments)
	if fast {
		// The fast tier rests on Go's test cache, and runs nearly every package, so -count=1 here is the
		// costliest run there is on a loaded machine (@system_cohere_build's catch).
		if countsOnce(testArguments) {
			refuseCountOnce()
			return 2
		}
		return withToken("go test --fast "+strings.Join(testArguments, " "), func(environment []string) int {
			return bounded(func() int { return fastTest(withoutVcsStamping(environment), testArguments, limit) })
		})
	}
	if wholeModule(testArguments) && countsOnce(testArguments) {
		refuseCountOnce()
		return 2
	}
	// A whole-module run from the root covers what the landing gate covers: every module go.work names
	// outside a submodule (workspace.go).
	if wholeModule(testArguments) {
		if checkGitlinksError := checkRootGitlinks(); checkGitlinksError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", checkGitlinksError)
			return 2
		}
		expanded, withWorkspaceModulesError := withWorkspaceModules(testArguments)
		if withWorkspaceModulesError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: reading the modules to test: %v\n", withWorkspaceModulesError)
			return 2
		}
		testArguments = expanded
	}
	return withToken("go test "+strings.Join(testArguments, " "), func(environment []string) int {
		return bounded(func() int { return goTest(withoutVcsStamping(environment), testArguments, limit) })
	})
}

// withoutVcsStamping adds -buildvcs=false to a test or vet run's `GOFLAGS`, unless the caller chose a -buildvcs
// of their own. Most worktrees are made with TypeScript a symlink to another checkout's, git status refuses
// that tree, and so every go build a test runs failed "error obtaining VCS status": four of
// @system_cohere_go's Circles set it by hand on 2026-11-05. The landing gate has run with it since
// 2026-10-06 (gateEnvironment), so no test needs the stamp.
//
// Not for build or exec: a binary someone keeps is read for its stamp. packaging's version.go prints its
// commit and whether its tree was modified, and the bench record marks a run dirty from it, so a build
// without the stamp would read as clean.
func withoutVcsStamping(environment []string) []string {
	stamped := make([]string, 0, len(environment)+1)
	hasFlags := false
	for _, variable := range environment {
		value, isFlags := strings.CutPrefix(variable, "GOFLAGS=")
		if !isFlags {
			stamped = append(stamped, variable)
			continue
		}
		hasFlags = true
		chosen := slices.ContainsFunc(strings.Fields(value), func(flag string) bool {
			return flag == "-buildvcs" || strings.HasPrefix(flag, "-buildvcs=")
		})
		if chosen {
			stamped = append(stamped, variable)
			continue
		}
		stamped = append(stamped, "GOFLAGS="+strings.TrimSpace(value+" -buildvcs=false"))
	}
	if !hasFlags {
		stamped = append(stamped, "GOFLAGS=-buildvcs=false")
	}
	return stamped
}

// withEveryVetCheck runs every check go vet runs, the landing gate's set, in a test run's vet, unless the caller
// chose a -vet of their own. go test runs a curated subset by default, which leaves out composites: 130
// unkeyed literals of another package's struct passed cohere-dev test and failed plain go vet (#kr2yp54).
func withEveryVetCheck(arguments []string) []string {
	for _, argument := range arguments {
		if argument == "-vet" ||
			argument == "--vet" ||
			strings.HasPrefix(argument, "-vet=") ||
			strings.HasPrefix(argument, "--vet=") {
			return arguments
		}
	}
	return append([]string{"-vet=all"}, arguments...)
}

// withoutFlag returns the arguments with every copy of flag removed, and whether there was one, so a flag of
// this wrapper's never reaches go test.
func withoutFlag(arguments []string, flag string) ([]string, bool) {
	kept := make([]string, 0, len(arguments))
	found := false
	for _, argument := range arguments {
		if argument == flag {
			found = true
			continue
		}
		kept = append(kept, argument)
	}
	return kept, found
}

// wholeModule reports whether the arguments name every package, the run that takes a slot.
func wholeModule(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "./..." || argument == "..." {
			return true
		}
	}
	return false
}

// withWorkspaceModules replaces a ./... run from the repository's root with the patterns of every module
// the landing gate covers, saying which. From anywhere else ./... means that directory's tree, and stays.
func withWorkspaceModules(arguments []string) ([]string, error) {
	root, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		return nil, workingDirectoryError
	}
	if _, statError := os.Stat(filepath.Join(root, "go.work")); statError != nil {
		return arguments, nil
	}
	covered, readGatedWorkspaceError := readGatedWorkspace(root)
	if readGatedWorkspaceError != nil {
		return nil, readGatedWorkspaceError
	}
	reportWorkspace(covered, streams.Errors)
	expanded := make([]string, 0, len(arguments)+len(covered.Modules))
	for _, argument := range arguments {
		if argument == "./..." {
			expanded = append(expanded, covered.Patterns()...)
			continue
		}
		expanded = append(expanded, argument)
	}
	return expanded, nil
}

// checkRootGitlinks runs the land gate's gitlink check (checkGitlinks) on the head of a whole-module run from a
// repository's root, which is the gate the Square runs on every merge: a merge that records TypeScript as a
// symlink passes its tests on a disk where the symlink resolves. A ./... anywhere else is that directory's tree,
// and a root with no .git has no commit to read, so both run as they did.
func checkRootGitlinks() error {
	root, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		return workingDirectoryError
	}
	if _, statError := os.Lstat(filepath.Join(root, ".git")); statError != nil {
		if os.IsNotExist(statError) {
			return nil
		}
		return fmt.Errorf("reading whether %s is a repository's root: %w", root, statError)
	}
	return checkGitlinks(root, "HEAD")
}

// refuseCountOnce says why a run of nearly every package, through the slot or the fast tier, never passes
// -count=1.
func refuseCountOnce() {
	fmt.Fprintln(streams.Errors, "cohere-dev: a whole-module run never passes -count=1: it re-runs every package "+
		"whose inputs did not change, which Go's test cache would have skipped. Fix a flaky test rather than re-run "+
		"around it, and name a package to re-run it alone: go test -count=1 ./its/package")
}

// countsOnce reports whether the arguments turn Go's test cache off with -count=1.
func countsOnce(arguments []string) bool {
	for index, argument := range arguments {
		switch {
		case argument == "-count=1" || argument == "--count=1":
			return true
		case (argument == "-count" || argument == "--count") && index+1 < len(arguments) && arguments[index+1] == "1":
			return true
		}
	}
	return false
}

// goTest runs go test with the caller's terminal, under the bound when there is one, and returns its exit code.
func goTest(base []string, arguments []string, limit *bound) int {
	environment, uncovered, testEnvironmentError := testEnvironment(base)
	if testEnvironmentError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", testEnvironmentError)
		return 2
	}
	defer printUncovered(streams.Errors, uncovered)
	return runAttached(limit, environment, "go", append([]string{"test"}, arguments...))
}

// holderLine describes the run holding a token: who, where, since when and what it runs.
func holderLine(what string) string {
	// Ignored: the line only tells a waiting run who holds the token; one whose directory can't be read still
	// names its pid and what it runs, and the lock, not this line, is what holds the token.
	directory, _ := os.Getwd()
	return fmt.Sprintf("pid %d in %s since %s: %s", os.Getpid(), directory, time.Now().Format("15:04:05"), what)
}

// holders reads who holds each taken slot. A slot that is free says nothing.
func holders(directory string, slots int) []string {
	lines := []string{}
	for slot := 1; slot <= slots; slot++ {
		contents, readError := os.ReadFile(holderPath(directory, slot))
		if readError == nil && len(contents) > 0 {
			lines = append(lines, strings.TrimSpace(string(contents)))
		}
	}
	return lines
}

func lockPath(directory string, slot int) string {
	return filepath.Join(directory, fmt.Sprintf("slot-%d.lock", slot))
}

func holderPath(directory string, slot int) string {
	return filepath.Join(directory, fmt.Sprintf("slot-%d.holder", slot))
}

// heldSlot is a token this process holds until release, or until it exits.
type heldSlot struct {
	number int
	lock   *os.File
	holder string
}

func (held *heldSlot) release() {
	// Ignored: a holder file left behind names no one, since a token is held by its lock and heldBy reads the
	// lock first, as it does for a run killed outright.
	_ = os.Remove(held.holder)
	unlockFile(held.lock)
	// Ignored: nothing was written through the lock file, so its close loses nothing, and the lock is gone with
	// the descriptor, or with the process.
	_ = held.lock.Close()
}

// takeSlot takes the first free token, or returns nil when every token is held.
func takeSlot(directory string, slots int, what string) (*heldSlot, error) {
	for slot := 1; slot <= slots; slot++ {
		lock, openError := os.OpenFile(lockPath(directory, slot), os.O_CREATE|os.O_RDWR, 0o644)
		if openError != nil {
			return nil, openError
		}
		taken, tryLockFileError := tryLockFile(lock)
		if tryLockFileError != nil {
			_ = lock.Close() // Ignored: never written, and the lock's error is the one returned.
			return nil, tryLockFileError
		}
		if !taken {
			_ = lock.Close() // Ignored: never written, and never locked by this process.
			continue
		}
		holder := holderPath(directory, slot)
		if writeError := os.WriteFile(holder, []byte(holderLine(what)+"\n"), 0o644); writeError != nil {
			unlockFile(lock)
			_ = lock.Close() // Ignored: never written, and the holder's write error is the one returned.
			return nil, writeError
		}
		return &heldSlot{number: slot, lock: lock, holder: holder}, nil
	}
	return nil, nil
}
