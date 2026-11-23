package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/format/native"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/streams"
)

// Every landing measures itself (#6wnsnq2), in Kirk's words "nothing gets committed to main without timing numbers".
// After the gate passes and before the landing commit is made, land runs the Go engine on Shackler, the quiet house Mac
// that holds numbers of record, and writes what it measured to Performance.jsonc at the root of the tree that lands. So
// main's history is its performance series, and each landing's diff of the file shows what it did to the engine.
//
// Rulings that keep it honest (@system_cohere, on #6wnsnq2):
//
//   - A landing never lands without its numbers. Shackler is claimed through the gap claim every org uses, so a
//     landing's window and a pair's never overlap. While she is held, the land waits holding the lock, reading her
//     claim every performanceAgain and saying whose claim it waits behind and since when, and refuses once
//     performanceWaitVariable's wait has passed, naming the holder. A measurement that runs and fails refuses the
//     landing in the box's words.
//   - What was gated is what lands, plus this file alone. It is written after the gate, so nothing that builds or tests
//     may read it, which internal/guard holds (TestNothingThatBuildsOrTestsReadsPerformanceJsonc).
//   - The comparison is honest: a number is "moved" only when its new min..max range doesn't overlap the old one, and
//     "level" otherwise; another machine, another build or another shape of measurement is a line of its own, never a
//     comparison.
//   - The file records that the box was held quiet, and her load before and after.
//   - A test-only landing measures too, and Go is the only engine measured.
//
// A tree with no command/cohere, every land test's fixture, has no engine to time, and lands saying so. cohere's own
// tree always has it.

// performanceFileName is the file at the root of every landing's tree that holds main's performance at that commit.
const performanceFileName = "Performance.jsonc"

// performanceBox is the Mac the measurement runs on: Shackler, who holds numbers of record, which is why no land gate
// runs on her (gateBoxRefused).
const performanceBox = gateBoxRefused

// performanceWaitVariable is how long a landing waits in line for Shackler while another holds her, as Go writes a
// duration. Unset, it is performanceWaitDefault, as long as a Mac's claim may stand: codebase's pairs claim her in
// slices of at most 15 minutes, so a waiting landing waits one slice at most, and the bound is the backstop.
const (
	performanceWaitVariable = "COHERE_DEV_PERFORMANCE_WAIT"
	performanceWaitDefault  = 2 * time.Hour
)

// performanceAgain is how long a landing waits, while Shackler is held, before it reads her claim again.
const performanceAgain = time.Minute

// performanceGapBound is the measurement's gap's --within. Its runs take about 30 seconds, and a build on a cold cache
// or a mirror that lags take minutes, so half an hour leaves them room while a run that hangs still gives her back.
const performanceGapBound = "30m"

// performanceScratch is the directory the measurement works in on Shackler, under the gate's scratch in loom's home.
const performanceScratch = macBoxScratch + "/performance"

// performanceReference is the older cohere every landing also times, deeafbf4, Kirk's bar row's second tree, laid once
// on Shackler and kept; performanceReferenceTree is its name in the script's rows and in the file.
const (
	performanceReference     = "deeafbf43e5dfb39c7bacdcdc3f165b571b97276"
	performanceReferenceTree = "deeafbf4"
)

// performanceMarker opens every line the measurement's script prints for Kirk's Mac to read, and
// performanceRefusedMarker the line it prints when it refuses to measure, its reason after it.
const (
	performanceMarker        = "cohere-dev-performance: "
	performanceRefusedMarker = performanceMarker + "refused "
)

// The runs the script makes: performanceFills untimed runs before each cold row's rounds fill the binary's system
// store, the cold rows at the root and at deeafbf4 are timed performanceColdRounds and performanceReferenceRounds
// times, and the warm row, after one untimed run fills the project store, performanceWarmRounds times. Every row has
// more than one round, since a row of one is a range of one point, which any change moves.
const (
	performanceFills           = 2
	performanceColdRounds      = 3
	performanceReferenceRounds = 2
	performanceWarmRounds      = 3
)

// performanceBuild is how the script builds the binary it times: a release's Go build, without the Swift engine. The
// file's tools say it whole.
const performanceBuild = "CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags='-s -w'"

// performanceSchema is the file's schema, which Adamic's Performance.jsonc matches (#hed1mkb).
const performanceSchema = 1

// performanceHeader opens the file, above the body native.FormatHouse prints.
const performanceHeader = "// Performance.jsonc: main's performance at this commit, rewritten by cohere-dev land " +
	"in every landing commit (#6wnsnq2).\n// Git history is the series. Never edit it by hand. Times are integer " +
	"milliseconds, the median of the rounds with their min and max.\n"

