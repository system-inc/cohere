package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/command_line/streams"
	"github.com/system-inc/cohere/internal/testing/needs"
)

// cohere-dev land-job: the one command a Loom land job runs on Shackler to judge a merge Loom made for main (#etpnnw1,
// Kirk's design; its units #s50bcnr, a warm cache, #6ex523f, measurement first, and the seam with Loom, #hnbb5vr). It
// runs in the job's own tree, main's tip laid with no history, so its code is main's, and Loom runs one at a time, so
// it takes no land lock. It runs one git throughout, the developer tools' own (landJobGit), never macOS's shim. It
// reads the merge from the mirror landJobMirrorVariable names, refuses one whose first parent isn't the commit it runs
// at, and lays the merge's tree under its scratch. Each step says one line with its seconds, and the first refusal
// stops it:
//
//  1. Preflight: what no gate needs to run for, refused in seconds (landJobPreflight).
//  2. Lay: the merge's tree and its TypeScript pin under the scratch.
//  3. Measure: performanceScript run here with bash, before any test, handed main's binary's sha256, so a merge that
//     builds main's binary keeps main's file and times nothing.
//  4. Tests: macBoxScript run here with bash and read by readBoxRun, so coverage, the shards, -race and the verdict
//     are the gate box's, on the Go cache landJobCacheVariable keeps, or an empty one.
//  5. Checks: go vet, the rule CPU budget, the unit isolation, the docs data and cohere on the tree, through the gate's
//     own functions, a step whose inputs a passed run keyed replaying it (landJobReplay).
//  6. Landing: nothing is committed or pushed. The last lines say what Loom's finisher needs to make the landing on
//     Kirk's Mac (landJobLandingLines): the performance file, the tested tree's hash, and the landing's whole message.

// landJobCacheVariable is the land job's kept directory: its Go build and test cache, `gocache`, kept across jobs, and
// its replay store, `steps`. Unset, the land job's tests run as a gate box's do, on an empty cache with -count=1.
const landJobCacheVariable = "COHERE_LAND_CACHE"

// landJobShellVariable names a command run in place of bash for the land job's scripts, for a test: it is handed
// bash's arguments and the script on its standard input. Unset, bash runs them.
const landJobShellVariable = "COHERE_DEV_LAND_JOB_SHELL"

// landJobGitVariable names a git run in place of the one landJobGit finds, for a test. Unset, the land job finds it.
const landJobGitVariable = "COHERE_DEV_LAND_JOB_GIT"

// landJobMirrorVariable is the cohere mirror the land job reads the merge from, where Loom's bridge puts it; unset, it
// is landJobMirrorDefault. The TypeScript mirror beside it, TypeScript.git, holds every pin.
const (
	landJobMirrorVariable = "COHERE_LAND_MIRROR"
	landJobMirrorDefault  = macBoxMirrors + "/cohere.git"
)

// landJobCommitVariable is the commit the Loom job runs at, which Loom's runner sets in every job's environment. Unset,
// as in a plain checkout, the commit is the working directory's `HEAD`.
const landJobCommitVariable = "LOOM_JOB_COMMIT"

// landJobBox is the box a land job runs on, which its lines name: Shackler, who holds numbers of record.
const landJobBox = performanceBox

// landJobScratch is where the land job keeps its mirrors and its tree, under its home, beside the scripts' own scratch.
var landJobScratch = filepath.Join(strings.TrimPrefix(macBoxScratch, "~/"), "land-job")

// landJobMarker opens each of the land job's lines Loom's finisher reads.
const landJobMarker = "land-job: "

// fullCommit is a commit as `--merge` names it, whole.
var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// landJobOptions are what land-job is told: the merge Loom made, the task it finishes, and what landed when its
// commits don't say it in one subject.
type landJobOptions struct {
	Merge   string
	Task    string
	Message string
}

// parseLandJobOptions reads `--merge <sha>`, `--task <id>` and `--message <what landed>`, each also as `--name=value`,
// the merge, whole, and the task required.
func parseLandJobOptions(arguments []string) (landJobOptions, error) {
	options := landJobOptions{}
	for index := 0; index < len(arguments); index++ {
		name, value, joined := strings.Cut(strings.TrimPrefix(arguments[index], "--"), "=")
		if !strings.HasPrefix(arguments[index], "--") || (name != "merge" && name != "task" && name != "message") {
			return landJobOptions{}, fmt.Errorf("land-job takes --merge <sha>, --task <id> and --message <what "+
				"landed>, and %q is none of them", arguments[index])
		}
		if !joined {
			if index+1 == len(arguments) {
				return landJobOptions{}, fmt.Errorf("--%s needs a value", name)
			}
			index++
			value = arguments[index]
		}
		switch name {
		case "merge":
			options.Merge = strings.TrimSpace(value)
		case "task":
			options.Task = strings.TrimPrefix(strings.TrimSpace(value), "#")
		case "message":
			options.Message = strings.TrimSpace(value)
		}
	}
	if !fullCommit.MatchString(options.Merge) || options.Task == "" {
		return landJobOptions{}, errors.New("land-job needs --merge <the merge's whole sha> and --task <id>")
	}
	return options, nil
}

