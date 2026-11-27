package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/system-inc/cohere/internal/command_line/streams"
	release "github.com/system-inc/cohere/internal/release/packaging"
)

// cohere-dev integrate-job: the one command a Loom integrate job runs on Shackler to judge a merge Loom made for main
// (#etpnnw1, Kirk's design; its units #s50bcnr, a warm cache, #6ex523f, measurement first, and the seam with Loom,
// #hnbb5vr). It runs in the job's own tree, main's tip laid with no history, so its code is main's, and Loom runs one
// at a time, so it takes no land lock. It runs one git throughout, the developer tools' own (integrateJobGit), never
// macOS's shim. It reads the merge from the mirror integrateJobMirrorVariable names, refuses one whose first parent
// isn't the commit it runs at, and lays the merge's tree under its scratch. Each step says one line with its seconds,
// and the first refusal stops it:
//
//  1. Preflight: what no gate needs to run for, refused in seconds (integrateJobPreflight), and the laid resolved set
//     of cohere-swift's packages named for every child after, so Swift is built and checked here offline
//     (nameSwiftResolved).
//  2. Lay: the merge's tree and its TypeScript pin under the scratch.
//  3. Measure: performanceScript run here with bash, before any test, handed main's binary's sha256, so a merge that
//     builds main's binary keeps main's file and times nothing.
//  4. Tests: macBoxScript run here with bash and read by readBoxRun, so coverage, the shards, -race and the verdict
//     are the gate box's, on the Go cache integrateJobCacheVariable keeps, or an empty one.
//  5. Checks: go vet, the rule CPU budget, the unit isolation, the docs data and cohere on the tree, through the gate's
//     own functions, a step whose inputs a passed run keyed replaying it (integrateJobReplay).
//  6. Landing: nothing is committed or pushed. The last lines say what Loom's finisher needs to make the landing on
//     Kirk's Mac (landingLines): the performance file, the tested tree's hash, and the landing's whole message.
//
// With --fill it judges no merge but the commit it runs at, main's tip itself (#fnrchck, Loom's ruling): the job Loom's
// runner lets fill the shared Go cache does the same work at main an integration does, so its verdict compares with an
// integration's at the same tree step by step. Its preflight lays the mirrors, waits for the commit to reach them, and
// runs only what reads the tree, the gitlinks and the laid set; what judges a candidate has nothing new to judge
// (fillPreflight). It lays that commit's tree as a merge's is laid, measures nothing, its account saying so once, runs
// the tests and the checks as an integration does, and its last line is `filled <tree>`, with no finisher's lines,
// nothing committed or pushed.

// integrateJobCacheVariable is the integrate job's kept directory: its Go build and test cache, `gocache`, kept across
// jobs, and its replay store, `steps`. Unset, the integrate job's tests run on the Go cache its runner declared, with
// no -count=1 (runnerDeclaredCache, #e94a6r4), and with none declared as a gate box's do, on an empty cache with
// -count=1.
const integrateJobCacheVariable = "COHERE_INTEGRATE_CACHE"

// integrateJobShellVariable names a command run in place of bash for the integrate job's scripts, for a test: it is
// handed bash's arguments and the script on its standard input. Unset, bash runs them.
const integrateJobShellVariable = "COHERE_DEV_INTEGRATE_JOB_SHELL"

// integrateJobGitVariable names a git run in place of the one integrateJobGit finds, for a test. Unset, the integrate
// job finds it.
const integrateJobGitVariable = "COHERE_DEV_INTEGRATE_JOB_GIT"

// integrateJobMirrorVariable is the cohere mirror the integrate job reads the merge from, where Loom's bridge puts it;
// unset, it is integrateJobMirrorDefault. The TypeScript mirror beside it, TypeScript.git, holds every pin.
const (
	integrateJobMirrorVariable = "COHERE_INTEGRATE_MIRROR"
	integrateJobMirrorDefault  = macBoxMirrors + "/cohere.git"
)

// integrateJobSwiftResolvedRoot is where Machines lays the resolved sets of cohere-swift's packages on Shackler, each
// `<the sha256 of the Package.resolved it was resolved from>`, root's and read-only (#je1r32a), and
// integrateJobSwiftResolvedVariable names another root in its place, for a test.
const (
	integrateJobSwiftResolvedRoot     = "/opt/loom-tools/swiftpm-cohere"
	integrateJobSwiftResolvedVariable = "COHERE_DEV_INTEGRATE_JOB_SWIFT_RESOLVED"
)

// integrateJobMergeWaitVariable shortens, for a test, how long the integrate job waits for its merge to reach the
// mirror, as a Go duration. Unset, it waits integrateJobMergeWait.
const integrateJobMergeWaitVariable = "COHERE_DEV_INTEGRATE_JOB_MERGE_WAIT"

// integrateJobMergeWait is how long the integrate job waits for its --merge to appear in the mirror before it refuses:
// Loom's bridge may carry the merge a minute after the job starts, until the node's start waits on it (#hcae6f4).
// Loom's condition 2 on #hq61wfg sets it at 5 minutes.
const integrateJobMergeWait = 5 * time.Minute

