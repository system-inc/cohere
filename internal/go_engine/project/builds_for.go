package project

import (
	"path/filepath"
	"slices"
	"strconv"
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
// host's cgo, or the other where a file names `cgo` or imports "C".
//
// A platform's build carries the host's tags alone, so a tag's type errors never cost a platform its linting
// (#h7m6zyp, adamic's darwin, whose 168 packages went unlinted under three folded tags). The files only a custom tag
// compiles get builds of their own, one tag component at a time (tagComponents: the tags one package's files name
// share a build, and no other component's ride with them), on the host's platform, or on a platform where a file of
// the component names that platform and the tag together. No build sets tags that compile a program of its own,
// which would put it beside its directory's package (breaks).
func BuildsFor(census Census, host Build, ports []Port, extraTargets []Port) ([]Build, []Unbuilt, error) {
	var candidatesUncompiled []source
	var unbuilt []Unbuilt
	// named are the `GOOS` values the project's files name as ones they are built for (targetPorts).
	var named []string
	// compiledPackages are the packages the host's build compiles in each directory, by name as packageOf reads it.
	compiledPackages := map[string]map[string]bool{}
	for _, file := range census.Ours {
		read, readError := readSource(file)
		if readError != nil {
			// The engine names an unreadable file as a gap of its own; no build can say it compiles one.
			unbuilt = append(unbuilt, Unbuilt{File: file, Reason: readError.Error()})
			continue
		}
		named = append(named, positiveOperatingSystems(read)...)
		compiled, compileError := compiles(host, read)
		if compileError != nil {
			return nil, nil, compileError
		}
		if !compiled {
			candidatesUncompiled = append(candidatesUncompiled, read)
			continue
		}
		if name := packageOf(read); name != "" {
			directory := filepath.Dir(read.file)
			if compiledPackages[directory] == nil {
				compiledPackages[directory] = map[string]bool{}
			}
			compiledPackages[directory][name] = true
		}
	}
	unix := unixOperatingSystems(ports)
	// A file whose package is not the one its directory compiles is a program of its own, run by itself, such as a
	// `package main` oracle beside a package's tests behind a tag of its own (adamic's lintoracle, #rkw4xcn): no build
	// of the directory's package compiles it, so setting its tag only breaks that package, and it is unbuilt, named.
	var uncompiled []source
	var programs []program
	for _, read := range candidatesUncompiled {
		name, directoryPackages := packageOf(read), compiledPackages[filepath.Dir(read.file)]
		// A file behind `ignore` is no build's at all, which its own reason says better (unbuiltReason).
		ignored := readingOf(read, nil).ignore
		if name != "" && len(directoryPackages) > 0 && !directoryPackages[name] && !ignored {
			besides := strings.Join(sortedKeys(directoryPackages), " and ")
			unbuilt = append(unbuilt, Unbuilt{File: read.file, Reason: "a program of its own, package " + name +
				" beside package " + besides + ", so no build of its directory's package compiles it"})
			programs = append(programs, program{read: read, name: name, besides: besides})
			continue
		}
		uncompiled = append(uncompiled, read)
	}
	builds := []Build{host}
	targets := targetPorts(host, ports, sortedSet(named), extraTargets)
	platforms := platformsFor(host, targets, dimensionsOf(uncompiled, ports).Cgo)
	// Each target the settings list is built, whether or not a file is its alone, so the code every platform shares
	// is type-checked for it too (#14vweg1, Kirk's ruling, Nov 10). The targets the files name stay as needed.
	listed := listedBuilds(host, extraTargets)
	if len(uncompiled) == 0 {
		builds = append(builds, sortedBuilds(listed, host, platforms)...)
		slices.SortFunc(unbuilt, func(left Unbuilt, right Unbuilt) int {
			return strings.Compare(left.File, right.File)
		})
		return builds, unbuilt, nil
	}

	// First the platforms, each under the host's tags: the listed ones, then the fewest others that compile what any
	// platform does and no listed one does.
	candidates, candidatesError := candidatesFor(uncompiled, host, platforms)
	if candidatesError != nil {
		return nil, nil, candidatesError
	}
	coverable := newCoverage(len(uncompiled))
	for _, each := range candidates {
		coverable.add(each.coverage)
	}
	chosen := slices.Clone(listed)
	// What the listed builds compile is read under them, never from the candidates, which leave out a listed platform
	// an earlier one compiles everything of (withoutDominated).
	listedCoverage := newCoverage(len(uncompiled))
	for _, each := range listed {
		for index, read := range uncompiled {
			compiled, compileError := compiles(each, read)
			if compileError != nil {
				return nil, nil, compileError
			}
			if compiled {
				listedCoverage.set(index)
			}
		}
	}
	var unlisted []candidate
	wanted := newCoverage(len(uncompiled))
	for _, each := range candidates {
		if slices.ContainsFunc(listed, each.build.sameAs) {
			continue
		}
		// Invariant: a file a listed build compiles is never wanted again, and every other is compiled by an unlisted
		// candidate, since a listed one compiles only what its listed build does.
		remaining := newCoverage(len(uncompiled))
		for index := range uncompiled {
			if each.coverage.has(index) && !listedCoverage.has(index) {
				remaining.set(index)
			}
		}
		if !remaining.empty() {
			unlisted = append(unlisted, candidate{build: each.build, coverage: remaining})
			wanted.add(remaining)
		}
	}
	for _, index := range fewestCovering(unlisted, wanted) {
		chosen = append(chosen, unlisted[index].build)
	}
	// Then the files only a tag compiles, each tag component's on the platforms its files name beside its tags.
	var tagged []int
	for index := range uncompiled {
		if !coverable.has(index) {
			tagged = append(tagged, index)
		}
	}
	taggedFiles := make([]source, 0, len(tagged))
	for _, index := range tagged {
		taggedFiles = append(taggedFiles, uncompiled[index])
	}
	components := newTagComponents(taggedFiles, unix)
	for component := range components.tags {
		componentFiles := make([]source, 0, len(components.files[component]))
		for _, file := range components.files[component] {
			componentFiles = append(componentFiles, taggedFiles[file])
		}
		componentBuilds, covered, componentError := buildsByComponent(componentFiles, host,
			platformsNamedBy(componentFiles, host, platforms), unix, programs)
		if componentError != nil {
			return nil, nil, componentError
		}
		chosen = append(chosen, componentBuilds...)
		for position, file := range components.files[component] {
			if covered.has(position) {
				coverable.set(tagged[file])
			}
		}
	}
	builds = append(builds, sortedBuilds(chosen, host, platforms)...)
	for index, read := range uncompiled {
		if !coverable.has(index) {
			reason, reasonError := unbuiltReason(read, host, ports, targets, programs, census.Root)
			if reasonError != nil {
				return nil, nil, reasonError
			}
			unbuilt = append(unbuilt, Unbuilt{File: read.file, Reason: reason})
		}
	}
	slices.SortFunc(unbuilt, func(left Unbuilt, right Unbuilt) int { return strings.Compare(left.File, right.File) })
	return builds, unbuilt, nil
}

// listedBuilds is a build under the host's tags on each target the settings list (extraTargets), but the host's own
// platform, which the host's build is: cgo off, as on any platform other than the host's, each once, in their order.
func listedBuilds(host Build, extraTargets []Port) []Build {
	var listed []Build
	for _, port := range extraTargets {
		if port.OperatingSystem == host.OperatingSystem && port.Architecture == host.Architecture {
			continue
		}
		build := Build{OperatingSystem: port.OperatingSystem, Architecture: port.Architecture,
			Tags: slices.Clone(host.Tags), Flags: slices.Clone(host.Flags)}
		if !slices.ContainsFunc(listed, build.sameAs) {
			listed = append(listed, build)
		}
	}
	return listed
}

// program is a file of a package other than the one its directory compiles (see BuildsFor): its package's name,
// and the packages its directory compiles, joined as its reason names them.
type program struct {
	read    source
	name    string
	besides string
}

// breaks is the programs of their own a build compiles that its platform under the host's tags does not, sorted by
// file: setting the build's tags is what would put each beside its directory's package. lintoracle compiles all 4 of
// adamic's oracles beside package assertions, so no build sets it (#h7m6zyp), while a tag that only turns a program
// off, or one a program needs only beside another, never compiles one alone and is set as any other.
func breaks(target Build, host Build, programs []program) ([]program, error) {
	hostTagged := target
	hostTagged.Tags = host.Tags
	var broken []program
	for _, each := range programs {
		compiled, compileError := compiles(target, each.read)
		if compileError != nil {
			return nil, compileError
		}
		if !compiled {
			continue
		}
		compiledUnderHost, hostError := compiles(hostTagged, each.read)
		if hostError != nil {
			return nil, hostError
		}
		if !compiledUnderHost {
			broken = append(broken, each)
		}
	}
	return broken, nil
}

// breakingReason is why no build sets the tags tried takes beyond the host's: the first program they compile, its
// directory relative to root, and how many other directories they would break.
func breakingReason(tried Build, host Build, broken []program, root string) string {
	var changed []string
	for _, tag := range tried.Tags {
		if !slices.Contains(host.Tags, tag) {
			changed = append(changed, tag)
		}
	}
	directory := filepath.Dir(broken[0].read.file)
	if relative, relativeError := filepath.Rel(root, directory); relativeError == nil && root != "" {
		directory = filepath.ToSlash(relative)
	}
	reason := "its tag " + changed[0] + " is never set: setting it puts package "
	if len(changed) > 1 {
		reason = "its tags " + strings.Join(changed[:len(changed)-1], ", ") + " and " + changed[len(changed)-1] +
			" are never set together: setting them puts package "
	}
	reason += broken[0].name + " beside package " + broken[0].besides + " in " + directory
	directories := map[string]bool{}
	for _, each := range broken {
		directories[filepath.Dir(each.read.file)] = true
	}
	if others := len(directories) - 1; others == 1 {
		reason += " and 1 other directory"
	} else if others > 1 {
		reason += " and " + strconv.Itoa(others) + " other directories"
	}
	return reason
}

// platformsNamedBy are the platforms a tag component's builds may take: the host's, and each a file of the component
// names as one it is built for, since that file names the platform beside a tag of the component.
func platformsNamedBy(files []source, host Build, platforms []Build) []Build {
	var named []string
	for _, read := range files {
		named = append(named, positiveOperatingSystems(read)...)
	}
	var allowed []Build
	for _, platform := range platforms {
		onHost := platform.OperatingSystem == host.OperatingSystem && platform.Architecture == host.Architecture
		if onHost || slices.Contains(named, platform.OperatingSystem) {
			allowed = append(allowed, platform)
		}
	}
	return allowed
}

// sortedBuilds is the extra builds in the order they are preferred: by platform as platformsFor orders them, a
// platform's own build, under the host's tags, before its tag components', and those by their tags.
func sortedBuilds(chosen []Build, host Build, platforms []Build) []Build {
	platformOf := func(target Build) int {
		return slices.IndexFunc(platforms, func(each Build) bool {
			return each.OperatingSystem == target.OperatingSystem && each.Architecture == target.Architecture &&
				each.CgoEnabled == target.CgoEnabled
		})
	}
	sorted := slices.Clone(chosen)
	slices.SortStableFunc(sorted, func(left Build, right Build) int {
		if order := platformOf(left) - platformOf(right); order != 0 {
			return order
		}
		leftTagged, rightTagged := !slices.Equal(left.Tags, host.Tags), !slices.Equal(right.Tags, host.Tags)
		if leftTagged != rightTagged {
			if leftTagged {
				return 1
			}
			return -1
		}
		return slices.Compare(left.Tags, right.Tags)
	})
	return sorted
}

// candidate is a platform's build under the host's tags that might be chosen, and which of the uncompiled files
// it compiles.
type candidate struct {
	build    Build
	coverage coverage
}

// platformsFor is each platform an extra build may take, a build with no tags of its own yet, one for each target
// (see targetPorts), in the order they are preferred: the host's architecture first, then by `GOOS` (see
// compareOperatingSystems), then by architecture, the host's cgo before the other. A platform other than the
// host's has cgo off; the host's takes the other cgo setting too when a file names `cgo` or imports "C", where the
// platform supports it.
func platformsFor(host Build, targets []Port, cgo bool) []Build {
	var ordered []Port
	for _, port := range targets {
		if !slices.ContainsFunc(ordered, func(seen Port) bool { return seen == port }) {
			ordered = append(ordered, port)
		}
	}
	slices.SortFunc(ordered, func(left Port, right Port) int {
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
	var platforms []Build
	for _, port := range ordered {
		cgoSettings := []bool{false}
		if port.OperatingSystem == host.OperatingSystem && port.Architecture == host.Architecture {
			cgoSettings = []bool{host.CgoEnabled}
			if cgo && (host.CgoEnabled || port.CgoSupported) {
				cgoSettings = append(cgoSettings, !host.CgoEnabled)
			}
		}
		for _, cgoEnabled := range cgoSettings {
			platforms = append(platforms, Build{OperatingSystem: port.OperatingSystem, Architecture: port.Architecture,
				CgoEnabled: cgoEnabled, Flags: slices.Clone(host.Flags)})
		}
	}
	return platforms
}

// targetPorts are the platforms the project is built for (#ntamp9e, Kirk's ruling, Nov 10): on the host's
// architecture, the host's own `GOOS` and every `GOOS` named positively by the project's files, in a constraint
// outside a negation or in a file name's suffix, and beside them each port the settings' targets name, whatever its
// architecture. A file whose constraint only rules platforms out, as `!linux && !darwin` does, is satisfied from
// these alone, so it is never checked as a platform nobody named, and a file no target compiles is a named gap.
func targetPorts(host Build, ports []Port, named []string, extra []Port) []Port {
	var targets []Port
	for _, port := range ports {
		onHost := port.Architecture == host.Architecture &&
			(port.OperatingSystem == host.OperatingSystem || slices.Contains(named, port.OperatingSystem))
		if onHost || slices.Contains(extra, port) {
			targets = append(targets, port)
		}
	}
	return targets
}

// candidatesFor is each platform's build under the host's tags that compiles at least one of the uncompiled files, in
// the order platforms are preferred, each that an earlier one compiles everything of left out (withoutDominated).
func candidatesFor(uncompiled []source, host Build, platforms []Build) ([]candidate, error) {
	var candidates []candidate
	for _, platform := range platforms {
		extra := platform
		extra.Tags = slices.Clone(host.Tags)
		extra.Flags = slices.Clone(platform.Flags)
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
			candidates = append(candidates, candidate{build: extra, coverage: covered})
		}
	}
	return withoutDominated(candidates), nil
}

// tagChoice is a build's tags, and how many of the named tags it sets otherwise than the host.
type tagChoice struct {
	tags []string
	cost int
}

// tagChoices is every set of tags a build may take where a file names named: the host's tags none of them names
// stay on, and the named ones are taken in every combination. A choice changing more of them comes before one
// changing fewer, and choices alike in that are by their tags, so the build unbuiltReason names never turns on the
// order of the file's tags.
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

// withoutDominated is candidates less each that an earlier one compiles everything of: any cover holding the later
// one holds as well with the earlier in its place.
func withoutDominated(candidates []candidate) []candidate {
	var kept []candidate
	for index, each := range candidates {
		dominated := slices.ContainsFunc(candidates[:index], func(earlier candidate) bool {
			return earlier.coverage.holds(each.coverage)
		})
		if !dominated {
			kept = append(kept, each)
		}
	}
	return kept
}

// fewestCovering is the indexes of the fewest candidates whose coverage together is want, in order, the earliest in
// candidates' order of the fewest. Past searchBudget it is the greedy answer instead, each time the candidate
// compiling the most still wanted, the earliest of those.
func fewestCovering(candidates []candidate, want coverage) []int {
	if want.empty() {
		return nil
	}
	weighed := 0
	for size := 1; size <= len(candidates); size++ {
		var best []int
		var choose func(start int, chosen []int, union coverage)
		choose = func(start int, chosen []int, union coverage) {
			weighed++
			if len(chosen) == size {
				if best == nil && union.equal(want) {
					best = slices.Clone(chosen)
				}
				return
			}
			for index := start; index <= len(candidates)-(size-len(chosen)); index++ {
				if weighed > searchBudget || best != nil {
					return
				}
				choose(index+1, append(chosen, index), union.with(candidates[index].coverage))
			}
		}
		choose(0, nil, newCoverage(want.size))
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

// unbuiltReason is why no extra build compiles a file: it needs tags no build sets (breaks); only another
// architecture does, which is the layout blind spot; only a tag on a platform no file names beside it does; only cgo
// on another `GOOS` does; or no target of ports does, with whatever its constraint names that says why.
func unbuiltReason(
	read source,
	host Build,
	ports []Port,
	targets []Port,
	programs []program,
	root string,
) (string, error) {
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
				broken, breaksError := breaks(tried, host, programs)
				if breaksError != nil {
					return "", breaksError
				}
				if len(broken) > 0 {
					return breakingReason(tried, host, broken, root), nil
				}
				if port.Architecture == host.Architecture && !slices.Contains(targets, port) {
					// A platform the project never named builds it, as a constraint that only rules platforms out
					// lets every other: it is no target, so the file is a gap, never checked as that platform.
					return "no target platform compiles it: " + constraintText(read), nil
				}
				elsewhere := port.OperatingSystem != host.OperatingSystem || port.Architecture != host.Architecture
				taggedElsewhere := elsewhere && !cgoEnabled && !slices.Equal(set.tags, host.Tags)
				if taggedElsewhere && slices.Contains(targets, port) {
					return "only " + tried.String() + " builds it, and a platform's build carries the host's tags " +
						"alone, taking a tag only where a file names the platform beside it", nil
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
		targeted[port.Architecture] = true
	}
	for _, named := range slices.Concat(fileReading.operatingSystems, fileReading.architectures) {
		if !targeted[named] {
			return "it names " + named + ", which go tool dist list does not target", nil
		}
	}
	return "no target go tool dist list names builds it, with or without the tags it names", nil
}