// landJobRun is one land job as its steps carry it.
type landJobRun struct {
	options landJobOptions
	started time.Time
	// running is the commit the job runs at, main as the merge's first parent must be, and candidate its second.
	running   string
	candidate string
	merge     string
	// mirror is the cohere mirror the merge is read from, and scratch the land job's own: its mirrors, borrowing the
	// house's, of which repository is cohere's, and tree, the merge's tree laid.
	mirror     string
	scratch    string
	mirrors    string
	repository string
	tree       string
	// commits are what the candidate brings to main, and subject the landing's, both read by the preflight.
	commits []branchCommit
	subject string
	// mergeTree is the merge's tree, and landedTree the tree that lands, mergeTree with the performance file the
	// measurement wrote or kept.
	mergeTree  string
	landedTree string
	// measured is what the measurement says in the landing's body, and ran what the gate ran, as its account says it.
	measured string
	ran      []string
	// cache is the land job's kept directory, empty when landJobCacheVariable is unset.
	cache string
	// git is the git every git the job starts runs (landJobGit), first on its own path and on its scripts'.
	git string
	// steps is each step that ended, `<step> <seconds>`, for the landing's body.
	steps []string
}

// landJob judges --merge and returns its exit code: 0 when it would land, its last lines what Loom's finisher needs
// and the very last `landed <tree>`, and 1 when a step refused, the reason in its last lines.
func landJob(arguments []string) int {
	options, parseError := parseLandJobOptions(arguments)
	if parseError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", parseError)
		return 2
	}
	// The git is found before anything runs one, and its directory goes first on the job's own path, so every git it
	// starts, its own, its packages' and its children's, is that one, and the account's path is otherwise as it was.
	git, landJobGitError := landJobGit()
	if landJobGitError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", landJobGitError)
		return 1
	}
	path := filepath.Dir(git) + string(os.PathListSeparator) + os.Getenv("PATH")
	if setError := os.Setenv("PATH", path); setError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: putting %s first on the land job's path: %v\n", filepath.Dir(git),
			setError)
		return 1
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: land-job runs the git at %s\n", git)
	job := &landJobRun{options: options, started: time.Now(), merge: options.Merge, git: git,
		cache:  strings.TrimSpace(os.Getenv(landJobCacheVariable)),
		mirror: settingOr(landJobMirrorVariable, landJobMirrorDefault)}
	if job.cache != "" && !filepath.IsAbs(job.cache) {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s=%q is no directory's whole path\n", landJobCacheVariable, job.cache)
		return 2
	}
	job.running = strings.TrimSpace(os.Getenv(landJobCommitVariable))
	if job.running == "" {
		// Ignored: a working directory with no history has no `HEAD`, which the refusal below says.
		job.running, _ = gitOutput("", "rev-parse", "--verify", "HEAD^{commit}")
	}
	if job.running == "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: land-job can't tell the commit it runs at: %s is unset, and the "+
			"working directory has no HEAD\n", landJobCommitVariable)
		return 1
	}
	home, homeError := os.UserHomeDir()
	if homeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: finding the land job's home for its scratch: %v\n", homeError)
		return 1
	}
	// The pool hangs from the account's home in the system's user database, never `HOME` (poolHome), so that a gate
	// with a scratch `HOME` still shares the machine's pool. A land job runs alone in an account of its own, whose home
	// in that database is /var/empty in Shackler's sandbox (J-5xseq90: "mkdir /var/empty/Library"), so its pool hangs
	// from the job's own home, unless one is named.
	if os.Getenv(homeVariable) == "" {
		if setError := os.Setenv(homeVariable, home); setError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: hanging the land job's pool from its home %s: %v\n", home,
				setError)
			return 1
		}
	}
	// A land job has no network, so when Loom hands it a laid module set in `GOMODCACHE`, its own go commands read
	// that set with the proxy and the checksum database off, as its scripts do: the test listing, go vet, the rule CPU
	// budget, the unit isolation, the docs data, cohere's build and cohere's own go children. Each then fails naming a
	// module the set lacks, never waiting on a network it can't reach (#5wj23dd).
	if strings.TrimSpace(os.Getenv("GOMODCACHE")) != "" {
		for _, setting := range [][2]string{{"GOPROXY", "off"}, {"GOSUMDB", "off"}} {
			if setError := os.Setenv(setting[0], setting[1]); setError != nil {
				fmt.Fprintf(streams.Errors, "cohere-dev: setting %s=%s for the land job's laid modules: %v\n",
					setting[0], setting[1], setError)
				return 1
			}
		}
	}
	// cohere-swift can't be built in a land job's sandbox (needs.SwiftUnlaid), so the tests that build it skip by name
	// (needs.SwiftLaid), cohere on the tree checks every language but Swift (landJobCohereArguments), a merge that
	// changes what only Swift checks is refused (landJobSwiftRefusal), and the landing's account says so.
	if setError := os.Setenv(needs.SwiftUnlaidVariable, needs.SwiftUnlaidSetting); setError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: setting %s for the land job's tests: %v\n", needs.SwiftUnlaidVariable,
			setError)
		return 1
	}
	job.scratch = filepath.Join(home, landJobScratch)
	job.mirrors = filepath.Join(job.scratch, "mirrors")
	job.repository = filepath.Join(job.mirrors, "cohere.git")
	job.tree = filepath.Join(job.scratch, "tree")
	if makeError := os.MkdirAll(job.scratch, 0o755); makeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: making the land job's scratch: %v\n", makeError)
		return 1
	}
	job.sayDisk("before")
	code := job.land()
	job.say("step total %s", landJobSeconds(time.Since(job.started)))
	job.sayDisk("after")
	if code != 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: land-job refused after %s, so nothing lands\n",
			landJobSeconds(time.Since(job.started)))
		return code
	}
	return 0
}

