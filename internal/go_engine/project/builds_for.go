package project

import (
	"slices"
	"strings"
)

// Unbuilt is a census file no build BuildsFor gives compiles, and why.
type Unbuilt struct {
	File   string
	Reason string
}

// preferredOperatingSystems are tried after the host's own, in this order, and every other `GOOS` after them by
// name: linux, windows and darwin, which go calls first-class ports, then freebsd, so an extra build is a platform
// the files are most often built for, and a run's builds never turn on the order go tool dist list prints.
var preferredOperatingSystems = []string{"linux", "windows", "darwin", "freebsd"}

// searchBudget is how many partial choices the search for the fewest builds may weigh before it takes the greedy
// answer instead: a census whose constraints vary on enough at once would otherwise weigh combinations without end.
const searchBudget = 1 << 20

// BuildsFor is the builds every file of the census that is ours compiles under at least once: the host first,
// then the fewest extra builds that compile what it does not, sorted as they are preferred, and each file no
// target of ports compiles as Unbuilt, sorted by file. ports are what go tool dist list says the toolchain targets
// (see ReadPorts).
//
// Every extra build is on the host's `GOARCH`: our files constrain on `GOOS`, and one architecture is one set of
// sizes, so layout is the same in every build. A file only another architecture compiles is Unbuilt, naming the
// layout blind spot, until layout read without a name is handled. An extra build on another `GOOS` has cgo off,
// since cgo for a target other than the host's needs a C toolchain for it, and one on the host's `GOOS` takes the
// host's cgo, or the other where a file names `cgo` or imports "C". Tags fold into a build: candidates take each
// set of the custom tags the uncompiled files name, the host's own tags kept where none names them, and among the
// fewest builds the ones that change the fewest tags from the host's win, then the most preferred, so tags ride on
// an extra build another `GOOS` needs, or on the host's platform when nothing else needs one.
func BuildsFor(census Census, host Build, ports []Port) ([]Build, []Unbuilt, error) {
	var uncompiled []source
	var unbuilt []Unbuilt
	for _, file := range census.Ours {
		read, readError := readSource(file)
		if readError != nil {
			// The engine names an unreadable file as a gap of its own; no build can say it compiles one.
			unbuilt = append(unbuilt, Unbuilt{File: file, Reason: readError.Error()})
			continue
		}
		compiled, compileError := compiles(host, read)
		if compileError != nil {
			return nil, nil, compileError
		}
		if !compiled {
			uncompiled = append(uncompiled, read)
		}
	}
	builds := []Build{host}
	if len(uncompiled) == 0 {
		return builds, unbuilt, nil
	}

	candidates, candidatesError := candidatesFor(uncompiled, host, ports)
	if candidatesError != nil {
		return nil, nil, candidatesError
	}
	coverable := newCoverage(len(uncompiled))
	for _, each := range candidates {
		coverable.add(each.coverage)
	}
	for _, index := range fewestCovering(candidates, coverable) {
		builds = append(builds, candidates[index].build)
	}
	for index, read := range uncompiled {
		if !coverable.has(index) {
			reason, reasonError := unbuiltReason(read, host, ports)
			if reasonError != nil {
				return nil, nil, reasonError
			}
			unbuilt = append(unbuilt, Unbuilt{File: read.file, Reason: reason})
		}
	}
	slices.SortFunc(unbuilt, func(left Unbuilt, right Unbuilt) int { return strings.Compare(left.File, right.File) })
	return builds, unbuilt, nil
}

// candidate is an extra build that might be chosen: what it changes from the host's tags, and which of the
// uncompiled files it compiles.
type candidate struct {
	build    Build
	tagCost  int
	coverage coverage
}