// integrateJobMergePoll is how often the integrate job looks for its merge while it waits.
const integrateJobMergePoll = 5 * time.Second

// integrateJobCommitVariable is the commit the Loom job runs at, which Loom's runner sets in every job's environment.
// Unset, as in a plain checkout, the commit is the working directory's `HEAD`.
const integrateJobCommitVariable = "LOOM_JOB_COMMIT"

// integrateJobBox is the box an integrate job runs on, which its lines name: Shackler, who holds numbers of record.
const integrateJobBox = performanceBox

// integrateJobScratch is where the integrate job keeps its mirrors and its tree, under its home, beside the scripts'
// own scratch.
var integrateJobScratch = filepath.Join(strings.TrimPrefix(macBoxScratch, "~/"), "integrate-job")

// integrateJobMarker opens each of the integrate job's lines Loom's finisher reads. Its value is the finisher's grammar
// (LandFinishJudge.ts in loom), so it keeps the old word until it flips in the same window as the finisher's own
// rename (#je6yfxt).
const integrateJobMarker = "land-job: "

// fullCommit is a commit as `--merge` names it, whole.
var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// integrateJobOptions are what integrate-job is told: the merge Loom made, the task it finishes, and what it integrates
// when its commits don't say it in one subject; or, with Fill, that it judges the commit it runs at, filling.
type integrateJobOptions struct {
	Merge   string
	Task    string
	Message string
	Fill    bool
}