// performanceScript is what Shackler runs, from the land that runs it rather than from the tree it measures, so no
// branch can change how it is measured. Its arguments are the commit, deeafbf4's commit and the counts of fills, cold
// rounds, deeafbf4's rounds and warm rounds. It works only under performanceScratch:
//
//   - It waits for her mirror to hold the commit, as the gate's script does (macBoxMirrorReads).
//   - It lays the root at the commit and deeafbf4 once, each with its TypeScript at its pin, as #h38e6y7's kit lays
//     them: clones sharing the mirrors' objects, kept across landings.
//   - It checks the Go toolchain against its digest (sharedBoxToolchainCheck) and the module cache against go.sum, and
//     builds the binary with them on a Go cache it keeps under the scratch across landings, so a build compiles only
//     what changed.
//   - It makes an empty system store for this binary, says her load before, and runs the engine each time with the
//     kit's clean environment under /usr/bin/time -l: at the root two untimed fills and the cold rounds, at deeafbf4
//     two fills and its rounds, at the root one untimed warm run and the warm rounds, and one timeline run. Each run
//     says what it checked in the engine's own words, its lint record's files walked and its summary's findings, and
//     refuses without them. Then it says the timeline's record and her load after.
//
// Each phase says its milliseconds, so what costs what can be read.
const performanceScript = `set -euo pipefail
commit="$1"; reference="$2"; fills="$3"; coldRounds="$4"; referenceRounds="$5"; warmRounds="$6"
` + macBoxMirrorLine + `
base=` + performanceScratch + `
mkdir -p "$base"; cd "$base"
refuse() { echo "` + performanceRefusedMarker + `$*"; exit 1; }
say() { echo "` + performanceMarker + `$*"; }
# now is the wall clock in milliseconds: macOS's bash and date read whole seconds alone, and gap itself reads the Mac
# through /usr/bin/perl. phase says how long the phase it names took, from the end of the one before it.
now() { /usr/bin/perl -MTime::HiRes=time -e 'printf "%d", time * 1000'; }
phaseStarted=$(now)
phase() { phaseEnded=$(now); say "phase $1 $((phaseEnded - phaseStarted))"; phaseStarted=$phaseEnded; }
` + macBoxMirrorReads + `export GIT_CONFIG_GLOBAL="$safe"
held cohere.git "$commit" || refuse "` + macBoxMirrorLacks + `$commit"
phase mirror
# lay checks the tree $1 out at the commit $2, with its TypeScript at the pin it records, each a clone sharing its
# mirror's objects. A clone is kept across landings, so a lay moves only what changed, and is cleaned of what the last
# landing's runs left in it.
lay() {
  where="$base/$1"
  if [ ! -e "$where/.git" ]; then
    rm -rf "$where"
    git clone -q --shared --no-checkout "$mirrors/cohere.git" "$where" || return 1
  fi
  git -C "$where" cat-file -e "$2^{commit}" 2>/dev/null || git -C "$where" fetch -q origin "$2" || return 1
  git -C "$where" checkout -q --detach --force "$2" || return 1
  git -C "$where" clean -q -f -d -x -e TypeScript || return 1
  pin=$(git -C "$where" rev-parse "$2:TypeScript") || return 1
  held TypeScript.git "$pin" || refuse "this box's TypeScript mirror doesn't hold $pin"
  if [ ! -e "$where/TypeScript/.git" ]; then
    rm -rf "$where/TypeScript"
    git clone -q --shared --no-checkout "$mirrors/TypeScript.git" "$where/TypeScript" || return 1
  fi
  git -C "$where/TypeScript" cat-file -e "$pin^{commit}" 2>/dev/null ||
    git -C "$where/TypeScript" fetch -q origin "$pin" || return 1
  git -C "$where/TypeScript" checkout -q --detach --force "$pin"
}
lay root "$commit" || refuse "the root didn't lay at $commit"
# deeafbf4 is laid once and kept: its mark names the commit it was laid at, and is written only once the lay is whole.
if [ "$(cat deeafbf4.laid 2>/dev/null || true)" != "$reference" ]; then
  held cohere.git "$reference" || refuse "` + macBoxMirrorLacks + `$reference"
  lay deeafbf4 "$reference" || refuse "deeafbf4 didn't lay at $reference"
  echo "$reference" > deeafbf4.laid
fi
rm -f "$safe"; trap - EXIT; unset GIT_CONFIG_GLOBAL
phase lay
goroot="` + macBoxGoroot + `"; digest="` + macBoxToolchainDigest + `"
export PATH="$goroot/bin:` + sharedBoxSystemPath + `"
` + sharedBoxToolchainCheck + `unset GOFLAGS GOCACHE GOMODCACHE GOPATH GOWORK GOPROXY GONOSUMDB GONOSUMCHECK GOPRIVATE
unset GOINSECURE GOSUMDB
export GOROOT="$goroot" GOTOOLCHAIN=local GOENV=off
phase toolchain
# The runs read modules from a cache of the measurement's own, kept under the scratch: loom's account names a cache of
# its own (GOMODCACHE=~/cache/go-mod on Shackler), and the default held only what this script had downloaded, so
# deeafbf4's modules were missing and its runs stopped at type errors, which read as a fast row (Nov 21). Every module
# of both trees' workspaces is downloaded and verified here, before any run, and the runs then go offline.
export GOMODCACHE="$base/gomodcache"
for tree in root deeafbf4; do
  downloaded=$(cd "$tree" && go mod download 2>&1) ||
    refuse "go mod download failed at $tree: $(echo "$downloaded" | tail -n 1)"
  verified=$(cd "$tree" && go mod verify 2>&1) || refuse "go mod verify failed at $tree: $(echo "$verified" | tail -n 1)"
done
cd root
phase modules
# The build's Go cache is kept under the scratch across landings, so a landing compiles only what it changed: the binary
# is built on a cache the box kept, as the file's tools say. A -trimpath build's bytes are the same on any cache.
built=$(GOCACHE="$base/gocache" ` + performanceBuild + ` -o "$base/cohere" ./command/cohere 2>&1) ||
  refuse "cohere didn't build at $commit: $(echo "$built" | tail -n 1)"
cd "$base"
phase build
say "tested $commit on $(go env GOOS)/$(go env GOARCH)"
# The binary's digest: a -trimpath build of the same source is the same bytes, so a landing that changed no file the
# engine compiles builds main's binary again, and its note can say any difference is the box's (#6wnsnq2).
say "binary $(sha256sum "$base/cohere" | cut -d' ' -f1)"
say "machine $(sysctl -n hw.ncpu) $(sysctl -n machdep.cpu.brand_string)"
say "go $(go env GOVERSION)"
# Her load as gap's own quiet lines read it, and the busiest processes beside it.
loadNow() {
  LC_ALL=C sysctl -n vm.loadavg 2>/dev/null </dev/null | tr -d '{}' | awk 'NF == 3 { print $1 " " $2 " " $3 }'
}
say "load before $(loadNow)"
say "top before $(LC_ALL=C ps -Ao pid,user,pcpu,comm -r 2>/dev/null </dev/null | awk 'NR > 1 && NR <= 6 {
  name = $4; sub(/.*\//, "", name); top = top (top == "" ? "" : "; ") $1 " " $2 " " $3 " " name } END { print top }' |
  cut -b 1-400)"
# The binary's own system store, empty, which the fills fill; deeafbf4's project store goes with it.
rm -rf system deeafbf4/.cache; mkdir system
clean=(env -u GOFLAGS -u PWD -u GOGC -u GOMEMLIMIT -u COHERE_SCRATCH_BUILDS_WIDTH -u COHERE_SYSTEM_CACHE -u GOARM64
  GOPROXY=off GOSUMDB=sum.golang.org GONOSUMDB='*')
# recorded is the number the field $2 holds in run.jsonl's first record of the kind $1, the first it names, or nothing
# when there is no such record: this bash has no jq, and python3 may not be there.
recorded() {
  grep -m 1 "\"kind\":\"$1\"" run.jsonl | grep -o "\"$2\":[0-9]*" | head -n 1 | cut -d : -f 2 || true
}
# engineSaid is the engine's own last line in run.err, the one before /usr/bin/time's report, which run.err ends with.
engineSaid() {
  awk '/ real +[0-9.]+ user +[0-9.]+ sys/ { exit } { last = $0 } END { print last }' run.err
}
# run runs the engine once at the tree $2 for the row $1, round $3 ("fill" for an untimed one), with the flags after,
# and says its wall, user and system milliseconds from /usr/bin/time, its exit, how many findings it printed, and what
# the engine's own records say it checked: the lint record's files walked and the summary's findings. A run that
# checked less reads faster, so a run with no record of what it checked measures nothing and refuses.
run() {
  runMode=$1; runTree=$2; runRound=$3; shift 3
  where="$base/$runTree"
  rm -f run.jsonl run.err
  if (cd "$where" && "${clean[@]}" PWD="$where" COHERE_SYSTEM_CACHE="$base/system" COHERE_GO_ENGINE_CHILD=1 \
    /usr/bin/time -l "$base/cohere" go-engine --contract 3 --root "$where" --no-fix "$@" > "$base/run.jsonl" \
    2> "$base/run.err"); then
    runCode=0
  else
    runCode=$?
  fi
  times=$(grep -E ' real +[0-9.]+ user +[0-9.]+ sys' run.err | tail -n 1 |
    awk '{ printf "%d %d %d", $1 * 1000 + 0.5, $3 * 1000 + 0.5, $5 * 1000 + 0.5 }' || true)
  [ -n "$times" ] || refuse "the $runMode run at $runTree printed no times: $(tail -n 1 run.err)"
  findings=$(grep -c '"kind":"finding"' run.jsonl || true)
  walked=$(recorded lint filesWalked)
  [ -n "$walked" ] ||
    refuse "the $runMode run at $runTree, round $runRound, printed no lint record: $(engineSaid)"
  summarized=$(recorded summary findings)
  [ -n "$summarized" ] ||
    refuse "the $runMode run at $runTree, round $runRound, printed no summary record: $(engineSaid)"
  say "run $runMode $runTree $runRound $times $runCode $findings $walked $summarized"
}
# rounds runs the row $1 at the tree $2, $3 untimed fills and then $4 timed rounds, with the flags after.
rounds() {
  roundsMode=$1; roundsTree=$2; roundsFills=$3; roundsTimed=$4; shift 4
  round=1
  while [ "$round" -le "$roundsFills" ]; do run "$roundsMode" "$roundsTree" fill "$@"; round=$((round + 1)); done
  round=1
  while [ "$round" -le "$roundsTimed" ]; do run "$roundsMode" "$roundsTree" "$round" "$@"; round=$((round + 1)); done
  phase "$roundsMode-$roundsTree"
}
rounds cold root "$fills" "$coldRounds" --no-cache
rounds cold deeafbf4 "$fills" "$referenceRounds" --no-cache
rounds warm root 1 "$warmRounds"
rounds timeline root 0 1 --no-cache --timeline
say "timeline $(grep -m 1 '"kind":"timeline"' run.jsonl || true)"
say "load after $(loadNow)"
`