// candidatesFor is every extra build on the host's architecture that compiles at least one of the uncompiled
// files, in the order they are preferred: by `GOOS` (see compareOperatingSystems), the host's cgo before the
// other, then by tagChoices. Each that an earlier one changing no more tags compiles everything of is left out
// (withoutDominated).
func candidatesFor(uncompiled []source, host Build, ports []Port) ([]candidate, error) {
	dimensions := dimensionsOf(uncompiled, ports)
	var onArchitecture []Port
	for _, port := range ports {
		if port.Architecture == host.Architecture &&
			!slices.ContainsFunc(onArchitecture, func(seen Port) bool { return seen == port }) {
			onArchitecture = append(onArchitecture, port)
		}
	}
	slices.SortFunc(onArchitecture, func(left Port, right Port) int {
		return compareOperatingSystems(left.OperatingSystem, right.OperatingSystem, host.OperatingSystem)
	})

	var candidates []candidate
	for _, port := range onArchitecture {
		cgoSettings := []bool{false}
		if port.OperatingSystem == host.OperatingSystem {
			cgoSettings = []bool{host.CgoEnabled}
			if dimensions.Cgo && (host.CgoEnabled || port.CgoSupported) {
				cgoSettings = append(cgoSettings, !host.CgoEnabled)
			}
		}
		for _, cgoEnabled := range cgoSettings {
			for _, set := range tagChoices(host.Tags, dimensions.Tags) {
				extra := Build{OperatingSystem: port.OperatingSystem, Architecture: port.Architecture,
					CgoEnabled: cgoEnabled, Tags: set.tags, Flags: slices.Clone(host.Flags)}
				if extra.sameAs(host) {
					continue
				}
				covered := newCoverage(len(uncompiled))
				for index, read := range uncompiled {
					compiled, compileError := compiles(extra, read)
					if compileError != nil {
						return nil, compileError
					}
					if compiled {
						covered.set(index)
					}
				}
				if !covered.empty() {
					candidates = append(candidates, candidate{build: extra, tagCost: set.cost, coverage: covered})
				}
			}
		}
	}
	return withoutDominated(candidates), nil
}

// tagChoice is a build's tags, and how many of the named tags it sets otherwise than the host.
type tagChoice struct {
	tags []string
	cost int
}

// tagChoices is every set of tags a build may take where files name named: the host's tags none of them names stay
// on, and the named ones are taken in every combination. A choice changing more of them comes before one changing
// fewer, so among covers alike in size and in tags changed the tags fold into the most preferred platform, and
// choices alike in that are by their tags.
func tagChoices(host []string, named []string) []tagChoice {
	kept := []string{}
	for _, tag := range host {
		if !slices.Contains(named, tag) {
			kept = append(kept, tag)
		}
	}
	choices := make([]tagChoice, 0, 1<<len(named))
	for mask := range 1 << len(named) {
		tags := slices.Clone(kept)
		cost := 0
		for index, tag := range named {
			taken := mask&(1<<index) != 0
			if taken {
				tags = append(tags, tag)
			}
			if taken != slices.Contains(host, tag) {
				cost++
			}
		}
		choices = append(choices, tagChoice{tags: sortedSet(tags), cost: cost})
	}
	slices.SortFunc(choices, func(left tagChoice, right tagChoice) int {
		if left.cost != right.cost {
			return right.cost - left.cost
		}
		return slices.Compare(left.tags, right.tags)
	})
	return choices
}

// compareOperatingSystems orders two `GOOS` values as an extra build prefers them: the host's own, then
// preferredOperatingSystems, then the rest by name.
func compareOperatingSystems(left string, right string, host string) int {
	rank := func(operatingSystem string) int {
		if operatingSystem == host {
			return -1
		}
		if index := slices.Index(preferredOperatingSystems, operatingSystem); index >= 0 {
			return index
		}
		return len(preferredOperatingSystems)
	}
	if rank(left) != rank(right) {
		return rank(left) - rank(right)
	}
	return strings.Compare(left, right)
}

// withoutDominated is candidates less each that an earlier one, changing no more tags, compiles everything of: any
// cover holding the later one holds as well with the earlier in its place.
func withoutDominated(candidates []candidate) []candidate {
	var kept []candidate
	for index, each := range candidates {
		dominated := slices.ContainsFunc(candidates[:index], func(earlier candidate) bool {
			return earlier.tagCost <= each.tagCost && earlier.coverage.holds(each.coverage)
		})
		if !dominated {
			kept = append(kept, each)
		}
	}
	return kept
}