// parseIntegrateJobOptions reads `--merge <sha>`, `--task <id>` and `--message <what it integrates>`, each also as
// `--name=value`, the merge, whole, and the task required; or `--fill` and `--task <id>`, a fill taking no merge and no
// message, since it lands nothing.
func parseIntegrateJobOptions(arguments []string) (integrateJobOptions, error) {
	options := integrateJobOptions{}
	named := map[string]bool{}
	for index := 0; index < len(arguments); index++ {
		if arguments[index] == "--fill" {
			options.Fill = true
			continue
		}
		name, value, joined := strings.Cut(strings.TrimPrefix(arguments[index], "--"), "=")
		if !strings.HasPrefix(arguments[index], "--") || (name != "merge" && name != "task" && name != "message") {
			return integrateJobOptions{}, fmt.Errorf(
				"integrate-job takes --merge <sha>, --task <id>, --message <what "+
					"it integrates> and --fill, and %q is none of them",
				arguments[index],
			)
		}
		named[name] = true
		if !joined {
			if index+1 == len(arguments) {
				return integrateJobOptions{}, fmt.Errorf("--%s needs a value", name)
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
	if options.Fill {
		for _, name := range []string{"merge", "message"} {
			if named[name] {
				return integrateJobOptions{}, fmt.Errorf("integrate-job --fill judges the commit it runs at and lands "+
					"nothing, so it takes no --%s", name)
			}
		}
		if options.Task == "" {
			return integrateJobOptions{}, errors.New("integrate-job --fill needs --task <id>")
		}
		return options, nil
	}
	if !fullCommit.MatchString(options.Merge) || options.Task == "" {
		return integrateJobOptions{}, errors.New("integrate-job needs --merge <the merge's whole sha> and --task <id>")
	}
	return options, nil
}

// integrateJobRun is one integrate job as its steps carry it.
type integrateJobRun struct {
	options integrateJobOptions
	started time.Time
	// running is the commit the job runs at, main as the merge's first parent must be, and candidate its second. A fill
	// judges running itself, so merge is running and there is no candidate.
	running   string
	candidate string
	merge     string
	// mirror is the cohere mirror the merge is read from, and scratch the integrate job's own: its mirrors, borrowing
	// the house's, of which repository is cohere's, and tree, the merge's tree laid.
	mirror     string
	scratch    string
	mirrors    string
	repository string
	tree       string
	// commits are what the candidate brings to main, and subject the landing's, both read by the preflight.
	commits []branchCommit
	subject string
	// mergeTree is the merge's tree, and integratedTree the tree integrated, mergeTree with the performance file the
	// measurement wrote or kept.
	mergeTree      string
	integratedTree string
	// measured is what the measurement says in the landing's body, and ran what the gate ran, as its account says it.
	measured string
	ran      []string
	// cache is the integrate job's kept directory, empty when integrateJobCacheVariable is unset.
	cache string
	// git is the git every git the job starts runs (integrateJobGit), first on its own path and on its scripts'.
	git string
	// steps is each step that ended, `<step> <seconds>`, for the landing's body.
	steps []string
}

// integrateJob judges --merge and returns its exit code: 0 when it would integrate, its last lines what Loom's finisher
// needs and the very last `integrated <tree>`, and 1 when a step refused, the reason in its last lines.
func integrateJob(arguments []string) int {
	options, parseError := parseIntegrateJobOptions(arguments)
	if parseError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", parseError)
		return 2
	}
	// The git is found before anything runs one, and its directory goes first on the job's own path, so every git it
	// starts, its own, its packages' and its children's, is that one, and the account's path is otherwise as it was.
	git, integrateJobGitError := integrateJobGit()
	if integrateJobGitError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", integrateJobGitError)
		return 1
	}
	path := filepath.Dir(git) + string(os.PathListSeparator) + os.Getenv("PATH")
	if setError := os.Setenv("PATH", path); setError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: putting %s first on the integrate job's path: %v\n", filepath.Dir(git),
			setError)
		return 1
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: integrate-job runs the git at %s\n", git)
	job := &integrateJobRun{options: options, started: time.Now(), merge: options.Merge, git: git,
		cache:  strings.TrimSpace(os.Getenv(integrateJobCacheVariable)),
		mirror: settingOr(integrateJobMirrorVariable, integrateJobMirrorDefault)}
	if job.cache != "" && !filepath.IsAbs(job.cache) {
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: %s=%q is no directory's whole path\n",
			integrateJobCacheVariable,
			job.cache,
		)
		return 2
	}
	job.running = strings.TrimSpace(os.Getenv(integrateJobCommitVariable))
	if job.running == "" {
		// Ignored: a working directory with no history has no `HEAD`, which the refusal below says.
		job.running, _ = gitOutput("", "rev-parse", "--verify", "HEAD^{commit}")
	}
	if job.running == "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: integrate-job can't tell the commit it runs at: %s is unset, and the "+
			"working directory has no HEAD\n", integrateJobCommitVariable)
		return 1
	}
	if options.Fill {
		job.merge = job.running
	}
	home, homeError := os.UserHomeDir()
	if homeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: finding the integrate job's home for its scratch: %v\n", homeError)
		return 1
	}
	// The pool hangs from the account's home in the system's user database, never `HOME` (poolHome), so that a gate
	// with a scratch `HOME` still shares the machine's pool. An integrate job runs alone in an account of its own,
	// whose home in that database is /var/empty in Shackler's sandbox (J-5xseq90: "mkdir /var/empty/Library"), so its
	// pool hangs from the job's own home, unless one is named.
	if os.Getenv(homeVariable) == "" {
		if setError := os.Setenv(homeVariable, home); setError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: hanging the integrate job's pool from its home %s: %v\n", home,
				setError)
			return 1
		}
	}
	// An integrate job has no network, so when Loom hands it a laid module set in `GOMODCACHE`, its own go commands
	// read that set with the proxy and the checksum database off, as its scripts do: the test listing, go vet, the rule
	// CPU budget, the unit isolation, the docs data, cohere's build and cohere's own go children. Each then fails
	// naming a module the set lacks, never waiting on a network it can't reach (#5wj23dd).
	if strings.TrimSpace(os.Getenv("GOMODCACHE")) != "" {
		for _, setting := range [][2]string{{"GOPROXY", "off"}, {"GOSUMDB", "off"}} {
			if setError := os.Setenv(setting[0], setting[1]); setError != nil {
				fmt.Fprintf(streams.Errors, "cohere-dev: setting %s=%s for the integrate job's laid modules: %v\n",
					setting[0], setting[1], setError)
				return 1
			}
		}
	}
	job.scratch = filepath.Join(home, integrateJobScratch)
	job.mirrors = filepath.Join(job.scratch, "mirrors")
	job.repository = filepath.Join(job.mirrors, "cohere.git")
	job.tree = filepath.Join(job.scratch, "tree")
	if makeError := os.MkdirAll(job.scratch, 0o755); makeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: making the integrate job's scratch: %v\n", makeError)
		return 1
	}
	job.sayDisk("before")
	code := job.integrate()
	job.say("step total %s", integrateJobSeconds(time.Since(job.started)))
	job.sayDisk("after")
	if code != 0 {
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: integrate-job refused after %s, so nothing %s\n",
			integrateJobSeconds(time.Since(job.started)),
			map[bool]string{false: "integrates", true: "fills"}[options.Fill],
		)
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