// commandLineToolsGit is where the Command Line Tools keep the real git on a Mac, and shimGit macOS's /usr/bin/git,
// which runs xcrun to find the developer tools and caches its answer under the account's temporary directory, which a
// sandboxed job account can't write (Loom job J-0bgcqrx on Shackler, Nov 25, #5wj23dd).
const (
	commandLineToolsGit = "/Library/Developer/CommandLineTools/usr/bin/git"
	shimGit             = "/usr/bin/git"
)

// landJobGit is the git the land job runs: the one landJobGitVariable names, or the real one realGit finds here. Its
// directory goes on a path, so it is refused when it holds the path's separator.
func landJobGit() (string, error) {
	git := strings.TrimSpace(os.Getenv(landJobGitVariable))
	if git != "" && !filepath.IsAbs(git) {
		return "", fmt.Errorf("%s=%q is no git's whole path", landJobGitVariable, git)
	}
	if git == "" {
		found, realGitError := realGit(runtime.GOOS, "/", xcodeDeveloperDirectory, exec.LookPath)
		if realGitError != nil {
			return "", realGitError
		}
		git = found
	}
	if strings.ContainsRune(filepath.Dir(git), os.PathListSeparator) {
		return "", fmt.Errorf("the git at %s is in a directory whose name holds %q, so no path can name it", git,
			os.PathListSeparator)
	}
	return git, nil
}

// realGit is the real git on operatingSystem: on darwin the first that is an executable file of the Command Line
// Tools' (commandLineToolsGit) and the one in the developer directory developerDirectory names, each under root, `/`
// but for a test, and never shimGit; anywhere else the git lookPath finds on the path. A Mac with neither refuses,
// naming both, since the shim would run xcrun.
func realGit(
	operatingSystem string,
	root string,
	developerDirectory func() (string, error),
	lookPath func(string) (string, error),
) (string, error) {
	if operatingSystem != "darwin" {
		return lookPath("git")
	}
	shim := filepath.Join(root, shimGit)
	commandLineTools := filepath.Join(root, commandLineToolsGit)
	if isRealGit(commandLineTools, shim) {
		return commandLineTools, nil
	}
	developer, developerError := developerDirectory()
	if developerError != nil {
		return "", fmt.Errorf("no git of the developer tools is at %s, and `xcode-select -p` named no developer "+
			"directory for the other (%w), so land-job refuses rather than run %s, whose xcrun can't write its cache "+
			"in a job's sandbox", commandLineTools, developerError, shimGit)
	}
	xcode := filepath.Join(root, developer, "usr", "bin", "git")
	if isRealGit(xcode, shim) {
		return xcode, nil
	}
	return "", fmt.Errorf("no git of the developer tools is at %s or %s, so land-job refuses rather than run %s, "+
		"whose xcrun can't write its cache in a job's sandbox", commandLineTools, xcode, shimGit)
}

// isRealGit says whether candidate is an executable file other than shim, either reached through a link.
func isRealGit(candidate string, shim string) bool {
	information, statError := os.Stat(candidate)
	if statError != nil || !information.Mode().IsRegular() || information.Mode().Perm()&0o111 == 0 {
		return false
	}
	resolved, resolveError := filepath.EvalSymlinks(candidate)
	if resolveError != nil {
		return false
	}
	// Ignored: a root with no shim resolves it to nothing, which no candidate that exists resolves to.
	shimResolved, _ := filepath.EvalSymlinks(shim)
	return resolved != shimResolved
}

// xcodeDeveloperDirectory is the developer directory `xcode-select -p` names, Xcode's or the Command Line Tools'. It
// is no xcrun shim, so it runs anywhere the land job does.
func xcodeDeveloperDirectory() (string, error) {
	output, outputError := exec.Command("xcode-select", "-p").Output()
	if outputError != nil {
		return "", outputError
	}
	developer := strings.TrimSpace(string(output))
	if !filepath.IsAbs(developer) {
		return "", fmt.Errorf("it said %q, no directory's whole path", developer)
	}
	return developer, nil
}

// settingOr is the setting variable holds, or fallback when it holds none.
func settingOr(variable string, fallback string) string {
	if setting := strings.TrimSpace(os.Getenv(variable)); setting != "" {
		return setting
	}
	return fallback
}

