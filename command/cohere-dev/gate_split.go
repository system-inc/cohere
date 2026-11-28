package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/system-inc/cohere/internal/command_line/streams"
)

// The landing gate split across two house Macs (#bc8nf0r). One gate on Sun2 took 41m48s: its plain tests 26m45s, nearly
// all of it internal/go_language's test binary (1,522.7s), then the -race tokens 10m29s and the -race runs 3m20s, and
// internal/go_language alone on an idle 12-core Mac takes 875s at a load of 11. The gate is CPU-bound, so it runs as
// two halves at once, one per box, both on the commit being landed, each in its own gap, on the Square's terms:
//
//   - The gate's packages are listed here, `go list` of the module's patterns at the commit in the gate's environment,
//     and each half is handed its packages as import paths in place of the patterns, with its share of the race tokens.
//   - The split is by measured time, not by name (the Square's ruling, #q1mvjgg). The starting split put
//     internal/go_language alone on one half, which was right while that package took 26 minutes; sharding cut it to
//     about 100s and the split never moved, so Sun2's half ended in 1m35s while Marin's ran about 21.5 minutes (Nov
//     25, the last six landings). Now every package and race token is placed by its last time (gateTimesFile), and
//     each half's estimate is calibrated by its box's last measured wall (gateHalfRecord), since a package's Elapsed
//     is its own wall under its box's contention and a half's wall is not the sum of its items: about 1,290s of wall
//     for 2,760s of summed times on Marin. The starting split stays for a pool with no times to weigh by.
//   - Coverage is proven, never assumed: each half's report names exactly the packages it was given, its -race run
//     exactly its race tokens' packages, and across both every listed package and every race token appears once. A
//     package missing, doubled or unexpected refuses the landing, naming it.
//   - The verdict is one verdict, green only when both halves are, with each half's box named.
//   - With one box held, the split waits gateSplitWaitVariable's wait for her, then the whole gate runs on the free
//     box, the land line saying why. With both held, the whole gate waits in line as it always has. A gate is never
//     half run: a half that can't run leaves no landing.

// gateSplitVariable is gateSplitOffSetting to run a landing's whole gate on one box, as before the split. Unset or
// "on", a landing with two gate boxes free splits its tests across them.
const gateSplitVariable = "COHERE_DEV_GATE_SPLIT"

// gateSplitOffSetting is the gateSplitVariable setting that runs the whole gate on one box.
const gateSplitOffSetting = "off"

// gateSplitWaitVariable is how long a split waits, as Go writes a duration, for its second box while one is free and
// the other held, before the whole gate runs on the free one. Unset, it is gateSplitWaitDefault.
const gateSplitWaitVariable = "COHERE_DEV_GATE_SPLIT_WAIT"

// gateSplitWaitDefault is the Square's wait for a split's second box: a split gate takes under 20 minutes against the
// whole one's 42, so waiting longer than this for the second box saves nothing.
const gateSplitWaitDefault = 10 * time.Minute

// gateTimesFile is where, beside the pool's other state, the gate keeps the seconds each package's test binary took in
// the last split gate, plain and under -race, and each box's last half, which the next split weighs its halves by.
const gateTimesFile = "gate-times.json"

// gateShardedDirectory is the package the box runs in shards (boxShardTable's first row), by its directory. It runs
// alone after a half's plain run on all of her cores, so its weight counts at full value on either half rather than
// scaled by the box's factor, and the Square's starting split runs it alone on one half until a split gate has written
// gateTimesFile.
const gateShardedDirectory = "internal/go_language"

// gateSplitUnweighed is the seconds a package or race token weighs when gateTimesFile holds no time of its kind at all.
const gateSplitUnweighed = 1.0

// gateSplitDefaultFactor is a box's factor, her half's wall over its summed item seconds, when neither box has a half
// recorded: about what Marin measured, 1,290s of wall for 2,760s of summed times (Nov 25).
const gateSplitDefaultFactor = 0.5

// gateTimes is gateTimesFile: each package's last plain Elapsed and each race token's package's last -race Elapsed,
// in seconds, by import path, and each box's last half, by her name. A file written before the halves were kept reads
// with none.
type gateTimes struct {
	Plain  map[string]float64        `json:"plain"`
	Race   map[string]float64        `json:"race"`
	Halves map[string]gateHalfRecord `json:"halves,omitempty"`
}

