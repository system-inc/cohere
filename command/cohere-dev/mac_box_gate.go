package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/command_line/streams"
)

// The road to a gate box (#rr818c8): the commit goes into Loom's feed on Zach with `ahra loom push --feed`, Operations'
// bridge carries it from there to each house Mac's root-owned /opt/loom-mirrors/cohere.git, and `ahra loom gap` runs
// macBoxScript on the first Mac gateBoxesVariable names that is free, as loom, in her own gap. The TypeScript pin a
// commit records is in each Mac's /opt/loom-mirrors/TypeScript.git already, so it is named and never pushed.
//
// A Mac another org holds goes to the next. With every one held, the land waits in line holding the lock, asking
// again every gateBoxAgain until gateWaitVariable's wait has passed, and then refuses the landing, naming each box and
// what held it. Nothing ever falls back to Kirk's Mac: that load is what the gate box exists to take off it.

// gateBoxesVariable is the house Macs a landing's tests may run on, comma-separated, tried in order. Unset, it is
// gateBoxesDefault.
const gateBoxesVariable = "COHERE_DEV_GATE_BOXES"

// gateBoxesDefault is the Macs @system_cohere ruled the gate's (#rr818c8).
const gateBoxesDefault = "Sun2,Marin"

// gateBoxRefused is the Mac no land gate runs on, refused by name wherever the setting names her: she holds numbers of
// record, which a gate's minutes of every core would spoil (#rr818c8).
const gateBoxRefused = "Shackler"

// gateWaitVariable is how long a landing waits in line for a gate box while every one is held, as Go writes a
// duration. Unset, it is gateWaitDefault.
const gateWaitVariable = "COHERE_DEV_GATE_WAIT"

// gateWaitDefault is how long a landing waits for a gate box, as long as a Mac's claim may stand (LoomGapMac.ts), so a
// box held for the longest claim there is comes free inside it.
const gateWaitDefault = 2 * time.Hour

// gateBoxAgain is how long a landing waits, while every gate box is held, before it asks each again.
const gateBoxAgain = time.Minute

// gateGapBound is the gap's --within on a gate box: how long its run may take. The module's tests build on an empty Go
// cache there every landing (sharedBoxGate), and a Mac takes at most 2 hours, so an hour leaves the build its room
// while a run that hangs still gives the box back.
const gateGapBound = "1h"

// The Mac's mirrors, which Operations' bridge fills; the words a box refuses with when hers lacks the commit; the one
// directory the box's script works in, under loom's home; and the configuration file it names the mirrors safe in.
const (
	macBoxMirrors      = "/opt/loom-mirrors"
	macBoxMirrorLacks  = "this box's mirror doesn't hold "
	macBoxScratch      = "~/scratch/system_cohere_tools"
	macBoxSafeSettings = "safe-directory.gitconfig"
)

// macBoxMirrorLine is macBoxScript's line naming the mirrors, how many times it asks whether one holds a commit and
// how many seconds apart: the bridge can lag the feed, so a mirror that lacks it is asked 10 times, 20 seconds apart,
// for 3 minutes, before the box refuses. A test replaces the line to point the script at mirrors of its own.
const macBoxMirrorLine = "mirrors=" + macBoxMirrors + "; tries=10; pause=20"

// macBoxIntegrateCacheLine is macBoxScript's and landGateScript's line setting integrateCache, sharedBoxGate's kept Go
// cache (sharedBoxCacheBlock), to none: set in the script itself, so no variable in a shared box's environment can keep
// a cache there. The integrate job on Shackler alone replaces it with its own setting (integrate_job_run.go).
const macBoxIntegrateCacheLine = `integrateCache=""`

// integrateJobLine is the line of macBoxScript, landGateScript and performanceScript saying the script runs in no
// integrate job: set in the script itself, so no variable in an account's environment can make it one. The integrate
// job alone replaces it with integrateJobSetting (integrate_job_run.go), which is what lets each script read the module
// set Loom laid for the job, with a kept cache or without one (sharedBoxGate, land_performance.go).
const (
	integrateJobLine    = `integrateJob=""`
	integrateJobSetting = `integrateJob=1`
)

