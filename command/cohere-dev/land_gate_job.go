package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/streams"
)

// A landing's tests can run as a Loom job of the land-gate kind, in place of a gap on a gate box, when gateVariable
// says landGateKindSetting. Loom places the job on a scratch box at the commit being landed, and the box runs the same
// sharedBoxGate a gate box runs, so its output is read the same way and holds to the same checks.
//
// No verdict from the job refuses the landing and never sends the tests to Kirk's Mac (#1v2shq7, item 2): a job that
// timed out or met Loom's or the machine's failure says nothing of the tree, and a Mac that took the run back each time
// would carry the load the kind exists to take off it.
//
// ahra's `loom wait job` is gone, so the land reads the job's row, `loom job <id> --json`, every landGateReadEvery
// until its state is Ended, for at most landGateWithin, and reads its end from the row's outcome (#rr818c8).

// landGateKindSetting is the gateVariable setting that runs a landing's tests as a land-gate job.
const landGateKindSetting = "kind"

// loomStandInVariable names a command run in place of ahra for a test, called with ahra's own arguments: the gate box's
// push (`loom push --feed <sha> ...`) and gap (`loom gap --box <Mac> ...`), and the land-gate job's ask (`loom job
// <command> ...`), row (`loom job <id> --json`) and log (`loom job <id> --log`). Unset, the house's ahra runs.
const loomStandInVariable = "COHERE_DEV_LOOM"

// landGateKind is the Loom job kind a landing's tests run as, landGateWithin how long the land waits on it, its time
// in line included, and landGateReadEvery how often it reads the job's row meanwhile.
const (
	landGateKind      = "land-gate"
	landGateWithin    = 2 * time.Hour
	landGateReadEvery = 30 * time.Second
)

// landGateUnreadMost is how many reads of the job's row in a row may fail before the land gives up on it: one blip of
// Zach's two hours into a wait in line would otherwise throw the wait away.
const landGateUnreadMost = 5

// landGateLayKitAt is the lay kit in loom's suite, by the commit it is pinned at and its path there.
const landGateLayKitAt = "fa30c7fada620692ef689df61b95a003e9c6327c:contract/proof/lay.sh"

// landGateLayKit opens the job's command: it lays the commit's submodules from the box's own mirrors, TypeScript among
// them, then runs what follows its `--`. The kit is read at a pinned commit of loom's suite, never a branch, so no push
// to it changes what a landing's job runs (#697bz4z), and a box without that commit says so as `FAIL lay:` with exit 3,
// which the Mac reads as the box's gap, never the tree's red (#adbkt5t).
const landGateLayKit = `git -C ~/loom-suite.git cat-file -e ` + landGateLayKitAt +
	` || { echo "FAIL lay: kit not on this box"; exit 3; }; bash <(git -C ~/loom-suite.git show ` + landGateLayKitAt +
	`) -- `

// landGateLayFailure opens the line the lay kit prints when the box could not lay the tree.
const landGateLayFailure = "FAIL lay:"

// landGateLayExitCode is the lay kit's exit when the box could not lay the tree.
const landGateLayExitCode = 3

// landGateScript is the box's run once the tree is laid: sharedBoxGate, unchanged, with its goroot, digest and races
// taken from its first three arguments and the module's test patterns from the rest.
const landGateScript = "set -euo pipefail\n" + `goroot="$1"; digest="$2"; races="$3"; shift 3` + "\n" +
	`node="` + sharedBoxNode + `"; nodeDigest="` + sharedBoxNodeDigest + `"` + "\n" + sharedBoxGate

// landGateJobId reads the id from the ask's line, `J-6nrpbay queued, 1st in line, 0 ahead`. Any state is taken, so a
// job placed at once is still waited on rather than left running with no one to hear it.
var landGateJobId = regexp.MustCompile(`(?m)^(J-[0-9a-z]{7}) [a-z]+`)