// gateHalfRecord is what a box's last half of a split gate measured, in seconds: its items' summed Elapsed, plain and
// -race, the sharded package's apart, the sharded package's slowest shard, and its wall, the sum of its steps.
type gateHalfRecord struct {
	Weight  float64 `json:"weight"`
	Sharded float64 `json:"sharded"`
	Wall    float64 `json:"wall"`
}

// gateListing is what the gate tests, as `go list` read it here: every package of the module's patterns, by import
// path in the listing's order, and each race token's package.
type gateListing struct {
	packages []string
	// racePackages is each race token's package, by the token.
	racePackages map[string]string
	// shardedPackage is the import path at gateShardedDirectory, empty when the tree has none there.
	shardedPackage string
}

// gateHalf is one half of a split gate: the box that runs it, the packages it tests by import path, and its race
// tokens, each as racePatterns writes it.
type gateHalf struct {
	box      string
	packages []string
	races    []string
	// weighed is whether the split placed the half by recorded times, and estimate then its wall as the split
	// reckoned it; the starting split, with no times, reckons none.
	weighed  bool
	estimate time.Duration
}

// gateSplit is what a split gate came to: its verdict or why it gave none, which refuses the landing, or, when it
// never split, the boxes the whole gate runs on in the order they are tried, and why it runs whole.
type gateSplit struct {
	verdict     boxVerdict
	unavailable string
	// whole is nil when the split ran, and because, said before ", so the whole gate ran on <box>", is why it didn't.
	whole   []string
	because string
}

// gateSplitSetting reads gateSplitVariable's setting: whether a landing splits its gate, refused when it is neither
// on nor off.
func gateSplitSetting(setting string) (bool, error) {
	switch strings.TrimSpace(setting) {
	case "", "on":
		return true, nil
	case gateSplitOffSetting:
		return false, nil
	}
	return false, fmt.Errorf("%s=%q is neither on nor %s", gateSplitVariable, setting, gateSplitOffSetting)
}

// runSplitGate runs the gate's tests, listing's packages and races, as two halves at once on two of boxes, commit
// already in Loom's feed with its pin, and returns their one verdict. With fewer than two packages to split, or with
// no second box free once one has been waited for, or with every box held, it returns the boxes the whole gate runs on
// instead and why.
func runSplitGate(
	worktree string,
	commit string,
	pin string,
	listing gateListing,
	races []string,
	boxes []string,
) gateSplit {
	wait, waitError := waitSetting(gateSplitWaitVariable, os.Getenv(gateSplitWaitVariable), gateSplitWaitDefault)
	if waitError != nil {
		return gateSplit{unavailable: waitError.Error()}
	}
	if len(listing.packages) < 2 {
		return gateSplit{whole: boxes, because: fmt.Sprintf("the gate lists %d packages, too few to split",
			len(listing.packages))}
	}
	free, whole, because := claimSplitBoxes(worktree, boxes, wait)
	if free == nil {
		return gateSplit{whole: whole, because: because}
	}
	times, timesFile := readGateTimes()
	halves := splitGate(listing, races, times, [2]string{free[0], free[1]})
	verdict, unavailable := runGateHalves(worktree, commit, pin, listing, races, halves, timesFile, times)
	return gateSplit{verdict: verdict, unavailable: unavailable}
}