// macBoxTemporaryBlock makes gatetemp, the tests' temporary directory: a new one each run on a gate box, under the
// account's `TMPDIR`, and in an integrate job one path every job, made empty first: inside the kept cache when one is
// kept, else under the script's base, which the integrate kind's fixed home holds at one path (#e94a6r4). go test's
// cache keys a test on the `TMPDIR` it read, which t.TempDir reads, so a directory named anew each job misses every
// package that makes one: J-5j0jbp5, on the runner's cache with no kept one, replayed 73 of 438. Never a fixed /tmp:
// an integrate job in Shackler's sandbox may write its own tmp and its kept cache, and /tmp refused it (J-7dhfcty). The
// integrate job is serial (integrate_job_run.go), so no two runs share the path.
const macBoxTemporaryBlock = `if [ -n "$integrateCache" ] || [ -n "$integrateJob" ]; then
  gatehome="${integrateCache:-$base}"
  gatetemp="$gatehome/temporary"
  { chmod -R u+w "$gatetemp" 2>/dev/null || true; rm -rf "$gatetemp"; } || refuse "$gatetemp couldn't be emptied"
  mkdir -p "$gatehome" && mkdir -m 700 "$gatetemp" || refuse "$gatetemp couldn't be made"
else
  gatetemp=$(mktemp -d "${TMPDIR:-/tmp}/cohere-gate.XXXXXX") || refuse "no temporary directory could be made"
fi
`

// macBoxMirrorReads is how a Mac's script reads her mirrors, given base, mirrors, tries, pause and refuse before it: it
// writes the configuration file naming both mirrors safe, removed when the script ends unless it is removed sooner, and
// defines held, which waits for a mirror to hold a commit. The gate's script (macBoxScript) and the landing's
// measurement on Shackler (land_performance.go) read them the same way.
//
// The mirrors are root's, and git refuses another owner's repository as dubious unless safe.directory names it. Git
// 2.43 reads that setting for a fetch's source only from global or system configuration, since `-c safe.directory`
// reaches the fetch but not the upload-pack it starts in the source (@system_cohere_go_codebase's 3ddc75cb in
// cohere-pairs, measured on Lunatic and Pupa). So the script writes a configuration file of its own naming both
// mirrors, and gives it to each git that reads them as GIT_CONFIG_GLOBAL.
const macBoxMirrorReads = `safe="$base/` + macBoxSafeSettings + `"
printf '[safe]\n\tdirectory = "%s"\n\tdirectory = "%s"\n' "$mirrors/cohere.git" "$mirrors/TypeScript.git" > "$safe"
trap 'rm -f "$safe"' EXIT
# held waits for the mirror $1 to hold the commit $2, asking again while git says only that it lacks it, and refuses
# at once when git can't read the mirror at all, in git's words.
held() {
  try=1
  while :; do
    if said=$(GIT_CONFIG_GLOBAL="$safe" git -C "$mirrors/$1" rev-parse --verify --quiet "$2^{commit}" 2>&1); then
      return 0
    else
      code=$?
    fi
    [ "$code" = 1 ] || refuse "$mirrors/$1 didn't read: $(echo "$said" | tail -n 1)"
    [ "$try" -lt "$tries" ] || return 1
    sleep "$pause"; try=$((try + 1))
  done
}
`

