package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/streams"
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

// macBoxScript is what a gate box runs, from the land that runs it rather than from the tree it tests, so no branch can
// change what tests it. Its arguments are the commit, its TypeScript pin, the raceGated packages comma-separated and
// the module's test patterns. It works only under macBoxScratch, in a bare repository and one tree that last across
// landings, so the tree's path stays one path. It waits for the box's mirror to hold the commit, refusing once it has
// waited 3 minutes, fetches the commit and the pin, checks them out, and runs sharedBoxGate against the Mac's pinned
// toolchain and node.
//
// The mirrors are root's, and git refuses another owner's repository as dubious unless safe.directory names it. Git
// 2.43 reads that setting for a fetch's source only from global or system configuration, since `-c safe.directory`
// reaches the fetch but not the upload-pack it starts in the source (@system_cohere_go_codebase's 3ddc75cb in
// cohere-pairs, measured on Lunatic and Pupa). So the script writes a configuration file of its own naming both
// mirrors, gives it to each git that reads them as GIT_CONFIG_GLOBAL, and removes it before any test runs.
const macBoxScript = `set -euo pipefail
commit="$1"; pin="$2"; races="$3"; shift 3
` + macBoxMirrorLine + `
base=` + macBoxScratch + `
mkdir -p "$base"; cd "$base"
refuse() { echo "` + sharedBoxRefusedMarker + `$*"; exit 1; }
safe="$base/` + macBoxSafeSettings + `"
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
held cohere.git "$commit" || refuse "` + macBoxMirrorLacks + `$commit"
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

// gateWait reads gateWaitVariable's setting, gateWaitDefault when it is empty.
func gateWait(setting string) (time.Duration, error) {
	if strings.TrimSpace(setting) == "" {
		return gateWaitDefault, nil
	}
	wait, parseError := time.ParseDuration(strings.TrimSpace(setting))
	if parseError != nil || wait <= 0 {
		return 0, fmt.Errorf("%s=%q is no wait, which Go writes as 2h or 90m", gateWaitVariable, setting)
	}
	return wait, nil
}

// runOnGateBoxes puts commit in Loom's feed, then runs the module's tests on the first of boxes that is free, through
// gap, and returns everything gap printed, the box it ran on and how gap ended. It returns why no box ran them
// instead, which refuses the landing: the feed didn't take the commit, or every box stayed held for wait.
//
// A Mac's gap waits on another org's claim for as long as a claim may stand rather than refusing, so each box's claim
// is read first, `gap --box <Mac>`, which exits 0 only while she is free, and a held one is passed over. A box taken
// between that read and the gap is passed over too, once gap says it stayed held.
func runOnGateBoxes(
	worktree string,
	commit string,
	patterns []string,
	races []string,
	boxes []string,
	wait time.Duration,
) ([]byte, string, string, error) {
	ahra := func(arguments ...string) ([]byte, error) {
		command, loomError := loomCommand(worktree, arguments...)
		if loomError != nil {
			return nil, loomError
		}
		return command.CombinedOutput()
	}
	pushed, pushError := ahra("loom", "push", "--feed", commit, "--repository", "cohere")
	if pushError != nil {
		return nil, "", fmt.Sprintf("Loom's feed didn't take %s (%s)", shortCommit(commit),
			lastWords(pushed, describeEnd(pushError))), nil
	}
	// Ignored: a tree with no TypeScript gitlink needs no pin, and the box's script skips an empty one.
	pin, _ := gitOutput(worktree, "rev-parse", "--verify", "--quiet", commit+":TypeScript")
	// Encoded, so nothing in the script is read by a shell before the box's bash: gap's command is one argument to ssh.
	remote := "echo " + base64.StdEncoding.EncodeToString([]byte(macBoxScript)) + " | base64 -d | bash -s -- " +
		commit + " '" + pin + "' '" + strings.Join(races, ",") + "' " + strings.Join(patterns, " ")
	started := time.Now()
	for {
		held := []string{}
		for _, box := range boxes {
			read, readError := ahra("loom", "gap", "--box", box)
			if readError != nil {
				words := box + " (" + lastWords(read, describeEnd(readError)) + ")"
				fmt.Fprintf(streams.Errors, "cohere-dev: %s is held: %s\n", box, words)
				held = append(held, words)
				continue
			}
			fmt.Fprintf(streams.Errors, "cohere-dev: testing %s on %s in her gap; the land lock stays held while it "+
				"runs\n", shortCommit(commit), box)
			streams.Show(streams.Errors)
			output, runError := ahra("loom", "gap", "--box", box, "--within", gateGapBound, "--", "ssh", box, remote)
			if gapHeld(runError, output) {
				words := box + " (" + lastWords(output, describeEnd(runError)) + ")"
				fmt.Fprintf(streams.Errors, "cohere-dev: %s is held: %s\n", box, words)
				held = append(held, words)
				continue
			}
			return output, box, "", runError
		}
		waited := time.Since(started)
		if waited >= wait {
			return nil, "", fmt.Sprintf("every gate box stayed held through %s of waiting: %s",
				waited.Round(time.Second), strings.Join(held, "; ")), nil
		}
		again := min(gateBoxAgain, wait-waited)
		fmt.Fprintf(streams.Errors, "cohere-dev: every gate box is held (%s); the land waits in line holding the "+
			"lock, asking again in %s, %s of %s waited\n", strings.Join(boxes, ", "), again.Round(time.Second),
			waited.Round(time.Second), wait)
		streams.Show(streams.Errors)
		time.Sleep(again)
	}
}