// claimSplitBoxes reads each box's claim, and returns the two boxes a split runs on, the first two free. With one free,
// it reads the others again every gateBoxAgain until one is free too or wait has passed, and then returns nil with
// the boxes the whole gate runs on, the free one first, and which stayed held for how long. With none free, it returns
// nil with the boxes as they are, for the whole gate's own wait in line.
func claimSplitBoxes(worktree string, boxes []string, wait time.Duration) ([]string, []string, string) {
	started := time.Now()
	// asked is how many times every box has been asked: twice at least before the split gives up, as the whole gate's
	// wait asks, so a wait shorter than one round of asking still asks again.
	asked := 0
	for {
		asked++
		free := []string{}
		held := []string{}
		for _, box := range boxes {
			if words := gateBoxHeld(worktree, box); words != "" {
				fmt.Fprintf(streams.Errors, "cohere-dev: %s is held: %s\n", box, words)
				held = append(held, box)
				continue
			}
			free = append(free, box)
		}
		switch {
		case len(free) >= 2:
			return free[:2], nil, ""
		case len(free) == 0:
			return nil, boxes, "every gate box was held when the split asked"
		}
		waited := time.Since(started)
		if waited >= wait && asked >= 2 {
			return nil, append(free, held...), fmt.Sprintf("%s stayed held %s", strings.Join(held, " and "),
				waited.Round(time.Second))
		}
		again := max(min(gateBoxAgain, wait-waited), 0)
		fmt.Fprintf(streams.Errors, "cohere-dev: %s is free and %s held; the split waits for a second box holding "+
			"the lock, asking again in %s, %s of %s waited, then the whole gate runs on %s\n", free[0],
			strings.Join(held, " and "), again.Round(time.Second), waited.Round(time.Second), wait, free[0])
		streams.Show(streams.Errors)
		time.Sleep(again)
	}
}

// listGatePackages lists what the gate tests, `go list` of patterns in worktree in the gate's environment testing,
// and finds each race token's package by its directory. A token naming no listed package is an error: the -race run
// it asks for would be one no half's coverage could vouch for.
func listGatePackages(worktree string, patterns []string, races []string, testing []string) (gateListing, error) {
	// -e, so a package that can't build is listed all the same, and its half's go test reports it failed.
	command := exec.Command("go", append([]string{"list", "-e", "-f", "{{.ImportPath}} {{.Dir}}"}, patterns...)...)
	command.Dir, command.Env = worktree, testing
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, listError := command.Output()
	if listError != nil {
		return gateListing{}, fmt.Errorf("go list: %w: %s", listError, strings.TrimSpace(standardError.String()))
	}
	listing := gateListing{racePackages: map[string]string{}}
	byDirectory := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		importPath, directory, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found || importPath == "" || slices.Contains(listing.packages, importPath) {
			continue
		}
		listing.packages = append(listing.packages, importPath)
		byDirectory[resolvedPath(directory)] = importPath
	}
	root := resolvedPath(worktree)
	for _, token := range races {
		directory, _, _ := strings.Cut(token, raceRunSeparator)
		importPath, listed := byDirectory[filepath.Join(root, filepath.FromSlash(directory))]
		if !listed {
			return gateListing{}, fmt.Errorf("the race token %s names no package the gate lists", token)
		}
		listing.racePackages[token] = importPath
	}
	listing.shardedPackage = byDirectory[filepath.Join(root, filepath.FromSlash(gateShardedDirectory))]
	return listing, nil
}

// resolvedPath is path with its links resolved, so a directory go list names and one reached through a link, /var for
// /private/var, compare as one; path itself when it can't be resolved.
func resolvedPath(path string) string {
	resolved, resolveError := filepath.EvalSymlinks(path)
	if resolveError != nil {
		return filepath.Clean(path)
	}
	return resolved
}

// gateTimesPath is gateTimesFile's path, in the pool's directory.
func gateTimesPath() (string, error) {
	directory, directoryError := poolDirectory()
	if directoryError != nil {
		return "", directoryError
	}
	return filepath.Join(directory, gateTimesFile), nil
}

// readGateTimes reads gateTimesFile, and returns its times, nil when there is none to read, and its path, empty when
// the pool has none. A file that doesn't read is said and weighed as none, since the times only balance the halves and
// no verdict comes from them.
func readGateTimes() (*gateTimes, string) {
	path, pathError := gateTimesPath()
	if pathError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: the gate's times have no place, so the split is the Square's "+
			"starting one: %v\n", pathError)
		return nil, ""
	}
	contents, readError := os.ReadFile(path)
	if errors.Is(readError, os.ErrNotExist) {
		return nil, path
	}
	times := gateTimes{}
	if readError == nil {
		readError = json.Unmarshal(contents, &times)
	}
	if readError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: the gate's times at %s don't read, so the split is the Square's "+
			"starting one: %v\n", path, readError)
		return nil, path
	}
	return &times, path
}