// macBoxScript is what a gate box runs, from the land that runs it rather than from the tree it tests, so no branch can
// change what tests it. Its arguments are the commit, its TypeScript pin, racePatterns' tokens comma-separated, each
// `./<directory>` or `./<directory>@<run>`, and the module's test patterns. It works only under macBoxScratch, in a
// bare repository and one tree that last across landings, so the tree's path stays one path. It waits for the box's
// mirror to hold the commit, refusing once it has waited 3 minutes, fetches the commit and the pin, checks them out,
// and runs sharedBoxGate against the Mac's pinned toolchain and node. It reads the mirrors through macBoxMirrorReads,
// and removes the configuration file naming them safe before any test runs. On a gate box it keeps no Go cache
// (macBoxIntegrateCacheLine) and is no integrate job (integrateJobLine); the integrate job runs it on Shackler with its
// mirrors, its cache when one is kept, and its own setting.
const macBoxScript = `set -euo pipefail
commit="$1"; pin="$2"; races="$3"; shift 3
` + macBoxMirrorLine + `
` + macBoxIntegrateCacheLine + `
` + integrateJobLine + `
base=` + macBoxScratch + `
mkdir -p "$base"; cd "$base"
refuse() { echo "` + sharedBoxRefusedMarker + `$*"; exit 1; }
` + macBoxMirrorReads + `held cohere.git "$commit" || refuse "` + macBoxMirrorLacks + `$commit"
[ -d cohere.git ] || git init -q --bare cohere.git
GIT_CONFIG_GLOBAL="$safe" git -C cohere.git fetch -q "$mirrors/cohere.git" "$commit"
[ -e tree/.git ] || git -C cohere.git worktree add -q --detach "$base/tree" "$commit"
git -C tree checkout -q --detach --force "$commit"
git -C tree clean -q -f -d -x -e TypeScript
if [ -n "$pin" ]; then
  held TypeScript.git "$pin" || refuse "this box's TypeScript mirror doesn't hold $pin"
  [ -d TypeScript.git ] || git init -q --bare TypeScript.git
  GIT_CONFIG_GLOBAL="$safe" git -C TypeScript.git fetch -q "$mirrors/TypeScript.git" "$pin"
  if [ ! -e tree/TypeScript/.git ]; then
    rmdir tree/TypeScript 2>/dev/null || true
    git -C TypeScript.git worktree add -q --detach "$base/tree/TypeScript" "$pin"
  fi
  git -C tree/TypeScript checkout -q --detach --force "$pin"
fi
rm -f "$safe"; trap - EXIT
# The tests' temporary directory, made anew under the account's TMPDIR, or one path every integrate job, and never
# under a fixed /tmp, which an integrate job in Shackler's sandbox may not write (J-7dhfcty).
` + macBoxTemporaryBlock + `trap 'chmod -R u+w "$gatetemp" 2>/dev/null; rm -rf "$gatetemp"' EXIT
export TMPDIR="$gatetemp"
cd tree
goroot="` + macBoxGoroot + `"; digest="` + macBoxToolchainDigest + `"
node="` + macBoxNode + `"; nodeDigest="` + macBoxNodeDigest + `"
` + sharedBoxGate

// gateBoxes reads gateBoxesVariable's setting, gateBoxesDefault when it is empty, refusing gateBoxRefused by name.
func gateBoxes(setting string) ([]string, error) {
	list := setting
	if strings.TrimSpace(list) == "" {
		list = gateBoxesDefault
	}
	boxes := []string{}
	for _, named := range strings.Split(list, ",") {
		box := strings.TrimSpace(named)
		if box == "" {
			continue
		}
		if strings.EqualFold(box, gateBoxRefused) {
			return nil, fmt.Errorf("%s names %s, who holds numbers of record, so no land gate runs on her",
				gateBoxesVariable, gateBoxRefused)
		}
		boxes = append(boxes, box)
	}
	if len(boxes) == 0 {
		return nil, fmt.Errorf("%s=%q names no box", gateBoxesVariable, setting)
	}
	return boxes, nil
}

// waitSetting reads the setting of a wait in line, variable's, as Go writes a duration: fallback when it is empty, and
// refused, naming variable, when it is no wait. A landing waits so for a gate box (gateWaitVariable) and for Shackler's
// measurement (performanceWaitVariable).
func waitSetting(variable string, setting string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(setting) == "" {
		return fallback, nil
	}
	wait, parseError := time.ParseDuration(strings.TrimSpace(setting))
	if parseError != nil || wait <= 0 {
		return 0, fmt.Errorf("%s=%q is no wait, which Go writes as 2h or 90m", variable, setting)
	}
	return wait, nil
}

// feedGateCommit puts commit in Loom's feed and reads the TypeScript pin it records, empty for a tree with none. It
// returns why the feed didn't take it instead, which refuses the landing.
func feedGateCommit(worktree string, commit string) (string, string) {
	pushed, pushError := loomCombined(worktree, "loom", "push", "--feed", commit, "--repository", "cohere")
	if pushError != nil {
		return "", fmt.Sprintf("Loom's feed didn't take %s (%s)", shortCommit(commit),
			lastWords(pushed, describeEnd(pushError)))
	}
	// Ignored: a tree with no TypeScript gitlink needs no pin, and the box's script skips an empty one.
	pin, _ := gitOutput(worktree, "rev-parse", "--verify", "--quiet", commit+":TypeScript")
	return pin, ""
}

// macBoxCoreBudgets is how many cores each gate Mac that shares herself with other work gives a gate, by her name:
// Marin carries Ahra's texting line and AhraHands, so a job of ours there keeps to all but two of her 12 cores (Kirk's
// box plan, Nov 23, relayed on #bc8nf0r). A Mac not named here gives a gate all of hers.
var macBoxCoreBudgets = map[string]int{"Marin": 10}

