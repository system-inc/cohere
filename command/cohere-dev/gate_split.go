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

	"github.com/system-inc/cohere/internal/streams"
)

// The landing gate split across two house Macs (#bc8nf0r). One gate on Sun2 took 41m48s: its plain tests 26m45s, nearly
// all of it internal/go_engine's test binary (1,522.7s), then the -race tokens 10m29s and the -race runs 3m20s, and
// internal/go_engine alone on an idle 12-core Mac takes 875s at a load of 11. The gate is CPU-bound, so it runs as two
// halves at once, one per box, both on the commit being landed, each in its own gap, on the Square's terms:
//
//   - The gate's packages are listed here, `go list` of the module's patterns at the commit in the gate's environment,
//     and each half is handed its packages as import paths in place of the patterns, with its share of the race tokens.
//     The split is the Square's starting one, internal/go_engine alone on one half and everything else on the other.
//     A package's Elapsed is its test binary's wall, not its CPU, and the box is CPU-bound: go_engine fills every core
//     while most binaries use one or two, so balancing summed Elapsed would load go_engine's half with more. The
//     halves' times are still kept (gateTimesFile), for a balancer built from the halves' own walls.
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
// the last split gate, plain and under -race, which the next split weighs its halves by.
const gateTimesFile = "gate-times.json"

// gateSplitStartingDirectory is the package the Square's starting split runs alone on one half, with every other
// package and every race token on the other, until a split gate has written gateTimesFile: its test binary is most of
// the gate's plain run.
const gateSplitStartingDirectory = "internal/go_engine"

// gateSplitUnweighed is the seconds a package or race token with no time in gateTimesFile weighs.
const gateSplitUnweighed = 1.0

// gateTimes is gateTimesFile: each package's last plain Elapsed and each race token's package's last -race Elapsed,
// in seconds, by import path.
type gateTimes struct {
	Plain map[string]float64 `json:"plain"`
	Race  map[string]float64 `json:"race"`
}

// gateListing is what the gate tests, as `go list` read it here: every package of the module's patterns, by import
// path in the listing's order, and each race token's package.
type gateListing struct {
	packages []string
	// racePackages is each race token's package, by the token.
	racePackages map[string]string
	// startingPackage is the import path at gateSplitStartingDirectory, empty when the tree has none there.
	startingPackage string
}