// splitGate splits the listing's packages and the race tokens into two halves, the first run by boxes[0] and the second
// by boxes[1]. With no times to weigh by and a package at gateShardedDirectory it is the Square's starting split: that
// package alone on the first half, every other package and every race token on the second. Otherwise every package and
// token (gateItems) goes, heaviest first, to the half whose estimate is lower once it holds it: the sum of its items'
// weights, each scaled by its box's factor (boxFactors) but the sharded package's, at full value, since it runs alone
// on all of her cores. A half left with no package takes the other's lightest, since a go test handed no package tests
// the directory it runs in. The listing holds two packages at least.
func splitGate(listing gateListing, races []string, times *gateTimes, boxes [2]string) [2]gateHalf {
	halves := [2]gateHalf{{box: boxes[0]}, {box: boxes[1]}}
	if times == nil && listing.shardedPackage != "" {
		halves[0].packages = []string{listing.shardedPackage}
		for _, importPath := range listing.packages {
			if importPath != listing.shardedPackage {
				halves[1].packages = append(halves[1].packages, importPath)
			}
		}
		halves[1].races = slices.Clone(races)
		return halves
	}
	weighed := gateTimes{}
	if times != nil {
		weighed = *times
	}
	factors := boxFactors(boxes, weighed)
	items := gateItems(listing, races, weighed)
	// Heaviest first, then packages before tokens, then by name, so one listing and one file always split alike.
	slices.SortFunc(items, func(one gateItem, other gateItem) int {
		kind := func(each gateItem) int {
			if each.race {
				return 1
			}
			return 0
		}
		return cmp.Or(cmp.Compare(other.weight, one.weight), cmp.Compare(kind(one), kind(other)),
			cmp.Compare(one.name, other.name))
	})
	// estimates is the seconds each half is reckoned to take so far.
	estimates := [2]float64{}
	for _, each := range items {
		target := 0
		if estimates[1]+scaledWeight(each, factors[1]) < estimates[0]+scaledWeight(each, factors[0]) {
			target = 1
		}
		estimates[target] += scaledWeight(each, factors[target])
		if each.race {
			halves[target].races = append(halves[target].races, each.name)
			continue
		}
		halves[target].packages = append(halves[target].packages, each.name)
	}
	for index := range halves {
		other := &halves[1-index]
		if len(halves[index].packages) > 0 || len(other.packages) < 2 {
			continue
		}
		// The other half's packages went to it heaviest first, so its last is its lightest.
		halves[index].packages = []string{other.packages[len(other.packages)-1]}
		other.packages = other.packages[:len(other.packages)-1]
	}
	for index := range halves {
		halves[index].packages = inListingOrder(halves[index].packages, listing.packages)
		halves[index].races = inListingOrder(halves[index].races, races)
		// Reckoned again from what the half holds, since the move of a lightest package changed both halves.
		seconds := 0.0
		for _, each := range items {
			given := halves[index].packages
			if each.race {
				given = halves[index].races
			}
			if slices.Contains(given, each.name) {
				seconds += scaledWeight(each, factors[index])
			}
		}
		halves[index].weighed = times != nil
		if halves[index].weighed {
			halves[index].estimate = time.Duration(seconds * float64(time.Second))
		}
	}
	return halves
}

// gateItem is one package or race token a split places: its name, as a half is handed it, its weight in seconds, and
// whether it counts at full value on either box, which the sharded package does, rather than scaled by her factor.
type gateItem struct {
	name   string
	race   bool
	weight float64
	full   bool
}

// scaledWeight is the seconds item adds to the estimate of a half whose box's factor is factor.
func scaledWeight(item gateItem, factor float64) float64 {
	if item.full {
		return item.weight
	}
	return item.weight * factor
}

// gateItems is every package the listing holds and every race token, each weighing its last time in times. One with no
// time weighs the median of the recorded times of its kind among them, plain or -race, so a new package lands like an
// ordinary one and is timed by its next gate, and gateSplitUnweighed when its kind has none recorded.
func gateItems(listing gateListing, races []string, times gateTimes) []gateItem {
	plainRecorded := []float64{}
	for _, importPath := range listing.packages {
		if seconds, timed := times.Plain[importPath]; timed {
			plainRecorded = append(plainRecorded, seconds)
		}
	}
	raceRecorded := []float64{}
	for _, token := range races {
		if seconds, timed := times.Race[listing.racePackages[token]]; timed {
			raceRecorded = append(raceRecorded, seconds)
		}
	}
	items := []gateItem{}
	for _, importPath := range listing.packages {
		items = append(items, gateItem{name: importPath, weight: weightOf(times.Plain, importPath, plainRecorded),
			full: importPath == listing.shardedPackage})
	}
	for _, token := range races {
		items = append(items, gateItem{name: token, race: true,
			weight: weightOf(times.Race, listing.racePackages[token], raceRecorded)})
	}
	return items
}