// landGateEnds is what each outcome of an ended job's row but Exit says, every one of them no verdict, as the wait's
// exits 200 to 210 said them. Infra is the name a Zach before the rename to Infrastructure sends (LoomJobsApi.ts).
var landGateEnds = map[string]string{
	"Cancelled":      "it was cancelled",
	"Superseded":     "it was superseded",
	"Orphaned":       "it was orphaned",
	"BlockerEnded":   "its blocker ended",
	"Infrastructure": "it met Loom's or the machine's failure, never the tree's (infrastructure)",
	"Infra":          "it met Loom's or the machine's failure, never the tree's (infrastructure)",
	"Refused":        "Loom refused it before anything ran",
}

// landGateRow is what the land reads of a job's row, `loom job <id> --json`.
type landGateRow struct {
	State           string  `json:"state"`
	Outcome         *string `json:"outcome"`
	OutcomeWhy      *string `json:"outcomeWhy"`
	Exit            *int    `json:"exit"`
	CancelledBy     *string `json:"cancelledBy"`
	CancelledReason *string `json:"cancelledReason"`
}

// landGateEnded is the state of a job's row once it has ended, any way.
const landGateEnded = "Ended"

// waitForLandGateJob reads job's row through read every landGateReadEvery, sleeping through sleep, until it has
// ended, and returns its exit, or why it gave no verdict: an end that isn't its command's exit, rows that didn't read,
// or landGateWithin passing, measured by since.
func waitForLandGateJob(
	job string,
	read func() ([]byte, error),
	sleep func(time.Duration),
	since func() time.Duration,
) (int, string) {
	unread := 0
	for {
		said, readError := read()
		var row landGateRow
		if readError == nil {
			readError = json.Unmarshal(said, &row)
		}
		switch {
		case readError != nil || row.State == "":
			unread++
			if unread >= landGateUnreadMost {
				return 0, fmt.Sprintf("land-gate job %s gave no verdict: the door failed to read it %d times in a row "+
					"(%s)", job, unread, lastWords(said, describeEnd(readError)))
			}
		case row.State == landGateEnded:
			return landGateEnd(job, row)
		default:
			unread = 0
		}
		if since() >= landGateWithin {
			return 0, fmt.Sprintf("land-gate job %s gave no verdict: the wait on it ran out after %s", job,
				landGateWithin)
		}
		sleep(landGateReadEvery)
	}
}

// landGateEnd is an ended row's exit, or why it is no verdict: every outcome but Exit, and an Exit with no exit, which
// is a broken row and never exit 0. A cancel's why is its reason when the row gives one, as ahra says it.
func landGateEnd(job string, row landGateRow) (int, string) {
	if row.Outcome == nil {
		return 0, "land-gate job " + job + " gave no verdict: it ended with no outcome"
	}
	if *row.Outcome == "Exit" {
		if row.Exit == nil {
			return 0, "land-gate job " + job + " gave no verdict: it ended Exit with no exit"
		}
		return *row.Exit, ""
	}
	end, known := landGateEnds[*row.Outcome]
	if !known {
		end = "it ended " + *row.Outcome + ", an outcome this land doesn't know"
	}
	why := row.OutcomeWhy
	if *row.Outcome == "Cancelled" {
		if row.CancelledBy != nil {
			end += " by " + *row.CancelledBy
		}
		if row.CancelledReason != nil {
			why = row.CancelledReason
		}
	}
	if why != nil && *why != "" {
		end += ": " + *why
	}
	return 0, "land-gate job " + job + " gave no verdict: " + end
}

// landGateCommand is the job's command: the lay kit, then sharedBoxGate through bash. The script is encoded, so no
// shell reads it before the box's bash does, and goroot is in double quotes, so the box's own shell expands its
// `$HOME`.
func landGateCommand(patterns []string, races []string) string {
	return landGateLayKit + `bash -c "$(echo ` + base64.StdEncoding.EncodeToString([]byte(landGateScript)) +
		` | base64 -d)" gate "` + sharedBoxGoroot + `" ` + sharedBoxToolchainDigest + ` '` + strings.Join(races, ",") +
		`' ` + strings.Join(patterns, " ")
}