// performanceSpread is one time of a row, in integer milliseconds: the median of its rounds, and their least and most.
type performanceSpread struct {
	Median  int64 `json:"median"`
	Minimum int64 `json:"min"`
	Maximum int64 `json:"max"`
}

// performanceChecked is what a row's rounds checked, each the same: the files the engine walked and the findings it
// printed. A row that checked less reads faster, so the note compares only rows that checked the same.
type performanceChecked struct {
	Files    int `json:"files"`
	Findings int `json:"findings"`
}

// performanceRow is a row's wall, user and system times, and what its rounds checked. Checked is a pointer since a file
// written before rows recorded it holds none, which the note says rather than reading it as nothing checked.
type performanceRow struct {
	Wall    performanceSpread   `json:"wall"`
	User    performanceSpread   `json:"user"`
	System  performanceSpread   `json:"sys"`
	Checked *performanceChecked `json:"checked,omitempty"`
}

// performanceTimeline is the timeline run's level 1 wall areas, each the milliseconds from the engine's start to the
// stamp it names: the host's listing's end, the handover's end, the extra builds' end and the run's end. A stamp the
// run didn't take, a handover with no extra build, is left out.
type performanceTimeline struct {
	Listing  int64 `json:"listing,omitempty"`
	Handover int64 `json:"handover,omitempty"`
	Builds   int64 `json:"builds,omitempty"`
	Run      int64 `json:"run,omitempty"`
}

// performanceTimelineStamps are the timeline's stamps each area of performanceTimeline is read from, the host's, in the
// order the file names them.
var performanceTimelineStamps = []string{"host.listing.end", "handover.end", "builds.end", "run.end"}

// performanceRounds is how many timed rounds each row took.
type performanceRounds struct {
	Cold     int `json:"cold"`
	Deeafbf4 int `json:"deeafbf4"`
	Warm     int `json:"warm"`
}

// performanceColdRows are the cold rows: every build's run with no project store, over a warm system store.
type performanceColdRows struct {
	Root     performanceRow `json:"root"`
	Deeafbf4 performanceRow `json:"deeafbf4"`
}

// performanceWarmRows is the warm row: a run over the project store the run before it filled.
type performanceWarmRows struct {
	Root performanceRow `json:"root"`
}

// performanceTimelineRows is the timeline run's areas, at the root.
type performanceTimelineRows struct {
	Root performanceTimeline `json:"root"`
}