// weightOf is the seconds name weighs in times, and when it has none there the median of recorded, the times of its
// kind, or gateSplitUnweighed when recorded holds none.
func weightOf(times map[string]float64, name string, recorded []float64) float64 {
	if seconds, timed := times[name]; timed {
		return seconds
	}
	if len(recorded) == 0 {
		return gateSplitUnweighed
	}
	sorted := slices.Sorted(slices.Values(recorded))
	middle := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[middle-1] + sorted[middle]) / 2
	}
	return sorted[middle]
}

// boxFactors is the factor of each of boxes, her half's wall over its summed item seconds, which scales an item's
// weight into her half's estimate: her last half's (boxFactor), or with none the other box's, or with neither
// gateSplitDefaultFactor.
func boxFactors(boxes [2]string, times gateTimes) [2]float64 {
	factors := [2]float64{}
	recorded := [2]bool{}
	for index, box := range boxes {
		factors[index], recorded[index] = boxFactor(times.Halves[box])
	}
	for index := range factors {
		if recorded[index] {
			continue
		}
		factors[index] = gateSplitDefaultFactor
		if recorded[1-index] {
			factors[index] = factors[1-index]
		}
	}
	return factors
}

// boxFactor is a box's factor from her last half, record, and whether it gives one: her wall, the sharded package's
// seconds taken out, over her summed item seconds. The sharded package is taken out because a split counts it at full
// value, so the factor reckons that half's own items back to its measured wall. A half with no item seconds or no wall
// left gives none, as does the zero record of a box never recorded.
func boxFactor(record gateHalfRecord) (float64, bool) {
	if record.Weight <= 0 || record.Wall-record.Sharded <= 0 {
		return 0, false
	}
	return (record.Wall - record.Sharded) / record.Weight, true
}

// inListingOrder is names in the order order holds them, so a half's arguments read as the listing does.
func inListingOrder(names []string, order []string) []string {
	ordered := []string{}
	for _, name := range order {
		if slices.Contains(names, name) {
			ordered = append(ordered, name)
		}
	}
	return ordered
}

// halfRun is what one half's gap came to, as runOnGateBox returns it.
type halfRun struct {
	output     []byte
	stayedHeld bool
	runError   error
}

// runGateHalves runs both halves at once, each on its box in her gap, and returns their one verdict, or why there is
// none, naming the box: a half that gave no verdict, refused, or, in a verdict with no failure, didn't cover exactly
// what it was given. A red verdict is returned as it is, its failures named. It rewrites the gate's times at timesFile
// from both halves once both gave a verdict.
func runGateHalves(
	worktree string,
	commit string,
	pin string,
	listing gateListing,
	races []string,
	halves [2]gateHalf,
	timesFile string,
	times *gateTimes,
) (boxVerdict, string) {
	lines := gateLines{lock: &sync.Mutex{}}
	runs := [2]halfRun{}
	var group sync.WaitGroup
	for index, half := range halves {
		group.Go(func() {
			// Ignored: a line to the terminal that never arrives loses nothing the verdict needs, and the
			// stream keeps its error for streams.Finish to report.
			fmt.Fprintf(lines, "cohere-dev: testing %s's half on %s in her gap, %d packages%s%s; the land lock "+
				"stays held while it runs\n", shortCommit(commit), half.box, len(half.packages), racesNamed(half),
				estimateNamed(half))
			remote := macBoxRemoteCommand(half.box, commit, pin, half.races, half.packages)
			output, stayedHeld, runError := runOnGateBox(worktree, half.box, remote)
			runs[index] = halfRun{output: output, stayedHeld: stayedHeld, runError: runError}
		})
	}
	group.Wait()
	verdicts := [2]boxVerdict{}
	for index, half := range halves {
		run := runs[index]
		switch {
		// A box taken between her claim's read and her half's gap never ran her half, and a gate is never half run.
		case run.stayedHeld:
			return boxVerdict{}, fmt.Sprintf("%s was taken before her half ran (%s), and a gate is never half run",
				half.box, lastWords(run.output, describeEnd(run.runError)))
		case run.runError != nil && len(run.output) == 0:
			return boxVerdict{}, "the gap on " + half.box + " could not run: " + run.runError.Error()
		}
		verdict, unavailable := readBoxRun(run.output, run.runError, half.box, commit, half.races)
		if unavailable != "" {
			return boxVerdict{}, unavailable
		}
		// A half's refusal refuses the landing in its box's own words, as a whole gate's does.
		if verdict.refused != "" {
			return verdict, ""
		}
		verdicts[index] = verdict
	}
	writeGateTimes(timesFile, times, listing, halves, verdicts)
	merged, unavailable := mergeHalfVerdicts(halves, verdicts)
	if unavailable != "" {
		return boxVerdict{}, unavailable
	}
	// A red half refuses the landing naming its failing tests, which a coverage gap said first would hide: a failed
	// run's report can be short of what it was given without the gate being any less red.
	if merged.failed() || (merged.race != nil && merged.race.failed()) {
		return merged, ""
	}
	if gaps := splitCoverageGaps(listing, races, halves, verdicts); len(gaps) > 0 {
		return boxVerdict{}, fmt.Sprintf("the split gate's halves didn't cover its packages exactly once each, so "+
			"its verdict is none: %s", strings.Join(gaps, "; "))
	}
	return merged, ""
}