// land runs the job's steps in order and returns the exit code.
func (job *landJobRun) land() int {
	for _, step := range []struct {
		name string
		work func() (string, int)
	}{
		{"preflight", job.preflight},
		{"lay", job.lay},
		{"measure", job.measure},
	} {
		if code := job.step(step.name, step.work); code != 0 {
			return code
		}
	}
	// The tests and the checks run through one pool token, each taking its share as the pool is when it starts.
	code := withSteppedToken("land job of "+shortCommit(job.merge), func(share func(string) []string) int {
		if code := job.step("tests", func() (string, int) { return job.tests(share) }); code != 0 {
			return code
		}
		return job.step("checks", func() (string, int) { return job.checks(share) })
	})
	// A pool that couldn't be read or waited in refuses as a step does, and the quiet window keeps its 75.
	if code != 0 && code != quietRefusedExitCode {
		return 1
	}
	if code != 0 {
		return code
	}
	lines, linesError := job.landingLines()
	if linesError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", linesError)
		return 1
	}
	// What was said first is delivered first, so the landing's lines are the last of everything printed.
	streams.Show(streams.Errors)
	fmt.Fprint(streams.Output, lines)
	return 0
}

// step runs work, the step named name, and says one line of it with its seconds: what work said when it passed, or
// that it refused, work having said why. It returns work's exit code, any refusal 1 but the quiet window's 75, which
// says try again later.
func (job *landJobRun) step(name string, work func() (string, int)) int {
	started := time.Now()
	said, code := work()
	spent := landJobSeconds(time.Since(started))
	job.say("step %s %s", name, spent)
	if code != 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: land-job step %s refused in %s\n", name, spent)
		streams.Show(streams.Errors)
		if code == quietRefusedExitCode {
			return code
		}
		return 1
	}
	job.steps = append(job.steps, name+" "+spent)
	fmt.Fprintf(streams.Errors, "cohere-dev: land-job step %s %s: %s\n", name, spent, said)
	streams.Show(streams.Errors)
	return 0
}

// landJobSeconds is a step's time as its line says it, in seconds to a tenth.
func landJobSeconds(spent time.Duration) string {
	return fmt.Sprintf("%.1fs", spent.Seconds())
}

// preflight lays the land job's mirrors, borrowing the house's, reads the merge from them, refuses one whose first
// parent isn't the commit the job runs at, and runs landJobPreflight on its parents.
func (job *landJobRun) preflight() (string, int) {
	if layError := layLandJobMirrors(filepath.Join(job.mirror, "objects"), job.mirrors,
		[]string{filepath.Join(filepath.Dir(job.mirror), "TypeScript.git", "objects")}); layError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: laying the land job's mirrors in %s: %v\n", job.mirrors, layError)
		return "", 1
	}
	parents, parentsError := gitOutput(job.repository, "rev-list", "--parents", "-n", "1", job.merge)
	fields := strings.Fields(parents)
	if parentsError != nil || len(fields) != 3 {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s holds no merge %s of two parents (%v)\n", job.mirror,
			job.merge, parentsError)
		return "", 1
	}
	if fields[1] != job.running {
		fmt.Fprintf(streams.Errors, "cohere-dev: the merge %s is onto %s, and this job runs at %s, so its code "+
			"wouldn't be the main it judges\n", shortCommit(job.merge), shortCommit(fields[1]),
			shortCommit(job.running))
		return "", 1
	}
	job.candidate = fields[2]
	// The merge is kept on a ref of the land job's own, so the tree laid from it is never left dangling.
	if _, updateError := gitOutput(job.repository, "update-ref", "refs/land-job/merge", job.merge); updateError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", updateError)
		return "", 1
	}
	mergeTree, treeError := gitOutput(job.repository, "rev-parse", job.merge+"^{tree}")
	if treeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", treeError)
		return "", 1
	}
	job.mergeTree = mergeTree
	commits, subject, refusal, preflightError := landJobPreflight(job.repository, job.running, job.candidate,
		job.merge, landingOptions{Task: job.options.Task, Message: job.options.Message})
	if preflightError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", preflightError)
		return "", 1
	}
	if refusal != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s\n", refusal)
		return "", 1
	}
	swiftRefusal, swiftError := landJobSwiftRefusal(job.repository, job.merge)
	if swiftError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", swiftError)
		return "", 1
	}
	if swiftRefusal != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s\n", swiftRefusal)
		return "", 1
	}
	job.commits, job.subject = commits, subject
	return subject + ", " + shortCommit(job.candidate) + " merged onto main at " + shortCommit(job.running) + " as " +
		shortCommit(job.merge), 0
}

