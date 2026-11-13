package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/system-inc/cohere/internal/streams"
)

// A landing's tests can run as a Loom job of the land-gate kind, in place of a gap on Home, when homeGateVariable says
// landGateKindSetting. Loom places the job on a scratch box at the commit being landed, and the box runs the same
// sharedBoxGate Home runs, so its output is read the same way and holds to the same checks.
//
// No verdict from the job refuses the landing and never sends the tests to the Mac (#1v2shq7, item 2): a job that
// timed out or met Loom's or the machine's failure says nothing of the tree, and a Mac that took the run back each time
// would carry the load the kind exists to take off it. "off" stays the Mac alone, and Home's own gap still falls back.

// landGateKindSetting is the homeGateVariable setting that runs a landing's tests as a land-gate job.
const landGateKindSetting = "kind"

// loomStandInVariable names a command run in place of ahra for a test, called with ahra's own arguments: the ask
// (`loom job <command> ...`), the wait (`loom wait job <id> ...`) and the log (`loom job <id> --log`). Unset, the
// house's ahra runs.
const loomStandInVariable = "COHERE_DEV_LOOM"

// landGateKind is the Loom job kind a landing's tests run as, and landGateWithin how long the wait on it lasts, its
// time in line included.
const (
	landGateKind   = "land-gate"
	landGateWithin = "2h"
)

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
const landGateScript = "set -euo pipefail\n" + `goroot="$1"; digest="$2"; races="$3"; shift 3` + "\n" + sharedBoxGate

// landGateJobId reads the id from the ask's line, `J-6nrpbay queued, 1st in line, 0 ahead`. Any state is taken, so a
// job placed at once is still waited on rather than left running with no one to hear it.
var landGateJobId = regexp.MustCompile(`(?m)^(J-[0-9a-z]{7}) [a-z]+`)

// landGateWaitEnds is what each of the wait's own exits says, every one of them no verdict: 0 to 199 are the job's.
var landGateWaitEnds = map[int]string{
	200: "it was cancelled",
	201: "it was superseded",
	202: "it was orphaned",
	203: "its blocker ended",
	205: "it met Loom's or the machine's failure, never the tree's (infrastructure)",
	207: "it ended in an outcome this ahra doesn't know",
	208: "the door failed to read it",
	209: "the wait on it ran out after " + landGateWithin,
	210: "Loom refused it before anything ran",
	211: "ahra refused the wait's words",
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
// from its own checkout, as runOnHome runs it.
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

	wait, loomError := loomCommand(worktree, "loom", "wait", "job", job, "--within", landGateWithin)
	if loomError != nil {
		return nil, 0, job, "land-gate job " + job + " could not be waited on: " + loomError.Error()
	}
	waited, waitError := wait.CombinedOutput()
	var exited *exec.ExitError
	if waitError != nil && !errors.As(waitError, &exited) {
		return nil, 0, job, "land-gate job " + job + " could not be waited on: " + waitError.Error()
	}
	code := 0
	if exited != nil {
		code = exited.ExitCode()
	}
	if code >= 200 {
		end, known := landGateWaitEnds[code]
		if !known {
			end = fmt.Sprintf("its wait ended with exit %d, which says no end of the job's", code)
		}
		return nil, 0, job, fmt.Sprintf("land-gate job %s gave no verdict: %s (%s)", job, end,
			lastWords(waited, describeEnd(waitError)))
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