// racesNamed is how a half's race tokens are counted after its packages, empty when it has none.
func racesNamed(half gateHalf) string {
	if len(half.races) == 0 {
		return ""
	}
	return fmt.Sprintf(" and %d -race tokens", len(half.races))
}

// estimateNamed is how a half's estimate is said after its counts, empty for a half the starting split placed.
func estimateNamed(half gateHalf) string {
	if !half.weighed {
		return ""
	}
	return ", reckoned at " + half.estimate.Round(time.Second).String()
}

// splitCoverageGaps is every way the halves' verdicts fail to cover the gate exactly once, each naming its package,
// test or token: a half that never reported a package it was given, reported one it wasn't, or reported one twice, a
// half whose shards didn't run each listed test exactly once (sharded_run.go), the same of its -race run against its
// race tokens' packages when it ran one, and across both halves a listed package not reported once or a race token not
// given to one half. A half whose plain run failed ran no -race run, and its
// failure refuses the landing on its own.
func splitCoverageGaps(listing gateListing, races []string, halves [2]gateHalf, verdicts [2]boxVerdict) []string {
	gaps := []string{}
	reportedAcross := map[string]int{}
	for index, half := range halves {
		verdict := verdicts[index]
		gaps = append(gaps, reportGaps(half.box+"'s run", half.packages, verdict.packagesReported)...)
		for _, gap := range verdict.shardGaps {
			gaps = append(gaps, half.box+"'s shards: "+gap)
		}
		for _, importPath := range verdict.packagesReported {
			reportedAcross[importPath]++
		}
		if verdict.race == nil {
			continue
		}
		expected := []string{}
		for _, token := range half.races {
			expected = append(expected, listing.racePackages[token])
		}
		gaps = append(gaps, reportGaps(half.box+"'s -race run", expected, verdict.race.packagesReported)...)
	}
	for _, importPath := range listing.packages {
		if count := reportedAcross[importPath]; count != 1 {
			gaps = append(gaps, fmt.Sprintf("%s, which the gate lists, was reported %d times across both halves",
				importPath, count))
		}
	}
	for _, importPath := range slices.Sorted(maps.Keys(reportedAcross)) {
		if !slices.Contains(listing.packages, importPath) {
			gaps = append(gaps, fmt.Sprintf("%s was reported, which the gate doesn't list", importPath))
		}
	}
	for _, token := range races {
		given := 0
		for _, half := range halves {
			for _, halfToken := range half.races {
				if halfToken == token {
					given++
				}
			}
		}
		if given != 1 {
			gaps = append(gaps, fmt.Sprintf("the race token %s went to %d halves", token, given))
		}
	}
	return gaps
}