// landJobPreflight is the land job's first step, every refusal it can make without a gate, in seconds: what head
// brings to main at mainCommit, merged as merge, and the landing's subject as options make it, or the refusal in
// words a submitter can act on, or the error of a git it couldn't read with.
//
// The preflight's call site (#mf8t6se): land --preflight's preflight is being built beside this, and once it lands this
// body is `preflight(repository, mainCommit, head, options)`, its reading's Commits and Subject returned. Until then it
// runs today's checks as land runs them before its line: main already holding head; the subjects of the commits it
// brings; its task and the landing's subject; and the merge recording each submodule as a gitlink.
func landJobPreflight(
	repository string,
	mainCommit string,
	head string,
	merge string,
	options landingOptions,
) ([]branchCommit, string, string, error) {
	commits, branchCommitsError := branchCommits(repository, mainCommit, head)
	if branchCommitsError != nil {
		return nil, "", "", branchCommitsError
	}
	tree, treeError := gitOutput(repository, "rev-parse", merge+"^{tree}")
	if treeError != nil {
		return nil, "", "", treeError
	}
	held, heldError := heldByMain(repository, mainCommit, head, tree)
	if heldError != nil {
		return nil, "", "", fmt.Errorf("reading whether main holds %s: %w", shortCommit(head), heldError)
	}
	if held != "" {
		return nil, "", "nothing to land, and no gate runs: " + held, nil
	}
	vocabulary, subjectVocabularyError := subjectVocabulary(repository, head)
	if subjectVocabularyError != nil {
		return nil, "", "", subjectVocabularyError
	}
	if checkSubjectsError := checkSubjects(commits, vocabulary); checkSubjectsError != nil {
		return nil, "", checkSubjectsError.Error(), nil
	}
	task, landingTaskError := landingTask(commits, options.Task)
	if landingTaskError != nil {
		return nil, "", landingTaskError.Error(), nil
	}
	subject, landingSubjectError := landingSubject(commits, task, options.Message)
	if landingSubjectError != nil {
		return nil, "", landingSubjectError.Error(), nil
	}
	if checkGitlinksError := checkGitlinks(repository, merge); checkGitlinksError != nil {
		return nil, "", checkGitlinksError.Error(), nil
	}
	return commits, subject, "", nil
}

// landJobSwiftRefusal is why the land job refuses merge, read from repository, for what it changes against its first
// parent that only Swift's checks judge, empty when it changes none of it: any path under swift_language/, the Swift
// package, and the Swift files of the packages that run cohere-swift or build it, swift.go and swift_*.go in
// landJobSwiftDirectories, whose tests that build it skip in a land job (needs.SwiftLaid). Until Machines lays Swift on
// Shackler (#je1r32a) no land job checks them, so a change to them is never landed unchecked. Renames are read as a
// removal and an addition, so a file moved out of the package is still the package's change.
// landJobSwiftDirectories are the packages whose Swift files landJobSwiftRefusal refuses: command/cohere runs
// cohere-swift, and release's dispatch and packaging build it.
var landJobSwiftDirectories = []string{"command/cohere/", "internal/release/dispatch/", "internal/release/packaging/"}

func landJobSwiftRefusal(repository string, merge string) (string, error) {
	changed, diffError := gitOutput(repository, "diff", "--name-only", "--no-renames", merge+"^1", merge)
	if diffError != nil {
		return "", fmt.Errorf("reading what the merge %s changes: %w", shortCommit(merge), diffError)
	}
	swift := []string{}
	for _, changedPath := range strings.Split(changed, "\n") {
		directory, name := path.Split(changedPath)
		swiftFile := (name == "swift.go" || strings.HasPrefix(name, "swift_")) && strings.HasSuffix(name, ".go")
		if strings.HasPrefix(changedPath, "swift_language/") ||
			(swiftFile && slices.Contains(landJobSwiftDirectories, directory)) {
			swift = append(swift, changedPath)
		}
	}
	if len(swift) == 0 {
		return "", nil
	}
	return fmt.Sprintf("%s, so the merge's change to %s can't be judged here, and nothing lands", needs.SwiftUnlaid,
		strings.Join(swift, ", ")), nil
}

// lay checks the merge's tree out under the land job's scratch, a worktree of its mirror kept across jobs, with its
// TypeScript at the pin it records.
func (job *landJobRun) lay() (string, int) {
	if _, statError := os.Stat(filepath.Join(job.tree, ".git")); statError != nil {
		// Ignored: a prune with nothing to prune says nothing, and the add below says why it failed if it does.
		_, _ = gitCombined(job.repository, "worktree", "prune")
		if output, addError := gitCombined(job.repository, "worktree", "add", "--quiet", "--force", "--detach",
			job.tree, job.merge); addError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: laying the merge's tree at %s: %v\n%s\n", job.tree, addError,
				output)
			return "", 1
		}
	}
	for _, arguments := range [][]string{
		{"checkout", "--quiet", "--detach", "--force", job.merge},
		{"clean", "-q", "-f", "-d", "-x", "-e", "TypeScript"},
	} {
		if output, gitError := gitCombined(job.tree, arguments...); gitError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: laying the merge's tree at %s: %v\n%s\n", job.tree, gitError,
				output)
			return "", 1
		}
	}
	// Ignored: a tree with no TypeScript gitlink needs no pin.
	pin, _ := gitOutput(job.repository, "rev-parse", "--verify", "--quiet", job.merge+":TypeScript")
	if pin != "" {
		if layError := layTypeScript(filepath.Join(job.mirrors, "TypeScript.git"), job.tree, pin); layError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: laying TypeScript at %s: %v\n", shortCommit(pin), layError)
			return "", 1
		}
	}
	if checkSubmodulesError := checkSubmodules(job.tree); checkSubmodulesError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", checkSubmodulesError)
		return "", 1
	}
	return shortCommit(job.merge) + " at " + job.tree, 0
}