// performanceEngine is the Go engine's measurement.
type performanceEngine struct {
	Rounds   performanceRounds       `json:"rounds"`
	Cold     performanceColdRows     `json:"cold"`
	Warm     performanceWarmRows     `json:"warm"`
	Timeline performanceTimelineRows `json:"timeline"`
}

// performanceMachine is the box measured on, her cores and her load before and after, and whether she was held quiet,
// which a measurement taken inside her gap claim always is.
type performanceMachine struct {
	Name       string    `json:"name"`
	Cpu        string    `json:"cpu"`
	Cores      int       `json:"cores"`
	LoadBefore []float64 `json:"loadBefore"`
	LoadAfter  []float64 `json:"loadAfter"`
	HeldQuiet  bool      `json:"heldQuiet"`
}

// performanceTools is what ran: the binary, by the commit it was built at and how, the Go that built it, and the
// binary's sha256. Binary is empty in a file written before files recorded it.
type performanceTools struct {
	Cohere string `json:"cohere"`
	Go     string `json:"go"`
	Binary string `json:"binary,omitempty"`
}

// sha256Pattern is a sha256 as sha256sum prints it, which the measurement says its binary's digest as.
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// performanceFile is Performance.jsonc's body (#hed1mkb's schema).
type performanceFile struct {
	Schema     int                `json:"schema"`
	Commit     string             `json:"commit"`
	MeasuredAt string             `json:"measuredAt"`
	Machine    performanceMachine `json:"machine"`
	Tools      performanceTools   `json:"tools"`
	Note       string             `json:"note"`
	Engine     performanceEngine  `json:"engine"`
}

// performanceRun is one engine run the script said: its row's mode and tree, its round, "fill" for an untimed one, its
// times in milliseconds, its exit, how many findings it printed, and the files its lint record walked and the findings
// its summary counted.
type performanceRun struct {
	mode, tree, round               string
	wall, user, system              int64
	exit, findingsPrinted           int
	filesWalked, findingsSummarized int
}

// performanceMeasurement is everything the script said.
type performanceMeasurement struct {
	commit, platform, cpu, goVersion, binary string
	cores                                    int
	loadBefore, loadAfter                    []float64
	// phases are the script's phases, each as the land line says it, "build 12.3s".
	phases   []string
	runs     []performanceRun
	timeline *contract.TimelineRecord
}

// performanceRows are the rows the note compares, by the name it says each by.
var performanceRows = []struct {
	name string
	row  func(file performanceFile) performanceRow
}{
	{"cold root", func(file performanceFile) performanceRow { return file.Engine.Cold.Root }},
	{"cold deeafbf4", func(file performanceFile) performanceRow { return file.Engine.Cold.Deeafbf4 }},
	{"warm root", func(file performanceFile) performanceRow { return file.Engine.Warm.Root }},
}

// measurePerformance measures gating, the commit the gate ran on, whose tree is tree, on Shackler, against main's
// Performance.jsonc at mainCommit, and returns the tree that lands: tree with Performance.jsonc written from the
// measurement, or tree itself when it has no command/cohere to measure. With it come the sentence the land line and the
// landing's body say of the measurement, and an exit code, 0 unless it refused the landing, which it has said why.
func measurePerformance(worktree string, gating string, tree string, mainCommit string) (string, string, int) {
	engine := strings.TrimPrefix(cohereSource, "./")
	mode, treeModeError := treeMode(worktree, gating, engine)
	if treeModeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading whether there is an engine to measure: %v\n", treeModeError)
		return "", "", 1
	}
	if mode == "" {
		said := fmt.Sprintf("Nothing measured: %s has no %s, so there is no engine to time", shortCommit(gating),
			engine)
		fmt.Fprintf(streams.Errors, "cohere-dev: %s\n", said)
		return tree, said + ".", 0
	}
	wait, waitError := waitSetting(performanceWaitVariable, os.Getenv(performanceWaitVariable), performanceWaitDefault)
	if waitError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", waitError)
		return "", "", 2
	}
	started := time.Now()
	output, unavailable, runError := measureOnShackler(worktree, gating, wait)
	if unavailable != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: no measurement from %s, and nothing lands unmeasured: %s\n",
			performanceBox, unavailable)
		return "", "", 1
	}
	measurement, problem := readPerformance(output, gating, runError)
	if problem != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's measurement refused the landing, and nothing lands unmeasured: "+
			"%s\n", performanceBox, problem)
		return "", "", 1
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: %s measured %s in %s: %s\n", performanceBox, shortCommit(gating),
		time.Since(started).Round(time.Second), strings.Join(measurement.phases, ", "))
	current := performanceFileOf(measurement, gating, time.Now())
	previous, found, unread := readPreviousPerformance(worktree, mainCommit)
	current.Note = performanceNote(current, previous, found, unread)
	contents, renderError := renderPerformance(current)
	if renderError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: writing %s: %v\n", performanceFileName, renderError)
		return "", "", 1
	}
	measured, spliceError := spliceIntoTree(worktree, tree, performanceFileName, contents)
	if spliceError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: writing %s into the landing's tree: %v\n", performanceFileName,
			spliceError)
		return "", "", 1
	}
	return measured, fmt.Sprintf("Measured on %s for %s: %s.", performanceBox, performanceFileName, current.Note), 0
}