// reportGaps is every way reported, the packages a run said it ended, differs from given, the packages it was
// handed: one never reported, one reported that wasn't given, or one reported more often than it was given, each
// named as run says who ran it.
func reportGaps(run string, given []string, reported []string) []string {
	want := map[string]int{}
	for _, importPath := range given {
		want[importPath]++
	}
	got := map[string]int{}
	for _, importPath := range reported {
		got[importPath]++
	}
	gaps := []string{}
	for _, importPath := range slices.Sorted(maps.Keys(want)) {
		if got[importPath] == 0 {
			gaps = append(gaps, fmt.Sprintf("%s never reported %s, which it was given", run, importPath))
		}
	}
	for _, importPath := range slices.Sorted(maps.Keys(got)) {
		switch {
		case want[importPath] == 0:
			gaps = append(gaps, fmt.Sprintf("%s reported %s, which it wasn't given", run, importPath))
		case got[importPath] > want[importPath]:
			gaps = append(gaps, fmt.Sprintf("%s reported %s %d times", run, importPath, got[importPath]))
		}
	}
	return gaps
}

// writeGateTimes rewrites the gate's times at path from both halves' verdicts: each package's plain Elapsed and each
// -race package's, keeping a time the last file held for a package this landing lists but neither half timed, a half
// whose plain run failed and so ran no -race run, and each half's record under its box's name (halfRecord), keeping the
// last file's record of a box whose half measured none or who ran no half. A time it can't write is said, and the next
// split weighs as this one.
func writeGateTimes(
	path string,
	previous *gateTimes,
	listing gateListing,
	halves [2]gateHalf,
	verdicts [2]boxVerdict,
) {
	if path == "" {
		return
	}
	// Halves is written only once a half is recorded, since an empty map is omitted.
	times := gateTimes{Plain: map[string]float64{}, Race: map[string]float64{}, Halves: map[string]gateHalfRecord{}}
	for _, verdict := range verdicts {
		maps.Copy(times.Plain, verdict.packageSeconds)
		if verdict.race != nil {
			maps.Copy(times.Race, verdict.race.packageSeconds)
		}
	}
	if previous != nil {
		for _, importPath := range listing.packages {
			if seconds, timed := previous.Plain[importPath]; timed {
				if _, measured := times.Plain[importPath]; !measured {
					times.Plain[importPath] = seconds
				}
			}
		}
		for _, importPath := range listing.racePackages {
			if seconds, timed := previous.Race[importPath]; timed {
				if _, measured := times.Race[importPath]; !measured {
					times.Race[importPath] = seconds
				}
			}
		}
	}
	for index, verdict := range verdicts {
		if record, measured := halfRecord(verdict); measured {
			times.Halves[halves[index].box] = record
		}
	}
	if previous != nil {
		for box, record := range previous.Halves {
			if _, measured := times.Halves[box]; !measured {
				times.Halves[box] = record
			}
		}
	}
	contents, marshalError := json.MarshalIndent(times, "", "  ")
	if marshalError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: the gate's times weren't written: %v\n", marshalError)
		return
	}
	// Written beside and renamed into place, so a split never reads half a file.
	written := path + ".writing"
	if writeError := os.WriteFile(written, append(contents, '\n'), 0o644); writeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: the gate's times weren't written to %s: %v\n", path, writeError)
		return
	}
	if renameError := os.Rename(written, path); renameError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: the gate's times weren't written to %s: %v\n", path, renameError)
	}
}

// halfRecord is what a half's verdict measured, for its box's next factor (boxFactor): its items' summed seconds,
// plain and -race, the sharded package's apart, and its wall, the sum of its steps, each summed in its names' order
// so one verdict always records alike. A half that said no step measured no wall, and one whose plain run failed ran
// no -race run, so its wall is short of a whole half's: neither gives a record.
func halfRecord(verdict boxVerdict) (gateHalfRecord, bool) {
	wall := verdict.halfWall()
	if wall <= 0 || verdict.failed() {
		return gateHalfRecord{}, false
	}
	record := gateHalfRecord{Wall: wall.Seconds()}
	for _, importPath := range slices.Sorted(maps.Keys(verdict.packageSeconds)) {
		if slices.Contains(verdict.shardedPackages, importPath) {
			record.Sharded += verdict.packageSeconds[importPath]
			continue
		}
		record.Weight += verdict.packageSeconds[importPath]
	}
	if verdict.race != nil {
		for _, importPath := range slices.Sorted(maps.Keys(verdict.race.packageSeconds)) {
			record.Weight += verdict.race.packageSeconds[importPath]
		}
	}
	return record, true
}