// gateHalf is one half of a split gate: the box that runs it, the packages it tests by import path, and its race
// tokens, each as racePatterns writes it.
type gateHalf struct {
	box      string
	packages []string
	races    []string
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

// runSplitGate runs the gate's tests as two halves at once on two of boxes, commit already in Loom's feed with its pin,
// and returns their one verdict. With fewer than two packages to split, or with no second box free once one has been
// waited for, or with every box held, it returns the boxes the whole gate runs on instead and why.
func runSplitGate(
	worktree string,
	commit string,
	pin string,
	patterns []string,
	races []string,
	corpora []gateCorpus,
	boxes []string,
	testing []string,
) gateSplit {
	wait, waitError := waitSetting(gateSplitWaitVariable, os.Getenv(gateSplitWaitVariable), gateSplitWaitDefault)
	if waitError != nil {
		return gateSplit{unavailable: waitError.Error()}
	}
	listing, listError := listGatePackages(worktree, patterns, races, testing)
	if listError != nil {
		return gateSplit{unavailable: fmt.Sprintf("the gate's packages couldn't be listed, so no half was sent: %v; "+
			"%s=%s runs the whole gate on one box", listError, gateSplitVariable, gateSplitOffSetting)}
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
	halves := splitGate(listing, races, times)
	halves[0].box, halves[1].box = free[0], free[1]
	verdict, unavailable := runGateHalves(worktree, commit, pin, corpora, listing, races, halves, timesFile, times)
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
	listing.startingPackage = byDirectory[filepath.Join(root, filepath.FromSlash(gateSplitStartingDirectory))]
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

// splitGate splits the listing's packages and the race tokens into two halves. With a package at
// gateSplitStartingDirectory it is the Square's starting split, times or none: that package alone on the first half,
// every other package and every race token on the second, since Elapsed is a binary's wall rather than its CPU, and
// balancing walls on a CPU-bound box would put more beside go_engine, which already fills every core. With no package
// there, each package and token goes, heaviest first, onto the lighter half, weighing its last time or
// gateSplitUnweighed, and a half left with no package takes the other's lightest, since a go test handed no package
// tests the directory it runs in. The listing holds two packages at least.
func splitGate(listing gateListing, races []string, times *gateTimes) [2]gateHalf {
	halves := [2]gateHalf{}
	if listing.startingPackage != "" {
		halves[0].packages = []string{listing.startingPackage}
		for _, importPath := range listing.packages {
			if importPath != listing.startingPackage {
				halves[1].packages = append(halves[1].packages, importPath)
			}
		}
		halves[1].races = slices.Clone(races)
		return halves
	}
	weighed := times
	if weighed == nil {
		weighed = &gateTimes{}
	}
	// item is one package or race token, with what it weighs.
	type item struct {
		name   string
		race   bool
		weight float64
	}
	items := []item{}
	for _, importPath := range listing.packages {
		items = append(items, item{name: importPath, weight: weightOf(weighed.Plain, importPath)})
	}
	for _, token := range races {
		items = append(items, item{name: token, race: true,
			weight: weightOf(weighed.Race, listing.racePackages[token])})
	}
	// Heaviest first, then packages before tokens, then by name, so one listing and one file always split alike.
	slices.SortFunc(items, func(one item, other item) int {
		kind := func(each item) int {
			if each.race {
				return 1
			}
			return 0
		}
		return cmp.Or(cmp.Compare(other.weight, one.weight), cmp.Compare(kind(one), kind(other)),
			cmp.Compare(one.name, other.name))
	})
	// loads is the seconds each half holds so far.
	loads := [2]float64{}
	for _, each := range items {
		target := 0
		if loads[1] < loads[0] {
			target = 1
		}
		loads[target] += each.weight
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
	}
	return halves
}

// weightOf is the seconds name weighs in times, gateSplitUnweighed when it has none there.
func weightOf(times map[string]float64, name string) float64 {
	if seconds, timed := times[name]; timed {
		return seconds
	}
	return gateSplitUnweighed
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
	output      []byte
	stayedHeld  bool
	unavailable string
	runError    error
}

// runGateHalves runs both halves at once, each on its box in her gap, each laying the corpora she lacks, and returns
// their one verdict, or why there is none, naming the box: a half that gave no verdict, refused, or, in a verdict with
// no failure, didn't cover exactly what it was given. A red verdict is returned as it is, its failures named. It
// rewrites the gate's times at timesFile from both halves once both gave a verdict.
func runGateHalves(
	worktree string,
	commit string,
	pin string,
	corpora []gateCorpus,
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
			fmt.Fprintf(lines, "cohere-dev: testing %s's half on %s in her gap, %d packages%s; the land lock stays "+
				"held while it runs\n", shortCommit(commit), half.box, len(half.packages), racesNamed(half))
			remote := macBoxRemoteCommand(half.box, commit, pin, half.races, corpora, half.packages)
			output, stayedHeld, unavailable, runError := runOnGateBox(worktree, half.box, remote, corpora, lines)
			runs[index] = halfRun{output: output, stayedHeld: stayedHeld, unavailable: unavailable, runError: runError}
		})
	}
	group.Wait()
	verdicts := [2]boxVerdict{}
	for index, half := range halves {
		run := runs[index]
		switch {
		case run.unavailable != "":
			return boxVerdict{}, run.unavailable
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
	writeGateTimes(timesFile, times, listing, verdicts)
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

// splitCoverageGaps is every way the halves' verdicts fail to cover the gate exactly once, each naming its package or
// token: a half that never reported a package it was given, reported one it wasn't, or reported one twice, the same
// of its -race run against its race tokens' packages when it ran one, and across both halves a listed package not
// reported once or a race token not given to one half. A half whose plain run failed ran no -race run, and its
// failure refuses the landing on its own.
func splitCoverageGaps(listing gateListing, races []string, halves [2]gateHalf, verdicts [2]boxVerdict) []string {
	gaps := []string{}
	reportedAcross := map[string]int{}
	for index, half := range halves {
		verdict := verdicts[index]
		gaps = append(gaps, reportGaps(half.box+"'s run", half.packages, verdict.packagesReported)...)
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
// whose plain run failed and so ran no -race run. A time it can't write is said, and the next split weighs as this one.
func writeGateTimes(path string, previous *gateTimes, listing gateListing, verdicts [2]boxVerdict) {
	if path == "" {
		return
	}
	times := gateTimes{Plain: map[string]float64{}, Race: map[string]float64{}}
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

// mergeHalfVerdicts is the halves' one verdict: counts summed, maps and lists united, each half kept whole under its
// box's name for its steps and its slowest binaries, and its -race runs merged alike. Halves that tested on two
// platforms are no one gate, which refuses the landing.
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
	return merged, ""
}

// mergedVerdict is a verdict of box summing parts, each a half's: their counts, lists and maps, and each part itself
// kept whole among its parts. The halves tested disjoint packages, so no map's key is in two.
func mergedVerdict(box string, parts []boxVerdict) boxVerdict {
	merged := boxVerdict{box: box, failedOutput: map[string][]string{}, skipped: map[string][]string{},
		optIn: map[string][]string{}, timeouts: map[string][]string{}, packageSeconds: map[string]float64{},
		parts: slices.Clone(parts)}
	for _, part := range parts {
		merged.packagesPassed += part.packagesPassed
		merged.packagesFailed = append(merged.packagesFailed, part.packagesFailed...)
		merged.failedTests = append(merged.failedTests, part.failedTests...)
		merged.raceReports = append(merged.raceReports, part.raceReports...)
		merged.packagesReported = append(merged.packagesReported, part.packagesReported...)
		maps.Copy(merged.failedOutput, part.failedOutput)
		maps.Copy(merged.skipped, part.skipped)
		maps.Copy(merged.optIn, part.optIn)
		maps.Copy(merged.timeouts, part.timeouts)
		maps.Copy(merged.packageSeconds, part.packageSeconds)
	}
	return merged
}