// macBoxRemoteCommand is the command gap runs over ssh on the gate box named box: macBoxScript, given commit, its pin,
// the race tokens and the packages to test, by import path, every package the gate lists for the whole gate and a
// half's for a split one (gate_split.go), with box's core budget when macBoxCoreBudgets names one.
func macBoxRemoteCommand(box string, commit string, pin string, races []string, packages []string) string {
	budget := ""
	if cores, budgeted := macBoxCoreBudgets[box]; budgeted {
		budget = fmt.Sprintf("%s=%d ", boxCoreBudgetVariable, cores)
	}
	// Encoded, so nothing in the script is read by a shell before the box's bash: gap's command is one argument to ssh.
	return "echo " + base64.StdEncoding.EncodeToString([]byte(macBoxScript)) + " | base64 -d | " + budget +
		"bash -s -- " +
		commit + " '" + pin + "' '" + strings.Join(races, ",") + "' " + strings.Join(packages, " ")
}

// gateBoxHeld reads box's claim, `gap --box <Mac>`, which exits 0 only while she is free, and returns what holds her,
// as the land line names it, empty while she is free.
func gateBoxHeld(worktree string, box string) string {
	read, readError := loomCombined(worktree, "loom", "gap", "--box", box)
	if readError != nil {
		return box + " (" + lastWords(read, describeEnd(readError)) + ")"
	}
	return ""
}

// runOnGateBox runs remote on box in her gap, and returns everything gap printed, whether gap said she stayed held, so
// remote never ran, and how gap ended.
func runOnGateBox(worktree string, box string, remote string) ([]byte, bool, error) {
	output, runError := loomCombined(worktree, "loom", "gap", "--box", box, "--within", gateGapBound, "--", "ssh", box,
		remote)
	return output, gapHeld(runError, output), runError
}

// runOnGateBoxes runs the module's tests, packages by import path as the gate listed them, on the first of boxes that
// is free, through gap, the commit already in Loom's feed with its pin (feedGateCommit), and returns everything gap
// printed, the box it ran on and how gap ended. It returns why no box ran them instead, which refuses the landing:
// every box stayed held for wait.
//
// A Mac's gap waits on another org's claim for as long as a claim may stand rather than refusing, so each box's claim
// is read first, `gap --box <Mac>`, which exits 0 only while she is free, and a held one is passed over. A box taken
// between that read and the gap is passed over too, once gap says it stayed held.
func runOnGateBoxes(
	worktree string,
	commit string,
	pin string,
	packages []string,
	races []string,
	boxes []string,
	wait time.Duration,
) ([]byte, string, string, error) {
	// A whole gate's command is made for each box in turn, since each may hold her own core budget.
	remoteFor := func(box string) string { return macBoxRemoteCommand(box, commit, pin, races, packages) }
	started := time.Now()
	// asked is how many times every box has been asked. A land always asks twice before it refuses, so a wait shorter
	// than one round of asking still asks again: on a loaded box one round outlasted a 2s wait (Sun2, Nov 16).
	asked := 0
	for {
		asked++
		held := []string{}
		for _, box := range boxes {
			if words := gateBoxHeld(worktree, box); words != "" {
				fmt.Fprintf(streams.Errors, "cohere-dev: %s is held: %s\n", box, words)
				held = append(held, words)
				continue
			}
			fmt.Fprintf(streams.Errors, "cohere-dev: testing %s on %s in her gap; the land lock stays held while it "+
				"runs\n", shortCommit(commit), box)
			streams.Show(streams.Errors)
			output, stayedHeld, runError := runOnGateBox(worktree, box, remoteFor(box))
			if stayedHeld {
				words := box + " (" + lastWords(output, describeEnd(runError)) + ")"
				fmt.Fprintf(streams.Errors, "cohere-dev: %s is held: %s\n", box, words)
				held = append(held, words)
				continue
			}
			return output, box, "", runError
		}
		waited := time.Since(started)
		if waited >= wait && asked >= 2 {
			return nil, "", fmt.Sprintf("every gate box stayed held through %s of waiting: %s",
				waited.Round(time.Second), strings.Join(held, "; ")), nil
		}
		again := max(min(gateBoxAgain, wait-waited), 0)
		fmt.Fprintf(streams.Errors, "cohere-dev: every gate box is held (%s); the land waits in line holding the "+
			"lock, asking again in %s, %s of %s waited\n", strings.Join(boxes, ", "), again.Round(time.Second),
			waited.Round(time.Second), wait)
		streams.Show(streams.Errors)
		time.Sleep(again)
	}
}