// integrateJobGit is the git the integrate job runs: the one integrateJobGitVariable names, or the real one realGit
// finds here. Its directory goes on a path, so it is refused when it holds the path's separator.
func integrateJobGit() (string, error) {
	git := strings.TrimSpace(os.Getenv(integrateJobGitVariable))
	if git != "" && !filepath.IsAbs(git) {
		return "", fmt.Errorf("%s=%q is no git's whole path", integrateJobGitVariable, git)
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
			"directory for the other (%w), so integrate-job refuses rather than run %s, whose xcrun can't write its "+
			"cache in a job's sandbox", commandLineTools, developerError, shimGit)
	}
	xcode := filepath.Join(root, developer, "usr", "bin", "git")
	if isRealGit(xcode, shim) {
		return xcode, nil
	}
	return "", fmt.Errorf("no git of the developer tools is at %s or %s, so integrate-job refuses rather than run %s, "+
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
// is no xcrun shim, so it runs anywhere the integrate job does.
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

// integrate runs the job's steps in order and returns the exit code. A fill runs them but the measurement, which its
// account says once it skipped, and ends on `filled <tree>`, the laid tree, never the finisher's lines.
func (job *integrateJobRun) integrate() int {
	steps := []struct {
		name string
		work func() (string, int)
	}{
		{"preflight", job.preflight},
		{"lay", job.lay},
		{"measure", job.measure},
	}
	if job.options.Fill {
		steps = steps[:2]
		steps[0].work = job.fillPreflight
	}
	for _, step := range steps {
		if code := job.step(step.name, step.work); code != 0 {
			return code
		}
	}
	if job.options.Fill {
		job.integratedTree = job.mergeTree
		job.say("measure skipped: a fill measures nothing")
	}
	// The tests and the checks run through one pool token, each taking its share as the pool is when it starts.
	code := withSteppedToken(map[bool]string{false: "integrate job of ", true: "fill job at "}[job.options.Fill]+
		shortCommit(job.merge), func(share func(string) []string) int {
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
	if job.options.Fill {
		streams.Show(streams.Errors)
		fmt.Fprintf(streams.Output, "filled %s\n", job.mergeTree)
		return 0
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
func (job *integrateJobRun) step(name string, work func() (string, int)) int {
	started := time.Now()
	said, code := work()
	spent := integrateJobSeconds(time.Since(started))
	job.say("step %s %s", name, spent)
	if code != 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: integrate-job step %s refused in %s\n", name, spent)
		streams.Show(streams.Errors)
		if code == quietRefusedExitCode {
			return code
		}
		return 1
	}
	job.steps = append(job.steps, name+" "+spent)
	fmt.Fprintf(streams.Errors, "cohere-dev: integrate-job step %s %s: %s\n", name, spent, said)
	streams.Show(streams.Errors)
	return 0
}

// awaitMerge waits, up to integrateJobMergeWait, for the merge to reach the job's repository, which borrows the mirror,
// and refuses once it hasn't, saying the mirror held no merge: the bridge's carry is the road's, so the refusal names
// it and never reads as a red of the merge. A merge already there costs one look. A fill waits so for the commit it
// runs at, which its words name a commit.
func (job *integrateJobRun) awaitMerge() int {
	judged := map[bool]string{false: "merge", true: "commit"}[job.options.Fill]
	wait := integrateJobMergeWait
	if setting := strings.TrimSpace(os.Getenv(integrateJobMergeWaitVariable)); setting != "" {
		parsed, parseError := time.ParseDuration(setting)
		if parseError != nil || parsed <= 0 {
			fmt.Fprintf(
				streams.Errors,
				"cohere-dev: %s=%q is no positive duration\n",
				integrateJobMergeWaitVariable,
				setting,
			)
			return 2
		}
		wait = parsed
	}
	poll := min(integrateJobMergePoll, wait/10)
	started := time.Now()
	for said := false; ; said = true {
		if _, lookError := gitOutput(job.repository, "cat-file", "-e", job.merge+"^{commit}"); lookError == nil {
			return 0
		}
		if time.Since(started) >= wait {
			fmt.Fprintf(streams.Errors, "cohere-dev: %s held no %s %s after %s of waiting: Loom's bridge hasn't "+
				"carried it there, which is the road's to fix and never a red of this %s (#hq61wfg)\n", job.mirror,
				judged, job.merge, wait, judged)
			return 1
		}
		if !said {
			fmt.Fprintf(streams.Errors, "cohere-dev: integrate-job waits up to %s for the %s %s to reach %s\n", wait,
				judged, shortCommit(job.merge), job.mirror)
			streams.Show(streams.Errors)
		}
		time.Sleep(poll)
	}
}

// integrateJobSeconds is a step's time as its line says it, in seconds to a tenth.
func integrateJobSeconds(spent time.Duration) string {
	return fmt.Sprintf("%.1fs", spent.Seconds())
}

// preflight reaches the merge in the integrate job's mirrors (reachMerge), reads it, refuses one whose first parent
// isn't the commit the job runs at, and runs integrateJobPreflight on its parents.
func (job *integrateJobRun) preflight() (string, int) {
	if code := job.reachMerge(); code != 0 {
		return "", code
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
	message := job.options.Message
	if message == "" {
		// On the PR road no --message is given, and GitHub's merge group commit carries the pull request's title.
		mergeMessage, messageError := gitOutput(job.repository, "log", "-1", "--format=%B", job.merge)
		if messageError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: reading the merge's message: %v\n", messageError)
			return "", 1
		}
		title, refusal := pullRequestTitle(mergeMessage, job.options.Task)
		if refusal != "" {
			fmt.Fprintf(streams.Errors, "cohere-dev: %s\n", refusal)
			return "", 1
		}
		message = title
	}
	commits, subject, refusal, preflightError := integrateJobPreflight(job.repository, job.running, job.candidate,
		job.merge, landingOptions{Task: job.options.Task, Message: message})
	if preflightError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", preflightError)
		return "", 1
	}
	if refusal != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s\n", refusal)
		return "", 1
	}
	if code := job.nameSwiftResolved(); code != 0 {
		return "", code
	}
	job.commits, job.subject = commits, subject
	return subject + ", " + shortCommit(job.candidate) + " merged onto main at " + shortCommit(job.running) + " as " +
		shortCommit(job.merge), 0
}

// fillPreflight is a fill's first step: the commit the job runs at reached in the mirrors as a merge is (reachMerge),
// and of integrateJobPreflight's checks the one that reads only the tree, its submodules recorded as gitlinks
// (checkGitlinks), and the laid set its tests build cohere-swift from (nameSwiftResolved). The rest judge a candidate,
// and a fill has none, main's tip being already landed: the merge's two parents, main holding the candidate
// (heldByMain), the commits' subjects and their task, and the landing's subject and --message.
func (job *integrateJobRun) fillPreflight() (string, int) {
	if code := job.reachMerge(); code != 0 {
		return "", code
	}
	if checkGitlinksError := checkGitlinks(job.repository, job.merge); checkGitlinksError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", checkGitlinksError)
		return "", 1
	}
	if code := job.nameSwiftResolved(); code != 0 {
		return "", code
	}
	return "main at " + shortCommit(job.merge) + ", no candidate to judge", 0
}

// swiftResolvedFile is the file of a tree whose bytes name the laid resolved set its Swift package builds from.
const swiftResolvedFile = "swift_language/Package.resolved"

// nameSwiftResolved finds the laid resolved set of cohere-swift's packages for what the job judges, the directory
// under integrateJobSwiftResolvedRoot named by the sha256 of its swiftResolvedFile, and names it in
// release.SwiftResolvedVariable for every child the job starts after: the measurement, the tests' script and the
// checks, cohere on the tree among them. Each SwiftPM build of theirs then builds offline from it
// (release.SwiftResolvedArguments), which is how Swift is checked in a sandbox with no network. The bytes hashed are
// the blob, the ones the lay checks out, every text file LF in git and in the tree alike (.gitattributes). A tree with
// no such file has no Swift package to build, and names none. A set that isn't laid is refused naming the sha: the lay
// is Machines' to make (#je1r32a), which is the road's to fix and never a red of what the job judges.
func (job *integrateJobRun) nameSwiftResolved() int {
	judged := map[bool]string{false: "merge", true: "commit"}[job.options.Fill]
	// Ignored: a tree with no swiftResolvedFile has no Swift package to build, and the read below says why it failed if
	// one is there.
	if blob, _ := gitOutput(job.repository, "rev-parse", "--verify", "--quiet",
		job.merge+":"+swiftResolvedFile); blob == "" {
		return 0
	}
	// Read whole, never through gitOutput, which trims what it reads, so the hash is of the file's own bytes.
	reading := exec.Command("git", "cat-file", "blob", job.merge+":"+swiftResolvedFile)
	reading.Dir = job.repository
	var readingErrors bytes.Buffer
	reading.Stderr = &readingErrors
	contents, readError := reading.Output()
	if readError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading the %s's %s: %v: %s\n", judged, swiftResolvedFile, readError,
			strings.TrimSpace(readingErrors.String()))
		return 1
	}
	digest := sha256.Sum256(contents)
	sha := hex.EncodeToString(digest[:])
	set := filepath.Join(settingOr(integrateJobSwiftResolvedVariable, integrateJobSwiftResolvedRoot), sha)
	if information, statError := os.Stat(filepath.Join(set, "scratch", "checkouts")); statError != nil ||
		!information.IsDir() {
		fmt.Fprintf(streams.Errors, "cohere-dev: no resolved set of cohere-swift's packages is laid at %s for the "+
			"%s's %s, sha256 %s, so its Swift can't be built offline here: Machines' lay owes it (#je1r32a), which is "+
			"the road's to fix and never a red of this %s\n", set, judged, swiftResolvedFile, sha, judged)
		return 1
	}
	if setError := os.Setenv(release.SwiftResolvedVariable, set); setError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: naming the laid set %s in %s for the integrate job's children: %v\n",
			set, release.SwiftResolvedVariable, setError)
		return 1
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: integrate-job builds cohere-swift from the laid set %s\n", set)
	return 0
}