// measure runs the measurement a landing makes, alone, at the commit its arguments name (the worktree's head unless
// one is given), and prints the file it would write with what each phase cost, landing nothing: for reading what the
// measurement costs, and for proving a change to it on the real box before it gates a landing. It waits for Shackler
// as a landing does.
func measure(arguments []string) int {
	worktree, worktreeError := landWorktree()
	if worktreeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: measure runs inside a cohere worktree: %v\n", worktreeError)
		return 2
	}
	revision := "HEAD"
	if len(arguments) == 1 {
		revision = arguments[0]
	} else if len(arguments) > 1 {
		fmt.Fprintln(streams.Errors, "cohere-dev: measure takes one commit at most")
		return 2
	}
	commit, commitError := gitOutput(worktree, "rev-parse", "--verify", revision+"^{commit}")
	if commitError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", commitError)
		return 2
	}
	wait, waitError := waitSetting(performanceWaitVariable, os.Getenv(performanceWaitVariable), performanceWaitDefault)
	if waitError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", waitError)
		return 2
	}
	started := time.Now()
	output, unavailable, runError := measureOnShackler(worktree, commit, wait)
	if unavailable != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: no measurement from %s: %s\n", performanceBox, unavailable)
		return 1
	}
	measurement, problem := readPerformance(output, commit, runError)
	if problem != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's measurement refused: %s\n%s\n", performanceBox, problem,
			lastWords(output, describeEnd(runError)))
		return 1
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: %s measured %s in %s: %s\n", performanceBox, shortCommit(commit),
		time.Since(started).Round(time.Second), strings.Join(measurement.phases, ", "))
	current := performanceFileOf(measurement, commit, time.Now())
	// Ignored: a main that can't be read compares with nothing, which the note says.
	mainCommit, _ := gitOutput(worktree, "rev-parse", "main")
	previous, found, unread := readPreviousPerformance(worktree, mainCommit)
	current.Note = performanceNote(current, previous, found, unread)
	contents, renderError := renderPerformance(current)
	if renderError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: writing %s: %v\n", performanceFileName, renderError)
		return 1
	}
	fmt.Fprint(streams.Output, string(contents))
	return 0
}

// measureOnShackler puts commit in Loom's feed and runs performanceScript on Shackler in her gap, waiting in line while
// another holds her, and returns everything the gap printed and how it ended. It returns why she never measured it
// instead, which refuses the landing: the feed didn't take it, or she stayed held through wait.
//
// A Mac's gap waits on another org's claim for as long as a claim may stand rather than refusing, so her claim is read
// first, `gap --box Shackler`, which exits 0 only while she is free and otherwise says whose claim holds her and since
// when. A claim taken between that read and the gap is waited on too, once gap says she stayed held.
func measureOnShackler(worktree string, commit string, wait time.Duration) ([]byte, string, error) {
	pushed, pushError := loomCombined(worktree, "loom", "push", "--feed", commit, "--repository", "cohere")
	if pushError != nil {
		return nil, fmt.Sprintf("Loom's feed didn't take %s (%s)", shortCommit(commit),
			lastWords(pushed, describeEnd(pushError))), nil
	}
	// Encoded, so nothing in the script is read by a shell before the box's bash: gap's command is one argument to ssh.
	remote := "echo " + base64.StdEncoding.EncodeToString([]byte(performanceScript)) + " | base64 -d | bash -s -- " +
		commit + " " + performanceReference + " " + strconv.Itoa(performanceFills) + " " +
		strconv.Itoa(performanceColdRounds) + " " + strconv.Itoa(performanceReferenceRounds) + " " +
		strconv.Itoa(performanceWarmRounds)
	started := time.Now()
	// asked is how many times her claim has been read. A land always reads it twice before it refuses, so a wait
	// shorter than one read still reads it again, as the gate's wait does.
	asked := 0
	for {
		asked++
		read, readError := loomCombined(worktree, "loom", "gap", "--box", performanceBox)
		if readError == nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: measuring %s on %s in her gap; the land lock stays held while it "+
				"runs\n", shortCommit(commit), performanceBox)
			streams.Show(streams.Errors)
			output, runError := loomCombined(worktree, "loom", "gap", "--box", performanceBox, "--within",
				performanceGapBound, "--", "ssh", performanceBox, remote)
			if !gapHeld(runError, output) {
				return output, "", runError
			}
			read, readError = output, runError
		}
		holder := lastWords(read, describeEnd(readError))
		waited := time.Since(started)
		if waited >= wait && asked >= 2 {
			return nil, fmt.Sprintf("%s stayed held through %s of waiting: %s", performanceBox,
				waited.Round(time.Second), holder), nil
		}
		again := max(min(performanceAgain, wait-waited), 0)
		fmt.Fprintf(streams.Errors, "cohere-dev: %s is held: %s; the land waits in line holding the lock to measure "+
			"this landing, reading her claim again in %s, %s of %s waited\n", performanceBox, holder,
			again.Round(time.Second), waited.Round(time.Second), wait)
		streams.Show(streams.Errors)
		time.Sleep(again)
	}
}