// loomCommand is ahra run with arguments: the stand-in loomStandInVariable names, in the worktree, or the house's ahra
// from its own checkout.
func loomCommand(worktree string, arguments ...string) (*exec.Cmd, error) {
	if standIn := os.Getenv(loomStandInVariable); standIn != "" {
		command := exec.Command(standIn, arguments...)
		command.Dir = worktree
		return command, nil
	}
	home, homeError := poolHome()
	if homeError != nil {
		return nil, homeError
	}
	ahraDirectory := filepath.Join(home, "Projects", "ahra")
	command := exec.Command(filepath.Join(ahraDirectory, "node_modules", ".bin", "ahra"), arguments...)
	command.Dir = ahraDirectory
	return command, nil
}

// loomCombined runs ahra with arguments, as loomCommand makes it, and returns everything it printed.
func loomCombined(worktree string, arguments ...string) ([]byte, error) {
	command, loomError := loomCommand(worktree, arguments...)
	if loomError != nil {
		return nil, loomError
	}
	return command.CombinedOutput()
}

// runLandGateJob asks Loom for a land-gate job testing commit for task, waits for its end and reads its whole log. It
// returns the log, the job's own exit code and its id, or why the job gave no verdict, which refuses the landing.
func runLandGateJob(
	worktree string,
	commit string,
	task string,
	patterns []string,
	races []string,
) ([]byte, int, string, string) {
	ask, loomError := loomCommand(worktree, "loom", "job", landGateCommand(patterns, races), "--kind", landGateKind,
		"--task", task, "--at", "cohere@"+commit)
	if loomError != nil {
		return nil, 0, "", "the land-gate job could not be asked: " + loomError.Error()
	}
	asked, askError := ask.CombinedOutput()
	found := landGateJobId.FindSubmatch(asked)
	if found == nil {
		return nil, 0, "", "Loom took no land-gate job (" + lastWords(asked, describeEnd(askError)) + ")"
	}
	job := string(found[1])
	fmt.Fprintf(streams.Errors, "cohere-dev: asked Loom for land-gate job %s at %s; the land lock stays held while it "+
		"waits in line, so landings behind this one wait too\n", job, shortCommit(commit))
	streams.Show(streams.Errors)

	started := time.Now()
	code, unavailable := waitForLandGateJob(job, func() ([]byte, error) {
		row, loomError := loomCommand(worktree, "loom", "job", job, "--json")
		if loomError != nil {
			return nil, loomError
		}
		var rowErrors bytes.Buffer
		row.Stderr = &rowErrors
		said, rowError := row.Output()
		if rowError != nil {
			return rowErrors.Bytes(), rowError
		}
		return said, nil
	}, time.Sleep, func() time.Duration { return time.Since(started) })
	if unavailable != "" {
		return nil, 0, job, unavailable
	}

	read, loomError := loomCommand(worktree, "loom", "job", job, "--log")
	if loomError != nil {
		return nil, 0, job, "land-gate job " + job + "'s log could not be read: " + loomError.Error()
	}
	var readErrors bytes.Buffer
	read.Stderr = &readErrors
	log, readError := read.Output()
	if readError != nil {
		return nil, 0, job, fmt.Sprintf("land-gate job %s's log could not be read (%s)", job,
			lastWords(readErrors.Bytes(), describeEnd(readError)))
	}
	if code == landGateLayExitCode {
		for _, line := range strings.Split(string(log), "\n") {
			if strings.HasPrefix(line, landGateLayFailure) {
				return nil, 0, job, fmt.Sprintf("land-gate job %s gave no verdict: its box could not lay the tree "+
					"(%s)", job, strings.TrimSpace(line))
			}
		}
	}
	return log, code, job, ""
}