// reachMerge lays the integrate job's mirrors, borrowing the house's, waits for what it judges, the merge or a fill's
// commit, to reach them (awaitMerge), keeps it on a ref of the job's own, so the tree laid from it is never left
// dangling, and reads its tree.
func (job *integrateJobRun) reachMerge() int {
	if layError := layIntegrateJobMirrors(filepath.Join(job.mirror, "objects"), job.mirrors,
		[]string{filepath.Join(filepath.Dir(job.mirror), "TypeScript.git", "objects")}); layError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: laying the integrate job's mirrors in %s: %v\n", job.mirrors, layError)
		return 1
	}
	if code := job.awaitMerge(); code != 0 {
		return code
	}
	if _, updateError := gitOutput(
		job.repository,
		"update-ref",
		"refs/integrate-job/merge",
		job.merge,
	); updateError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", updateError)
		return 1
	}
	mergeTree, treeError := gitOutput(job.repository, "rev-parse", job.merge+"^{tree}")
	if treeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", treeError)
		return 1
	}
	job.mergeTree = mergeTree
	return 0
}

// integrateJobPreflight is the integrate job's first step, every refusal it can make without a gate, in seconds: what
// head brings to main at mainCommit, merged as merge, and the landing's subject as options make it, or the refusal in
// words a submitter can act on, or the error of a git it couldn't read with.
//
// The preflight's call site (#mf8t6se): land --preflight's preflight is being built beside this, and once it is on main
// this body is `preflight(repository, mainCommit, head, options)`, its reading's Commits and Subject returned. Until
// then it runs today's checks as land runs them before its line: main already holding head; the subjects of the commits
// it brings; its task and the landing's subject; and the merge recording each submodule as a gitlink.
func integrateJobPreflight(
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
		return nil, "", "nothing to integrate, and no gate runs: " + held, nil
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

// pullRequestMergeSubject is the first line GitHub writes on a merge group's commit, naming the pull request it merges.
var pullRequestMergeSubject = regexp.MustCompile(`^Merge pull request #([0-9]+) from \S+$`)

// pullRequestTaskSuffix is the task a pull request's title names at its end, `(#<id>)`, as Zach's check holds every
// title to.
var pullRequestTaskSuffix = regexp.MustCompile(`\s*\(#([0-9a-z]+)\)$`)

// pullRequestTitleAllowed is the punctuation a pull request's title may carry into a landing's subject beside letters,
// digits and spaces (Loom's ruling as the trust seat on #hq61wfg).
const pullRequestTitleAllowed = ",.:;()'/_-+"

// pullRequestTitle is what landed, read from message, a merge's whole message, when it is a merge group's commit:
// GitHub's `Merge pull request #<n> from <branch>`, a blank line, then the pull request's title (#hq61wfg, Jobs' road
// as Loom ruled it). The title's trailing `(#<task>)` is dropped once it names task, the one --task says, and the rest
// is held to one line of letters, digits, spaces and pullRequestTitleAllowed, then cut at a word so that `Land
// #<task>: <title>` fits subjectLimit. A message of no merge group is no title, empty and no refusal, and the landing
// says what landed as before. A title that breaks a rule is refused naming the pull request's number and the rule,
// never its words, since a title is anyone's text; and it is only ever read as a value, never through a shell.
func pullRequestTitle(message string, task string) (string, string) {
	lines := strings.Split(strings.TrimRight(message, "\n"), "\n")
	named := pullRequestMergeSubject.FindStringSubmatch(lines[0])
	if named == nil {
		return "", ""
	}
	number := named[1]
	if len(lines) != 3 || lines[1] != "" {
		return "", fmt.Sprintf("pull request #%s's merge carries %d lines, not its subject, a blank line and its "+
			"title, so its title can't be read", number, len(lines))
	}
	title := strings.TrimSpace(lines[2])
	if suffix := pullRequestTaskSuffix.FindStringSubmatch(title); suffix != nil {
		if suffix[1] != task {
			return "", fmt.Sprintf("pull request #%s's title names a task that isn't #%s, the one this job judges",
				number, task)
		}
		title = strings.TrimSpace(strings.TrimSuffix(title, suffix[0]))
	}
	for offset, character := range title {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == ' ' ||
			strings.ContainsRune(pullRequestTitleAllowed, character) {
			continue
		}
		return "", fmt.Sprintf("pull request #%s's title holds a character outside letters, digits, spaces and %s at "+
			"byte %d, so it's refused and its words aren't repeated", number, pullRequestTitleAllowed, offset)
	}
	if title == "" {
		return "", fmt.Sprintf("pull request #%s's title says nothing beside its task", number)
	}
	room := subjectLimit - len([]rune("Land #"+task+": "))
	if words := strings.Fields(title); len([]rune(title)) > room {
		cut := ""
		for _, word := range words {
			if len([]rune(cut))+1+len([]rune(word)) > room && cut != "" {
				break
			}
			cut = strings.TrimSpace(cut + " " + word)
		}
		title = cut
	}
	if len([]rune(title)) > room {
		return "", fmt.Sprintf("pull request #%s's title opens with a word too long for the landing's subject's %d "+
			"characters", number, subjectLimit)
	}
	return title, ""
}

// lay checks the merge's tree out under the integrate job's scratch, a worktree of its mirror kept across jobs, with
// its TypeScript at the pin it records.
func (job *integrateJobRun) lay() (string, int) {
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
	// Every tracked file's time is its content's (stampTreeTimes), so a test whose inputs this merge didn't change
	// replays from the Go cache in a job that laid the tree fresh, as each does (#e94a6r4).
	stamped := []string{job.tree}
	if pin != "" {
		stamped = append(stamped, filepath.Join(job.tree, "TypeScript"))
	}
	left := []string{}
	for _, tree := range stamped {
		unstamped, stampError := stampTreeTimes(tree)
		if stampError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: stamping %s's times by content: %v\n", tree, stampError)
			return "", 1
		}
		for _, path := range unstamped {
			left = append(left, filepath.Join(tree, path))
		}
	}
	said := shortCommit(job.merge) + " at " + job.tree + ", its times by content"
	if len(left) > 0 {
		said += fmt.Sprintf(" but for %d that differ from its HEAD, left their own: %s", len(left),
			strings.Join(left, ", "))
	}
	return said, 0
}