// readPerformance reads what the script printed in output, about commit, its gap having ended with runError, and
// returns the measurement, or why it refuses the landing: the box refused, in her words; the run ended badly without
// refusing; it said another commit or left something out; or a run exited badly or found something where the gate
// found nothing.
func readPerformance(output []byte, commit string, runError error) (performanceMeasurement, string) {
	measurement := performanceMeasurement{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	// The timeline's record is one line holding every stamp of the run.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(terminalColor.ReplaceAllString(scanner.Text(), ""))
		if why, isRefusal := strings.CutPrefix(line, performanceRefusedMarker); isRefusal {
			return performanceMeasurement{}, performanceBox + " refused to measure it: " + strings.TrimSpace(why)
		}
		said, isOurs := strings.CutPrefix(line, performanceMarker)
		if !isOurs {
			continue
		}
		kind, rest, _ := strings.Cut(said, " ")
		switch kind {
		case "tested":
			measurement.commit, measurement.platform, _ = strings.Cut(rest, " on ")
		case "machine":
			cores, cpu, _ := strings.Cut(rest, " ")
			// Ignored: cores that don't read stay 0, which the check below refuses by name.
			measurement.cores, _ = strconv.Atoi(cores)
			measurement.cpu = strings.TrimSpace(cpu)
		case "go":
			measurement.goVersion = strings.TrimSpace(rest)
		case "binary":
			measurement.binary = strings.TrimSpace(rest)
		case "load":
			when, figures, _ := strings.Cut(rest, " ")
			load, loadProblem := loadOf(figures)
			if loadProblem != "" {
				return performanceMeasurement{}, fmt.Sprintf("%s's load %s read %q: %s", performanceBox, when,
					figures, loadProblem)
			}
			if when == "before" {
				measurement.loadBefore = load
			} else {
				measurement.loadAfter = load
			}
		case "phase":
			name, milliseconds, _ := strings.Cut(rest, " ")
			// Ignored: a phase that doesn't read is said as 0s, since the phases are for reading what costs what, and
			// no number of the file comes from them.
			spent, _ := strconv.ParseInt(milliseconds, 10, 64)
			measurement.phases = append(measurement.phases, fmt.Sprintf("%s %.1fs", name, float64(spent)/1000))
		case "run":
			run, runProblem := performanceRunOf(rest)
			if runProblem != "" {
				return performanceMeasurement{}, runProblem
			}
			measurement.runs = append(measurement.runs, run)
		case "timeline":
			record := contract.TimelineRecord{}
			if decodeError := json.Unmarshal([]byte(rest), &record); decodeError != nil {
				return performanceMeasurement{}, fmt.Sprintf("%s's timeline record didn't read: %v", performanceBox,
					decodeError)
			}
			measurement.timeline = &record
		}
	}
	switch {
	case runError != nil:
		return performanceMeasurement{}, fmt.Sprintf("%s's measurement ended %s (%s)", performanceBox,
			describeEnd(runError), lastWords(output, describeEnd(runError)))
	case measurement.commit != commit:
		return performanceMeasurement{}, fmt.Sprintf("%s measured %q, not %s (%s)", performanceBox,
			measurement.commit, shortCommit(commit), lastWords(output, describeEnd(runError)))
	case measurement.platform == "" || measurement.cpu == "" || measurement.cores <= 0 ||
		measurement.goVersion == "":
		return performanceMeasurement{}, performanceBox + "'s measurement never said her platform, her cpu and cores " +
			"or her Go"
	case !sha256Pattern.MatchString(measurement.binary):
		return performanceMeasurement{}, fmt.Sprintf("%s's measurement said the binary's digest as %q, not a sha256",
			performanceBox, measurement.binary)
	case measurement.loadBefore == nil || measurement.loadAfter == nil:
		return performanceMeasurement{}, performanceBox + "'s measurement never said her load before and after"
	case measurement.timeline == nil:
		return performanceMeasurement{}, performanceBox + "'s timeline run printed no timeline record"
	}
	for _, run := range measurement.runs {
		// The engine's summary counts the finding records it wrote, so a count of the lines that differs means the
		// stream was cut or read wrong, and what the run checked can't be said.
		if run.findingsPrinted != run.findingsSummarized {
			return performanceMeasurement{}, fmt.Sprintf("%s's %s run at %s, round %s, printed %d findings and its "+
				"summary counts %d", performanceBox, run.mode, run.tree, run.round, run.findingsPrinted,
				run.findingsSummarized)
		}
		clean := run.exit == 0 && run.findingsPrinted == 0
		// deeafbf4 is an older tree, in which today's rules may find something: the engine then exits 1 with what it
		// found printed, which is that tree's honest run. Any other end there, and anything found at the root, where
		// the gate found nothing, refuses.
		if run.tree == performanceReferenceTree {
			clean = clean || (run.exit == 1 && run.findingsPrinted > 0)
		}
		if !clean {
			return performanceMeasurement{}, fmt.Sprintf("%s's %s run at %s, round %s, exited %d with %d findings, "+
				"and a measured run at the root ends 0 with none, as the gate's did", performanceBox, run.mode,
				run.tree, run.round, run.exit, run.findingsPrinted)
		}
	}
	for _, row := range []struct {
		mode, tree string
		rounds     int
	}{
		{"cold", "root", performanceColdRounds},
		{"cold", performanceReferenceTree, performanceReferenceRounds},
		{"warm", "root", performanceWarmRounds},
		{"timeline", "root", 1},
	} {
		timed := timedRuns(measurement.runs, row.mode, row.tree)
		if len(timed) != row.rounds {
			return performanceMeasurement{}, fmt.Sprintf("%s's measurement timed the %s row at %s %d times, not %d",
				performanceBox, row.mode, row.tree, len(timed), row.rounds)
		}
		// The gate decides what is correct, but a row whose rounds checked different things measures nothing: its
		// median is of runs that did different work.
		for _, run := range timed[min(1, len(timed)):] {
			if run.filesWalked != timed[0].filesWalked || run.findingsPrinted != timed[0].findingsPrinted {
				return performanceMeasurement{}, fmt.Sprintf("%s's %s row at %s checked %d files and %d findings in "+
					"round %s, and %d files and %d findings in round %s, so its rounds measured different things",
					performanceBox, row.mode, row.tree, timed[0].filesWalked, timed[0].findingsPrinted, timed[0].round,
					run.filesWalked, run.findingsPrinted, run.round)
			}
		}
	}
	return measurement, ""
}

// performanceRunOf reads a run's line after its kind, `<mode> <tree> <round> <wall> <user> <sys> <exit> <findings>
// <filesWalked> <summaryFindings>`, or says why it doesn't read.
func performanceRunOf(rest string) (performanceRun, string) {
	fields := strings.Fields(rest)
	numbers := []int64{}
	for _, field := range fields[min(3, len(fields)):] {
		number, parseError := strconv.ParseInt(field, 10, 64)
		if parseError != nil {
			break
		}
		numbers = append(numbers, number)
	}
	if len(fields) != 10 || len(numbers) != 7 {
		return performanceRun{}, fmt.Sprintf("%s's run line %q doesn't read as a row, a round, three times, an exit, "+
			"a count of findings, the files walked and the summary's findings", performanceBox, rest)
	}
	return performanceRun{mode: fields[0], tree: fields[1], round: fields[2], wall: numbers[0], user: numbers[1],
		system: numbers[2], exit: int(numbers[3]), findingsPrinted: int(numbers[4]), filesWalked: int(numbers[5]),
		findingsSummarized: int(numbers[6])}, ""
}

// loadOf reads a load line's three figures, or says why they don't read.
func loadOf(figures string) ([]float64, string) {
	fields := strings.Fields(figures)
	if len(fields) != 3 {
		return nil, "not three figures"
	}
	load := []float64{}
	for _, field := range fields {
		figure, parseError := strconv.ParseFloat(field, 64)
		if parseError != nil {
			return nil, parseError.Error()
		}
		load = append(load, figure)
	}
	return load, ""
}