// fewestCovering is the indexes of the fewest candidates whose coverage together is want, in order: of the fewest,
// those changing the fewest tags, then the earliest in candidates' order. Past searchBudget it is the greedy
// answer instead, each time the candidate compiling the most still wanted, the earliest of those.
func fewestCovering(candidates []candidate, want coverage) []int {
	if want.empty() {
		return nil
	}
	weighed := 0
	for size := 1; size <= len(candidates); size++ {
		var best []int
		bestCost := -1
		var choose func(start int, chosen []int, union coverage, cost int)
		choose = func(start int, chosen []int, union coverage, cost int) {
			weighed++
			if len(chosen) == size {
				if union.equal(want) && (bestCost < 0 || cost < bestCost) {
					best, bestCost = slices.Clone(chosen), cost
				}
				return
			}
			for index := start; index <= len(candidates)-(size-len(chosen)); index++ {
				if weighed > searchBudget {
					return
				}
				nextCost := cost + candidates[index].tagCost
				if bestCost >= 0 && nextCost >= bestCost {
					continue
				}
				choose(index+1, append(chosen, index), union.with(candidates[index].coverage), nextCost)
			}
		}
		choose(0, nil, newCoverage(want.size), 0)
		if weighed > searchBudget {
			return greedyCovering(candidates, want)
		}
		if best != nil {
			return best
		}
	}
	// Invariant: want is the union of every candidate's coverage, so all of them together cover it.
	panic("project: no set of candidates covers what they cover together")
}

// greedyCovering takes, until want is covered, the candidate covering the most still wanted, the earliest of
// those, returned in candidates' order.
func greedyCovering(candidates []candidate, want coverage) []int {
	var chosen []int
	covered := newCoverage(want.size)
	for !covered.equal(want) {
		best, bestGain := -1, 0
		for index, each := range candidates {
			if gain := covered.with(each.coverage).count() - covered.count(); gain > bestGain {
				best, bestGain = index, gain
			}
		}
		chosen = append(chosen, best)
		covered = covered.with(candidates[best].coverage)
	}
	slices.Sort(chosen)
	return chosen
}

// unbuiltReason is why no extra build compiles a file: only another architecture does, which is the layout blind
// spot; only cgo on another `GOOS` does; or no target of ports does, with whatever its constraint names that says
// why.
func unbuiltReason(read source, host Build, ports []Port) (string, error) {
	fileReading := readingOf(read, unixOperatingSystems(ports))
	// The targets are tried on the host's architecture first, so a file cgo on another `GOOS` builds is named for
	// that, then in the order an extra build prefers them, so the reason never turns on the order go tool dist list
	// prints, and with the tags a candidate takes.
	sorted := slices.Clone(ports)
	slices.SortFunc(sorted, func(left Port, right Port) int {
		leftElsewhere, rightElsewhere := left.Architecture != host.Architecture, right.Architecture != host.Architecture
		if leftElsewhere != rightElsewhere {
			if leftElsewhere {
				return 1
			}
			return -1
		}
		if order := compareOperatingSystems(left.OperatingSystem, right.OperatingSystem,
			host.OperatingSystem); order != 0 {
			return order
		}
		return strings.Compare(left.Architecture, right.Architecture)
	})
	for _, port := range sorted {
		for _, set := range tagChoices(host.Tags, fileReading.tags) {
			for _, cgoEnabled := range []bool{false, true} {
				if cgoEnabled && !port.CgoSupported {
					continue
				}
				tried := Build{OperatingSystem: port.OperatingSystem, Architecture: port.Architecture,
					CgoEnabled: cgoEnabled, Tags: set.tags}
				compiled, compileError := compiles(tried, read)
				if compileError != nil {
					return "", compileError
				}
				if !compiled {
					continue
				}
				if port.Architecture != host.Architecture {
					return "only another architecture than the host's " + host.Architecture + " builds it, as " +
						tried.String() + ", and a build on another `GOARCH` is a layout blind spot: its sizes, read " +
						"without a name (`unsafe.Sizeof` of our type embedding a std struct; SA1027, SA9006), go " +
						"unchecked", nil
				}
				return "only " + tried.String() + " builds it, and cgo is on only for the host's own platform, " +
					"whose C toolchain the load has", nil
			}
		}
	}
	if fileReading.ignore {
		return "its constraint names `ignore`, which no build sets", nil
	}
	targeted := map[string]bool{}
	for _, port := range ports {
		targeted[port.OperatingSystem] = true
	}
	for _, operatingSystem := range fileReading.operatingSystems {
		if !targeted[operatingSystem] {
			return "it names " + operatingSystem + ", which go tool dist list does not target", nil
		}
	}
	return "no target go tool dist list names builds it, with or without the tags it names", nil
}