// layTypeScript checks tree's TypeScript out at pin, a worktree of the integrate job's TypeScript mirror kept across
// jobs.
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

// layIntegrateJobMirrors makes the mirrors the integrate job's scripts read in place of the house's (macBoxMirrors), in
// directory: cohere.git, a bare repository borrowing cohereObjects, the cohere mirror's, through git's alternates, and
// TypeScript.git, borrowing each of typeScriptObjects that exists. Nothing is copied. Both are kept across jobs, and
// each job writes their borrowing anew.
func layIntegrateJobMirrors(cohereObjects string, directory string, typeScriptObjects []string) error {
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
// from main's performance file, through the integrate job's own bash, read as a landing on the gap reads it. A merge
// that builds main's binary keeps main's file, the script having timed nothing.
func (job *integrateJobRun) measure() (string, int) {
	said, measurable, code := cohereToMeasure(job.repository, job.merge)
	if !measurable {
		job.integratedTree, job.measured = job.mergeTree, said
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
	script, scriptError := withIntegrateJobMirrors(performanceScript, job.mirrors)
	if scriptError == nil {
		// The integrate job's own setting, so the script reads the module set Loom laid for the job offline.
		script, scriptError = replacedOnce(script, integrateJobLine, integrateJobSetting)
	}
	if scriptError == nil {
		script, scriptError = withIntegrateJobGit(script, performancePathLine, job.git)
	}
	if scriptError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", scriptError)
		return "", 1
	}
	started := time.Now()
	output, runError := runIntegrateJobScript(job.scratch, script, performanceArguments(job.merge, mainBinary))
	tree, measured, measureCode := landMeasurement(job.repository, job.merge, job.mergeTree, job.running,
		performanceTaken{output: output, runError: runError, started: started, mainBinary: mainBinary})
	if measureCode != 0 {
		return "", measureCode
	}
	job.integratedTree, job.measured = tree, measured
	if mainBinary != "" && tree != "" && strings.HasPrefix(measured, "Not measured: ") {
		job.say("measure skipped: %s", measured)
	}
	return measured, 0
}

// tests runs the module's tests here as a gate box runs them: macBoxScript, handed the packages the laid tree lists,
// through the integrate job's own bash, its mirrors the integrate job's and its Go cache the one
// integrateJobCacheVariable keeps, and reads the run as a gate box's (readBoxRun, verdictLine). share is the pool
// token's environment for a step.
func (job *integrateJobRun) tests(share func(string) []string) (string, int) {
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
	script, scriptError := withIntegrateJobMirrors(macBoxScript, job.mirrors)
	if scriptError == nil {
		// The integrate job's own setting, so the gate reads the module set Loom laid for the job offline, whether or
		// not a cache is kept: keyed on the kept cache alone, a job with none went to the proxy it can't reach.
		script, scriptError = replacedOnce(script, integrateJobLine, integrateJobSetting)
	}
	if scriptError == nil && job.cache != "" {
		script, scriptError = withIntegrateJobCache(script, job.cache)
	}
	if scriptError == nil {
		script, scriptError = withIntegrateJobGit(script, sharedBoxPathLine, job.git)
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
	output, runError := runIntegrateJobScript(job.scratch, script, arguments)
	job.sayCache(before, job.readCache())
	job.sayPackages(output)
	verdict, unavailable := readBoxRun(output, runError, integrateJobBox, job.merge, races)
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
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: no verdict from %s's own run, so nothing integrates: %s\n",
			integrateJobBox,
			unavailable,
		)
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

// runnerDeclaredCache reports whether the job's runner declared a Go cache, its layer and its program, which
// sharedBoxCacheBlock then runs the tests on when no kept cache is named (#e94a6r4).
func runnerDeclaredCache() bool {
	return strings.TrimSpace(os.Getenv("GOCACHEPROG")) != "" && strings.TrimSpace(os.Getenv("GOCACHE")) != ""
}

// cacheSaid is how the integrate job's lines name the Go cache its tests ran on.
func (job *integrateJobRun) cacheSaid() string {
	switch {
	case job.cache != "":
		return "the integrate job's kept Go cache"
	case runnerDeclaredCache():
		return "the runner's declared Go cache"
	}
	return "an empty Go cache with -count=1"
}

// landingLines are the integrate job's last lines, all Loom's finisher needs to make the landing on Kirk's Mac, in
// order: the performance file, base64, or `unchanged` when the tested tree is the merge's own; the tested tree's hash,
// the merge's tree with that file spliced in; the landing's whole message between its begin and end lines, its subject
// `Land #<task>: <what>`, its body the gate's account, the measurement and every step's seconds; and last
// `integrated <tree>`.
func (job *integrateJobRun) landingLines() (string, error) {
	var lines strings.Builder
	performance := "unchanged"
	if job.integratedTree != job.mergeTree {
		reading := exec.Command("git", "cat-file", "blob", job.integratedTree+":"+performanceFileName)
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
	gated := fmt.Sprintf("Gated by cohere-dev integrate-job on %s in %s: %s.", integrateJobBox,
		time.Since(job.started).Round(time.Second), strings.Join(job.ran, ", "))
	account := gated + "\n\n" + job.measured + "\n\nSteps: " + strings.Join(job.steps, ", ") + "."
	message := landingMessage(job.subject, "", job.candidate, job.running, job.commits, account, "")
	fmt.Fprintf(&lines, "%sperformance %s\n", integrateJobMarker, performance)
	fmt.Fprintf(&lines, "%stree %s\n", integrateJobMarker, job.integratedTree)
	fmt.Fprintf(&lines, "%smessage begin\n%s\n%smessage end\n", integrateJobMarker, strings.TrimRight(message, "\n"),
		integrateJobMarker)
	fmt.Fprintf(&lines, "integrated %s\n", job.integratedTree)
	return lines.String(), nil
}

// checks runs the gate's other checks here, each through the gate's own function and in its environment: go vet, the
// rule CPU budget, the unit isolation of the rules the landing changes, the docs data, replayed when its inputs match a
// pass the store recorded (integrateJobReplay), and cohere on the tree. share is the pool token's environment for a
// step.
func (job *integrateJobRun) checks(share func(string) []string) (string, int) {
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
		job.say("step %s %s", name, integrateJobSeconds(time.Since(started)))
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
			return integrateJobReplay{directory: job.cache, by: job.merge, say: say}.run("docs-data",
				docsDataPlaceholderKey, job.tree, func() (int, string) {
					docsCode, docsChecked := checkDocsData(job.tree,
						func() []string { return gateEnvironment(share("the docs data check")) })
					return docsCode, map[bool]string{true: "the docs data"}[docsChecked]
				})
		}},
		{"self-check", func() (int, string) {
			code, ran := checkCohere(job.tree, gateCohereArguments,
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

// withIntegrateJobMirrors is script with its mirror line, macBoxMirrorLine, naming the integrate job's mirrors in place
// of the house's, read once, without waiting: the integrate job lays them itself just before.
func withIntegrateJobMirrors(script string, mirrors string) (string, error) {
	return replacedOnce(script, macBoxMirrorLine, "mirrors="+shellQuoted(mirrors)+"; tries=1; pause=0")
}

// withIntegrateJobGit is script with its path line, line, putting git's directory first, so what the script runs after
// it finds the git the integrate job runs (integrateJobGit) before the system's /usr/bin/git. Before that line the
// script reads the integrate job's own path, which holds the same directory first.
func withIntegrateJobGit(script string, line string, git string) (string, error) {
	rest, found := strings.CutPrefix(line, "export PATH=")
	if !found {
		return "", fmt.Errorf("the integrate job's path line %q sets no PATH", line)
	}
	return replacedOnce(script, line, "export PATH="+shellQuoted(filepath.Dir(git)+string(os.PathListSeparator))+rest)
}

// withIntegrateJobCache is macBoxScript with its line of no kept cache, macBoxIntegrateCacheLine, keeping the integrate
// job's.
func withIntegrateJobCache(script string, cache string) (string, error) {
	return replacedOnce(script, macBoxIntegrateCacheLine, "integrateCache="+shellQuoted(cache))
}

// replacedOnce is script with its one line, line, replaced by replacement, or why it isn't: a script that holds the
// line other than once is not the one the integrate job was written for.
func replacedOnce(script string, line string, replacement string) (string, error) {
	if count := strings.Count(script, "\n"+line+"\n"); count != 1 {
		return "", fmt.Errorf("the integrate job's script holds the line %q %d times, not once", line, count)
	}
	return strings.Replace(script, "\n"+line+"\n", "\n"+replacement+"\n", 1), nil
}

// shellQuoted is value in single quotes, as bash reads it back whole.
func shellQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// runIntegrateJobScript runs script with bash, or the stand-in integrateJobShellVariable names, handed arguments, in
// directory, and returns everything it printed and how it ended. It runs in the integrate job's own environment: as a
// box's gap would, it sets its own Go and leaves the account's unread.
func runIntegrateJobScript(directory string, script string, arguments []string) ([]byte, error) {
	shell := "bash"
	if standIn := os.Getenv(integrateJobShellVariable); standIn != "" {
		shell = standIn
	}
	command := exec.Command(shell, append([]string{"-s", "--"}, arguments...)...)
	command.Dir, command.Stdin = directory, strings.NewReader(script)
	return command.CombinedOutput()
}