// layTypeScript checks tree's TypeScript out at pin, a worktree of the land job's TypeScript mirror kept across jobs.
func layTypeScript(mirror string, tree string, pin string) error {
	place := filepath.Join(tree, "TypeScript")
	if _, statError := os.Stat(filepath.Join(place, ".git")); statError != nil {
		// Ignored: the gitlink's empty directory goes so the worktree can take its place, and one that isn't empty
		// makes the add below fail in git's words.
		_ = os.Remove(place)
		if output, addError := gitCombined(mirror, "worktree", "add", "--quiet", "--force", "--detach", place,
			pin); addError != nil {
			return fmt.Errorf("%w: %s", addError, output)
		}
	}
	if output, checkoutError := gitCombined(place, "checkout", "--quiet", "--detach", "--force",
		pin); checkoutError != nil {
		return fmt.Errorf("%w: %s", checkoutError, output)
	}
	return nil
}

// layLandJobMirrors makes the mirrors the land job's scripts read in place of the house's (macBoxMirrors), in
// directory: cohere.git, a bare repository borrowing cohereObjects, the cohere mirror's, through git's alternates, and
// TypeScript.git, borrowing each of typeScriptObjects that exists. Nothing is copied. Both are kept across jobs, and
// each job writes their borrowing anew.
func layLandJobMirrors(cohereObjects string, directory string, typeScriptObjects []string) error {
	for name, objects := range map[string][]string{"cohere.git": {cohereObjects}, "TypeScript.git": typeScriptObjects} {
		repository := filepath.Join(directory, name)
		if _, statError := os.Stat(filepath.Join(repository, "HEAD")); statError != nil {
			if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
				return makeError
			}
			if output, initError := gitCombined(directory, "init", "-q", "--bare", repository); initError != nil {
				return fmt.Errorf("git init --bare %s: %w: %s", repository, initError, output)
			}
		}
		existing := []string{}
		for _, borrowedObjects := range objects {
			if information, statError := os.Stat(borrowedObjects); statError == nil && information.IsDir() {
				existing = append(existing, borrowedObjects)
			}
		}
		if name == "cohere.git" && len(existing) == 0 {
			return fmt.Errorf("there is no cohere mirror at %s", filepath.Dir(cohereObjects))
		}
		alternates := filepath.Join(repository, "objects", "info", "alternates")
		if makeError := os.MkdirAll(filepath.Dir(alternates), 0o755); makeError != nil {
			return makeError
		}
		contents := ""
		if len(existing) > 0 {
			contents = strings.Join(existing, "\n") + "\n"
		}
		if writeError := os.WriteFile(alternates, []byte(contents), 0o644); writeError != nil {
			return writeError
		}
	}
	return nil
}

// measure measures the merge here, before any test runs (#6ex523f): performanceScript, handed main's binary's sha256
// from main's performance file, through the land job's own bash, read as a landing on the gap reads it. A merge that
// builds main's binary keeps main's file, the script having timed nothing.
func (job *landJobRun) measure() (string, int) {
	said, measurable, code := cohereToMeasure(job.repository, job.merge)
	if !measurable {
		job.landedTree, job.measured = job.mergeTree, said
		if code == 0 {
			job.say("measure skipped: %s", said)
		}
		return said, code
	}
	// A main whose file is missing or doesn't read has no binary to compare, so the script measures, and the note says
	// why it compares with nothing.
	mainBinary := ""
	previous, found, unread := readPreviousPerformance(job.repository, job.running)
	if found && unread == "" && sha256Pattern.MatchString(previous.Tools.Binary) {
		mainBinary = previous.Tools.Binary
	}
	script, scriptError := withLandJobMirrors(performanceScript, job.mirrors)
	if scriptError == nil {
		// The land job's own setting, so the script reads the module set Loom laid for the job offline.
		script, scriptError = replacedOnce(script, landJobLine, landJobSetting)
	}
	if scriptError == nil {
		script, scriptError = withLandJobGit(script, performancePathLine, job.git)
	}
	if scriptError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", scriptError)
		return "", 1
	}
	started := time.Now()
	output, runError := runLandJobScript(job.scratch, script, performanceArguments(job.merge, mainBinary))
	tree, measured, measureCode := landMeasurement(job.repository, job.merge, job.mergeTree, job.running,
		performanceTaken{output: output, runError: runError, started: started, mainBinary: mainBinary})
	if measureCode != 0 {
		return "", measureCode
	}
	job.landedTree, job.measured = tree, measured
	if mainBinary != "" && tree != "" && strings.HasPrefix(measured, "Not measured: ") {
		job.say("measure skipped: %s", measured)
	}
	return measured, 0
}