// splitBalance is how the halves were placed and what each took, as the land line says it: each half's estimate beside
// the wall its steps measured, or, for the starting split, which had no times to reckon by, each half's wall alone.
func splitBalance(halves [2]gateHalf, verdicts [2]boxVerdict) string {
	said := []string{}
	for index, half := range halves {
		ran := "said no steps"
		if wall := verdicts[index].halfWall(); wall > 0 {
			ran = "ran " + wall.String()
		}
		if half.weighed {
			said = append(said, fmt.Sprintf("%s est %s, %s", half.box, half.estimate.Round(time.Second), ran))
			continue
		}
		said = append(said, half.box+" "+ran)
	}
	if !halves[0].weighed {
		return "its halves by the starting split, with no times kept: " + strings.Join(said, "; ")
	}
	return "its halves by their recorded times: " + strings.Join(said, "; ")
}

// mergeHalfVerdicts is the halves' one verdict: counts summed, maps and lists united, each half kept whole under its
// box's name for its steps and its slowest binaries, and its -race runs merged alike, with how the halves were placed
// and what each took (splitBalance). Halves that tested on two platforms are no one gate, which refuses the landing.
func mergeHalfVerdicts(halves [2]gateHalf, verdicts [2]boxVerdict) (boxVerdict, string) {
	first, second := verdicts[0], verdicts[1]
	if first.operatingSystem != second.operatingSystem || first.architecture != second.architecture {
		return boxVerdict{}, fmt.Sprintf("%s tested on %s/%s and %s on %s/%s, so the halves are no one gate",
			first.box, first.operatingSystem, first.architecture, second.box, second.operatingSystem,
			second.architecture)
	}
	boxes := first.box + " and " + second.box
	merged := mergedVerdict(boxes, verdicts[:])
	merged.commit, merged.operatingSystem, merged.architecture = first.commit, first.operatingSystem, first.architecture
	races := []boxVerdict{}
	for _, verdict := range verdicts {
		if verdict.race != nil {
			races = append(races, *verdict.race)
		}
	}
	if len(races) > 0 {
		race := mergedVerdict(boxes, races)
		merged.race = &race
	}
	described := []string{}
	for _, half := range halves {
		described = append(described, fmt.Sprintf("%s %d%s", half.box, len(half.packages), racesNamed(half)))
	}
	merged.halves = strings.Join(described, ", ")
	merged.split = splitBalance(halves, verdicts)
	return merged, ""
}

// mergedVerdict is a verdict of box summing parts, each a half's: their counts, lists and maps, and each part itself
// kept whole among its parts. The halves tested disjoint packages, so no map's key is in two.
func mergedVerdict(box string, parts []boxVerdict) boxVerdict {
	merged := boxVerdict{box: box, failedOutput: map[string][]string{}, skipped: map[string][]string{},
		optIn: map[string][]string{}, timeouts: map[string][]string{}, packageSeconds: map[string]float64{},
		shardedRuns: map[string]*shardedRun{}, shardCounts: map[string]int{}, parts: slices.Clone(parts)}
	for _, part := range parts {
		merged.packagesPassed += part.packagesPassed
		merged.packagesFailed = append(merged.packagesFailed, part.packagesFailed...)
		merged.failedTests = append(merged.failedTests, part.failedTests...)
		merged.raceReports = append(merged.raceReports, part.raceReports...)
		merged.packagesReported = append(merged.packagesReported, part.packagesReported...)
		merged.shardedPackages = append(merged.shardedPackages, part.shardedPackages...)
		merged.shardGaps = append(merged.shardGaps, part.shardGaps...)
		maps.Copy(merged.failedOutput, part.failedOutput)
		maps.Copy(merged.skipped, part.skipped)
		maps.Copy(merged.optIn, part.optIn)
		maps.Copy(merged.timeouts, part.timeouts)
		maps.Copy(merged.packageSeconds, part.packageSeconds)
		maps.Copy(merged.shardedRuns, part.shardedRuns)
		maps.Copy(merged.shardCounts, part.shardCounts)
	}
	return merged
}