// timedRuns is the runs of a row that were timed, the fills left out.
func timedRuns(runs []performanceRun, mode string, tree string) []performanceRun {
	timed := []performanceRun{}
	for _, run := range runs {
		if run.mode == mode && run.tree == tree && run.round != "fill" {
			timed = append(timed, run)
		}
	}
	return timed
}

// rowOf is a row's times, each the median, least and most of its timed runs, and what they checked, which
// readPerformance has held the same in every round.
func rowOf(runs []performanceRun, mode string, tree string) performanceRow {
	timed := timedRuns(runs, mode, tree)
	checked := &performanceChecked{}
	if len(timed) > 0 {
		checked = &performanceChecked{Files: timed[0].filesWalked, Findings: timed[0].findingsPrinted}
	}
	of := func(measure func(run performanceRun) int64) performanceSpread {
		values := []int64{}
		for _, run := range timed {
			values = append(values, measure(run))
		}
		return spreadOf(values)
	}
	return performanceRow{
		Wall:    of(func(run performanceRun) int64 { return run.wall }),
		User:    of(func(run performanceRun) int64 { return run.user }),
		System:  of(func(run performanceRun) int64 { return run.system }),
		Checked: checked,
	}
}

// spreadOf is values' median, the mean of the middle two when there are an even number of them, and their least and
// most, all zero for none.
func spreadOf(values []int64) performanceSpread {
	if len(values) == 0 {
		return performanceSpread{}
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	middle := len(sorted) / 2
	median := sorted[middle]
	if len(sorted)%2 == 0 {
		median = (sorted[middle-1] + sorted[middle]) / 2
	}
	return performanceSpread{Median: median, Minimum: sorted[0], Maximum: sorted[len(sorted)-1]}
}

// performanceFileOf is the file a measurement of commit writes, measured at measuredAt, its note not yet written.
func performanceFileOf(measurement performanceMeasurement, commit string, measuredAt time.Time) performanceFile {
	areas := map[string]int64{}
	for _, stamp := range measurement.timeline.Stamps {
		_, taken := areas[stamp.Label]
		if stamp.Build == "" && stamp.Round == 0 && !taken && slices.Contains(performanceTimelineStamps, stamp.Label) {
			// Microseconds to milliseconds, to the nearest.
			areas[stamp.Label] = (stamp.WallMicroseconds + 500) / 1000
		}
	}
	return performanceFile{
		Schema:     performanceSchema,
		Commit:     commit,
		MeasuredAt: measuredAt.UTC().Format(time.RFC3339),
		Machine: performanceMachine{
			Name:       performanceBox,
			Cpu:        measurement.cpu,
			Cores:      measurement.cores,
			LoadBefore: measurement.loadBefore,
			LoadAfter:  measurement.loadAfter,
			HeldQuiet:  true,
		},
		Tools: performanceTools{
			Cohere: shortCommit(commit) + " " + performanceBuild + ", " + measurement.platform +
				", on a cache the box kept",
			Go:     measurement.goVersion,
			Binary: measurement.binary,
		},
		Engine: performanceEngine{
			Rounds: performanceRounds{
				Cold:     performanceColdRounds,
				Deeafbf4: performanceReferenceRounds,
				Warm:     performanceWarmRounds,
			},
			Cold: performanceColdRows{
				Root:     rowOf(measurement.runs, "cold", "root"),
				Deeafbf4: rowOf(measurement.runs, "cold", performanceReferenceTree),
			},
			Warm: performanceWarmRows{Root: rowOf(measurement.runs, "warm", "root")},
			Timeline: performanceTimelineRows{Root: performanceTimeline{
				Listing:  areas["host.listing.end"],
				Handover: areas["handover.end"],
				Builds:   areas["builds.end"],
				Run:      areas["run.end"],
			}},
		},
	}
}

// readPreviousPerformance is main's Performance.jsonc at mainCommit, read: found is false when main holds none, as
// before the first measured landing, and unread names why one main holds didn't read.
func readPreviousPerformance(worktree string, mainCommit string) (performanceFile, bool, string) {
	mode, treeModeError := treeMode(worktree, mainCommit, performanceFileName)
	if treeModeError != nil {
		return performanceFile{}, true, treeModeError.Error()
	}
	if mode == "" {
		return performanceFile{}, false, ""
	}
	contents, gitOutputError := gitOutput(worktree, "show", mainCommit+":"+performanceFileName)
	if gitOutputError != nil {
		return performanceFile{}, true, gitOutputError.Error()
	}
	previous := performanceFile{}
	if decodeError := json.Unmarshal([]byte(performanceBody(contents)), &previous); decodeError != nil {
		return performanceFile{}, true, decodeError.Error()
	}
	return previous, true, ""
}

// performanceBody is the file's contents without the comment lines that open it, which JSON doesn't read.
func performanceBody(contents string) string {
	lines := strings.Split(contents, "\n")
	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "//") {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// performanceNote is the file's one line on how current compares with previous, main's file before it: level or moved
// per row, by wall and by user time. With no previous it says so, and when previous didn't read, it names why. When
// previous was measured on another machine, by another build or in another shape, that is its line, never a
// comparison. A row whose files or findings checked differ from previous's same row is its own line, never compared,
// since a row that checked less would read faster. A previous file that records no checked counts, as those written
// before rows recorded them, is said once, and its times compared as they always were.
func performanceNote(current performanceFile, previous performanceFile, found bool, unread string) string {
	switch {
	case !found:
		return "the first measurement, so nothing to compare"
	case unread != "":
		return "main's " + performanceFileName + " didn't read (" + unread + "), so nothing is compared"
	}
	against := shortCommit(previous.Commit)
	if machine, before := machineNamed(current.Machine), machineNamed(previous.Machine); machine != before {
		return fmt.Sprintf("measured on %s, and main's at %s on %s: another machine, so not compared", machine,
			against, before)
	}
	if build, before := buildNamed(current.Tools), buildNamed(previous.Tools); build != before {
		return fmt.Sprintf("built as %s, and main's at %s as %s: another build, so not compared", build, against,
			before)
	}
	if previous.Schema != current.Schema || previous.Engine.Rounds != current.Engine.Rounds {
		return fmt.Sprintf("main's at %s was measured in another shape (schema %d, rounds %d/%d/%d), so not compared",
			against, previous.Schema, previous.Engine.Rounds.Cold, previous.Engine.Rounds.Deeafbf4,
			previous.Engine.Rounds.Warm)
	}
	// The same binary as main's ran: this landing changed nothing the engine compiles, so every difference is the
	// box's own drift between two measurements of one program, which the note says rather than calling it slower.
	sameBinary := current.Tools.Binary != "" && current.Tools.Binary == previous.Tools.Binary
	compared := spreadCompared
	if sameBinary {
		compared = driftCompared
	}
	rows := []string{}
	uncounted := false
	for _, row := range performanceRows {
		now, before := row.row(current), row.row(previous)
		if before.Checked == nil {
			uncounted = true
		} else if now.Checked != nil && *now.Checked != *before.Checked {
			rows = append(rows, fmt.Sprintf("%s checked %d files and %d findings, and main's %d and %d, so not "+
				"compared", row.name, now.Checked.Files, now.Checked.Findings, before.Checked.Files,
				before.Checked.Findings))
			continue
		}
		rows = append(rows, row.name+" wall "+compared(now.Wall, before.Wall)+", user "+
			compared(now.User, before.User))
	}
	if uncounted {
		rows = append([]string{"main's file records no checked counts"}, rows...)
	}
	if sameBinary {
		return "against " + against + ", the same binary (sha256 " + current.Tools.Binary[:12] + "), so no change " +
			"to the engine and any difference is the box's: " + strings.Join(rows, "; ")
	}
	return "against " + against + ": " + strings.Join(rows, "; ")
}

// spreadCompared says how now compares with before: level while their ranges overlap, and moved, up or down by how
// much of before's median, once they don't.
//
// The file is awareness, never a gate on speed (Kirk, Nov 21: "It's ok if we need to add cool shit and slow down total
// time, we are just aware of the change"), so a change reads as a plain fact, slower or faster by so much with the
// ranges apart, and nothing refuses or holds a landing for it.
func spreadCompared(now performanceSpread, before performanceSpread) string {
	figures := fmt.Sprintf("%d ms, was %d", now.Median, before.Median)
	if now.Minimum <= before.Maximum && before.Minimum <= now.Maximum {
		return "level (" + figures + ")"
	}
	direction := "slower"
	if now.Median < before.Median {
		direction = "faster"
	}
	change := now.Median - before.Median
	if change < 0 {
		change = -change
	}
	if before.Median <= 0 {
		return fmt.Sprintf("%s by %d ms, ranges apart (%s)", direction, change, figures)
	}
	return fmt.Sprintf("%s by %d ms, %d%%, ranges apart (%s)", direction, change,
		(change*100+before.Median/2)/before.Median, figures)
}

// driftCompared says how two measurements of one binary compare: level while their ranges overlap, and otherwise
// drifted up or down by so much, which is the box's spread between runs and never the engine's speed.
func driftCompared(now performanceSpread, before performanceSpread) string {
	said := spreadCompared(now, before)
	for _, word := range []string{"slower", "faster"} {
		if rest, found := strings.CutPrefix(said, word+" by "); found {
			direction := "up"
			if word == "faster" {
				direction = "down"
			}
			return "drifted " + direction + " by " + strings.Replace(rest, "ranges apart", "the box's ranges apart", 1)
		}
	}
	return said
}

// machineNamed is a machine as the note names it: her name, her cpu and her cores.
func machineNamed(machine performanceMachine) string {
	return fmt.Sprintf("%s (%s, %d cores)", machine.Name, machine.Cpu, machine.Cores)
}

// buildNamed is a build as the note names it: how the binary was built, the commit it was built at left out, and the
// Go that built it.
func buildNamed(tools performanceTools) string {
	_, how, _ := strings.Cut(tools.Cohere, " ")
	return how + " with " + tools.Go
}

// renderPerformance is the file's bytes: its header, then its body in the house format. No printer reads .jsonc, so
// the body is printed as the JSON it is.
func renderPerformance(file performanceFile) ([]byte, error) {
	body, marshalError := json.Marshal(file)
	if marshalError != nil {
		return nil, marshalError
	}
	formatted, formatError := native.FormatHouse(strings.TrimSuffix(performanceFileName, "c"), body)
	if formatError != nil {
		return nil, formatError
	}
	return append([]byte(performanceHeader), formatted...), nil
}

// spliceIntoTree is tree with the file name at its root holding contents: the bytes hashed as a blob, and the tree
// written from an index of its own that reads tree and adds the blob, so no working tree and no other index moves.
func spliceIntoTree(worktree string, tree string, name string, contents []byte) (string, error) {
	hashing := exec.Command("git", "hash-object", "-w", "--stdin")
	hashing.Dir, hashing.Stdin = worktree, bytes.NewReader(contents)
	var hashingErrors bytes.Buffer
	hashing.Stderr = &hashingErrors
	blob, hashError := hashing.Output()
	if hashError != nil {
		return "", fmt.Errorf("git hash-object: %w: %s", hashError, strings.TrimSpace(hashingErrors.String()))
	}
	directory, directoryError := os.MkdirTemp("", "cohere-dev-performance-")
	if directoryError != nil {
		return "", directoryError
	}
	defer func() {
		if removeError := os.RemoveAll(directory); removeError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: the landing's scratch index %s stays: %v\n", directory,
				removeError)
		}
	}()
	index := filepath.Join(directory, "index")
	withIndex := func(arguments ...string) (string, error) {
		command := exec.Command("git", arguments...)
		command.Dir, command.Env = worktree, append(os.Environ(), "GIT_INDEX_FILE="+index)
		output, outputError := command.CombinedOutput()
		if outputError != nil {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), outputError,
				strings.TrimSpace(string(output)))
		}
		return strings.TrimSpace(string(output)), nil
	}
	if _, readError := withIndex("read-tree", tree); readError != nil {
		return "", readError
	}
	if _, updateError := withIndex("update-index", "--add", "--cacheinfo",
		"100644,"+strings.TrimSpace(string(blob))+","+name); updateError != nil {
		return "", updateError
	}
	return withIndex("write-tree")
}