// tests runs the module's tests here as a gate box runs them: macBoxScript, handed the packages the laid tree lists,
// through the land job's own bash, its mirrors the land job's and its Go cache the one landJobCacheVariable keeps, and
// reads the run as a gate box's (readBoxRun, verdictLine). share is the pool token's environment for a step.
func (job *landJobRun) tests(share func(string) []string) (string, int) {
	covered, readGatedWorkspaceError := readGatedWorkspace(job.tree)
	if readGatedWorkspaceError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading the modules to gate: %v\n", readGatedWorkspaceError)
		return "", 1
	}
	reportWorkspace(covered, streams.Errors)
	races := racePatterns(job.tree)
	listing, listError := listGatePackages(job.tree, covered.Patterns(), races,
		gateEnvironment(share("the test listing")))
	if listError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: the gate's packages couldn't be listed, so no test ran: %v\n",
			listError)
		return "", 1
	}
	if len(listing.packages) == 0 {
		fmt.Fprintln(streams.Errors, "cohere-dev: the gate's patterns list no package, so no test ran")
		return "", 1
	}
	script, scriptError := withLandJobMirrors(macBoxScript, job.mirrors)
	if scriptError == nil {
		// The land job's own setting, so the gate reads the module set Loom laid for the job offline, whether or not a
		// cache is kept: keyed on the kept cache alone, a job with none went to the proxy it can't reach.
		script, scriptError = replacedOnce(script, landJobLine, landJobSetting)
	}
	if scriptError == nil && job.cache != "" {
		script, scriptError = withLandJobCache(script, job.cache)
	}
	if scriptError == nil {
		script, scriptError = withLandJobGit(script, sharedBoxPathLine, job.git)
	}
	if scriptError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", scriptError)
		return "", 1
	}
	// Ignored: a tree with no TypeScript gitlink needs no pin, and the script skips an empty one.
	pin, _ := gitOutput(job.repository, "rev-parse", "--verify", "--quiet", job.merge+":TypeScript")
	arguments := append([]string{job.merge, pin, strings.Join(races, ",")}, listing.packages...)
	fmt.Fprintf(streams.Errors, "cohere-dev: testing %s here, %d packages, %s\n", shortCommit(job.merge),
		len(listing.packages), job.cacheSaid())
	streams.Show(streams.Errors)
	before := job.readCache()
	output, runError := runLandJobScript(job.scratch, script, arguments)
	job.sayCache(before, job.readCache())
	job.sayPackages(output)
	verdict, unavailable := readBoxRun(output, runError, landJobBox, job.merge, races)
	for _, step := range verdict.steps {
		job.say("step box %s", step)
	}
	if verdict.race != nil {
		for _, step := range verdict.race.steps {
			job.say("step box -race %s", step)
		}
	}
	if unavailable == "" {
		unavailable = verdict.shardRefusal()
	}
	if unavailable != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: no verdict from %s's own run, so nothing lands: %s\n", landJobBox,
			unavailable)
		return "", 1
	}
	code, where := verdictLine(job.tree, covered, verdict, "shared uid, "+job.cacheSaid())
	if code != 0 {
		return "", code
	}
	tested := testedNamed(covered) + " " + where
	job.ran = append(job.ran, tested)
	return tested, 0
}

// cacheSaid is how the land job's lines name the Go cache its tests ran on.
func (job *landJobRun) cacheSaid() string {
	if job.cache == "" {
		return "an empty Go cache with -count=1"
	}
	return "the land job's kept Go cache"
}

// landingLines are the land job's last lines, all Loom's finisher needs to make the landing on Kirk's Mac, in order:
// the performance file, base64, or `unchanged` when the tested tree is the merge's own; the tested tree's hash, the
// merge's tree with that file spliced in; the landing's whole message between its begin and end lines, its subject
// `Land #<task>: <what>`, its body the gate's account, the measurement and every step's seconds; and `landed <tree>`.
func (job *landJobRun) landingLines() (string, error) {
	var lines strings.Builder
	performance := "unchanged"
	if job.landedTree != job.mergeTree {
		reading := exec.Command("git", "cat-file", "blob", job.landedTree+":"+performanceFileName)
		reading.Dir = job.repository
		var readingErrors bytes.Buffer
		reading.Stderr = &readingErrors
		contents, readError := reading.Output()
		if readError != nil {
			return "", fmt.Errorf("reading the tested tree's %s: %w: %s", performanceFileName, readError,
				strings.TrimSpace(readingErrors.String()))
		}
		performance = base64.StdEncoding.EncodeToString(contents)
	}
	gated := fmt.Sprintf("Gated by cohere-dev land-job on %s in %s: %s. %s.", landJobBox,
		time.Since(job.started).Round(time.Second), strings.Join(job.ran, ", "), needs.SwiftUnlaid)
	account := gated + "\n\n" + job.measured + "\n\nSteps: " + strings.Join(job.steps, ", ") + "."
	message := landingMessage(job.subject, "", job.candidate, job.running, job.commits, account, "")
	fmt.Fprintf(&lines, "%sperformance %s\n", landJobMarker, performance)
	fmt.Fprintf(&lines, "%stree %s\n", landJobMarker, job.landedTree)
	fmt.Fprintf(&lines, "%smessage begin\n%s\n%smessage end\n", landJobMarker, strings.TrimRight(message, "\n"),
		landJobMarker)
	fmt.Fprintf(&lines, "landed %s\n", job.landedTree)
	return lines.String(), nil
}

// checks runs the gate's other checks here, each through the gate's own function and in its environment: go vet, the
// rule CPU budget, the unit isolation of the rules the landing changes, the docs data, replayed when its inputs match a
// pass the store recorded (landJobReplay), and cohere on the tree. share is the pool token's environment for a step.
func (job *landJobRun) checks(share func(string) []string) (string, int) {
	covered, readGatedWorkspaceError := readGatedWorkspace(job.tree)
	if readGatedWorkspaceError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading the modules to vet: %v\n", readGatedWorkspaceError)
		return "", 1
	}
	// timed runs one check, the account saying its seconds, and returns its exit code and what the gate's account
	// says of it, empty when it checked nothing.
	timed := func(name string, check func() (int, string)) (int, string) {
		started := time.Now()
		code, said := check()
		job.say("step %s %s", name, landJobSeconds(time.Since(started)))
		return code, said
	}
	checked := []string{}
	for _, check := range []struct {
		name string
		run  func() (int, string)
	}{
		{"vet", func() (int, string) {
			return goVet(job.tree, covered.Patterns(), gateEnvironment(share("go vet"))), "go vet"
		}},
		{"rule-cpu", func() (int, string) {
			code, ran := checkRuleCpu(job.tree,
				func() []string { return gateEnvironment(share("the rule CPU budget check")) })
			return code, map[bool]string{true: "the rule CPU budget"}[ran]
		}},
		{"isolation", func() (int, string) {
			code, ran, said := checkUnitIsolation(job.tree, job.merge,
				func() []string { return gateEnvironment(share("the unit isolation")) })
			return code, map[bool]string{true: said}[ran]
		}},
		{"docs-data", func() (int, string) {
			say := func(line string) { job.say("%s", line) }
			return landJobReplay{directory: job.cache, by: job.merge, say: say}.run("docs-data",
				docsDataPlaceholderKey, job.tree, func() (int, string) {
					docsCode, docsChecked := checkDocsData(job.tree,
						func() []string { return gateEnvironment(share("the docs data check")) })
					return docsCode, map[bool]string{true: "the docs data"}[docsChecked]
				})
		}},
		{"self-check", func() (int, string) {
			code, ran := checkCohere(job.tree, landJobCohereArguments,
				func() []string { return gateEnvironment(share("building cohere")) },
				func() []string { return gateEnvironment(share("cohere on the tree")) })
			return code, map[bool]string{true: "cohere on the tree, 0 findings and no gaps"}[ran]
		}},
	} {
		code, said := timed(check.name, check.run)
		if code != 0 {
			return "", code
		}
		if said != "" {
			checked = append(checked, said)
		}
	}
	job.ran = append(job.ran, checked...)
	return strings.Join(checked, ", "), 0
}

// withLandJobMirrors is script with its mirror line, macBoxMirrorLine, naming the land job's mirrors in place of the
// house's, read once, without waiting: the land job lays them itself just before.
func withLandJobMirrors(script string, mirrors string) (string, error) {
	return replacedOnce(script, macBoxMirrorLine, "mirrors="+shellQuoted(mirrors)+"; tries=1; pause=0")
}

// withLandJobGit is script with its path line, line, putting git's directory first, so what the script runs after it
// finds the git the land job runs (landJobGit) before the system's /usr/bin/git. Before that line the script reads the
// land job's own path, which holds the same directory first.
func withLandJobGit(script string, line string, git string) (string, error) {
	rest, found := strings.CutPrefix(line, "export PATH=")
	if !found {
		return "", fmt.Errorf("the land job's path line %q sets no PATH", line)
	}
	return replacedOnce(script, line, "export PATH="+shellQuoted(filepath.Dir(git)+string(os.PathListSeparator))+rest)
}

// withLandJobCache is macBoxScript with its line of no kept cache, macBoxLandCacheLine, keeping the land job's.
func withLandJobCache(script string, cache string) (string, error) {
	return replacedOnce(script, macBoxLandCacheLine, "landCache="+shellQuoted(cache))
}

// replacedOnce is script with its one line, line, replaced by replacement, or why it isn't: a script that holds the
// line other than once is not the one the land job was written for.
func replacedOnce(script string, line string, replacement string) (string, error) {
	if count := strings.Count(script, "\n"+line+"\n"); count != 1 {
		return "", fmt.Errorf("the land job's script holds the line %q %d times, not once", line, count)
	}
	return strings.Replace(script, "\n"+line+"\n", "\n"+replacement+"\n", 1), nil
}

// shellQuoted is value in single quotes, as bash reads it back whole.
func shellQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// runLandJobScript runs script with bash, or the stand-in landJobShellVariable names, handed arguments, in directory,
// and returns everything it printed and how it ended. It runs in the land job's own environment: as a box's gap would,
// it sets its own Go and leaves the account's unread.
func runLandJobScript(directory string, script string, arguments []string) ([]byte, error) {
	shell := "bash"
	if standIn := os.Getenv(landJobShellVariable); standIn != "" {
		shell = standIn
	}
	command := exec.Command(shell, append([]string{"-s", "--"}, arguments...)...)
	command.Dir, command.Stdin = directory, strings.NewReader(script)
	return command.CombinedOutput()
}
