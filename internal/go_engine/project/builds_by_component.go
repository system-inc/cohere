package project

import (
	"slices"
	"strings"
)

// tagComponents are the custom tags the uncompiled files name, grouped so that tags named together in some file are
// in one component, joined across files. A file's constraint reads only the custom tags it names, so setting a tag
// never changes whether a file naming none of its component's tags compiles.
type tagComponents struct {
	// tags are each component's tags, sorted, the components by their first tag.
	tags [][]string

	// fileTags are each file's custom tags, sorted, and fileComponent its component, -1 for a file naming none.
	fileTags      [][]string
	fileComponent []int

	// files are each component's files, by index.
	files [][]int
}

// newTagComponents reads each file's custom tags and groups them into components.
func newTagComponents(uncompiled []source, unix []string) tagComponents {
	components := tagComponents{
		fileTags:      make([][]string, len(uncompiled)),
		fileComponent: make([]int, len(uncompiled)),
	}
	parent := map[string]string{}
	var root func(tag string) string
	root = func(tag string) string {
		if parent[tag] == tag {
			return tag
		}
		parent[tag] = root(parent[tag])
		return parent[tag]
	}
	for index, read := range uncompiled {
		tags := readingOf(read, unix).tags
		components.fileTags[index] = tags
		for _, tag := range tags {
			if _, seen := parent[tag]; !seen {
				parent[tag] = tag
			}
		}
		for _, tag := range tags {
			// The lesser root stays a root, so a component's root is its first tag in any order of files.
			left, right := root(tags[0]), root(tag)
			parent[max(left, right)] = min(left, right)
		}
	}
	grouped := map[string][]string{}
	for tag := range parent {
		grouped[root(tag)] = append(grouped[root(tag)], tag)
	}
	var roots []string
	for each := range grouped {
		roots = append(roots, each)
	}
	componentOf := map[string]int{}
	for index, each := range sortedSet(roots) {
		components.tags = append(components.tags, sortedSet(grouped[each]))
		componentOf[each] = index
	}
	components.files = make([][]int, len(components.tags))
	for index, tags := range components.fileTags {
		components.fileComponent[index] = -1
		if len(tags) > 0 {
			component := componentOf[root(tags[0])]
			components.fileComponent[index] = component
			components.files[component] = append(components.files[component], index)
		}
	}
	return components
}

// componentChoice is a platform's best build in a round of buildsByComponent: how many files still wanted it
// compiles, and how many of the named tags it sets otherwise than the host.
type componentChoice struct {
	build Build
	gain  int
	cost  int
}

// buildsByComponent chooses the extra builds when the named tags are too many for candidatesFor to weigh every set
// of them (enumerationBudget), from what the files' constraints distinguish rather than from every combination. It
// returns the builds, in the order candidatesFor would list them, and the files they compile.
//
// A build's tags are an assignment of each tag component (tagComponents), and the builds are chosen one at a time.
// Each round, every platform takes, component by component, the assignment compiling the most files still wanted,
// then the one changing the fewest tags from the host's, then the first by its tags, and the platform whose build
// compiles the most wins, the earliest on a tie. The assignments a component weighs are the host's and, for each of
// its files still wanted, each setting of that file's own tags that compiles it on the platform, so a round costs
// each file's own settings and a compile of each wanted file under each distinct assignment, never a walk of the
// combinations. Rounds end when no platform compiles one more file, and every file some platform compiles under
// some setting of its own tags is compiled by then: that setting alone would have compiled it.
func buildsByComponent(uncompiled []source, host Build, platforms []Build, unix []string) ([]Build, coverage, error) {
	components := newTagComponents(uncompiled, unix)
	wanted := newCoverage(len(uncompiled))
	for index := range uncompiled {
		wanted.set(index)
	}
	covered := newCoverage(len(uncompiled))
	var chosen []componentChoice
	for {
		var best componentChoice
		for _, platform := range platforms {
			choice, choiceError := bestOnPlatform(uncompiled, host, platform, components, wanted)
			if choiceError != nil {
				return nil, coverage{}, choiceError
			}
			if choice.gain > best.gain {
				best = choice
			}
		}
		if best.gain == 0 {
			break
		}
		for index, read := range uncompiled {
			if !wanted.has(index) {
				continue
			}
			compiled, compileError := compiles(best.build, read)
			if compileError != nil {
				return nil, coverage{}, compileError
			}
			if compiled {
				covered.set(index)
			}
		}
		// Invariant: the build compiles every file its gain counted, each read under its own component's assignment.
		wanted = newCoverage(len(uncompiled))
		for index := range uncompiled {
			if !covered.has(index) {
				wanted.set(index)
			}
		}
		chosen = append(chosen, best)
	}

	// The order candidatesFor lists builds in: by platform, then the more tags changed first, then by tags.
	platformOf := func(target Build) int {
		return slices.IndexFunc(platforms, func(each Build) bool {
			return each.OperatingSystem == target.OperatingSystem && each.CgoEnabled == target.CgoEnabled
		})
	}
	slices.SortStableFunc(chosen, func(left componentChoice, right componentChoice) int {
		if order := platformOf(left.build) - platformOf(right.build); order != 0 {
			return order
		}
		if left.cost != right.cost {
			return right.cost - left.cost
		}
		return slices.Compare(left.build.Tags, right.build.Tags)
	})
	builds := make([]Build, 0, len(chosen))
	for _, each := range chosen {
		builds = append(builds, each.build)
	}
	return builds, covered, nil
}

// bestOnPlatform is the platform's best build this round (see buildsByComponent), with no gain when it compiles
// nothing still wanted.
func bestOnPlatform(
	uncompiled []source,
	host Build,
	platform Build,
	components tagComponents,
	wanted coverage,
) (componentChoice, error) {
	var named []string
	for _, tags := range components.tags {
		named = append(named, tags...)
	}
	kept := []string{}
	for _, tag := range host.Tags {
		if !slices.Contains(named, tag) {
			kept = append(kept, tag)
		}
	}
	hostAssignments := make([][]string, len(components.tags))
	for component, tags := range components.tags {
		for _, tag := range tags {
			if slices.Contains(host.Tags, tag) {
				hostAssignments[component] = append(hostAssignments[component], tag)
			}
		}
	}
	// under is the platform with assignment for one component and the host's for every other, which reads alike
	// for every file of that component.
	under := func(component int, assignment []string) Build {
		target := platform
		tags := slices.Clone(kept)
		for index := range components.tags {
			if index == component {
				tags = append(tags, assignment...)
			} else {
				tags = append(tags, hostAssignments[index]...)
			}
		}
		target.Tags = sortedSet(tags)
		return target
	}
	countCompiled := func(target Build, files []int) (int, error) {
		count := 0
		for _, file := range files {
			compiled, compileError := compiles(target, uncompiled[file])
			if compileError != nil {
				return 0, compileError
			}
			if compiled {
				count++
			}
		}
		return count, nil
	}

	var untagged []int
	for index := range uncompiled {
		if wanted.has(index) && components.fileComponent[index] < 0 {
			untagged = append(untagged, index)
		}
	}
	gain, countError := countCompiled(under(-1, nil), untagged)
	if countError != nil {
		return componentChoice{}, countError
	}
	choice := componentChoice{gain: gain}
	tags := slices.Clone(kept)
	for component, componentTags := range components.tags {
		var wantedFiles []int
		assignments := [][]string{hostAssignments[component]}
		seen := map[string]bool{strings.Join(hostAssignments[component], ","): true}
		for _, file := range components.files[component] {
			if !wanted.has(file) {
				continue
			}
			wantedFiles = append(wantedFiles, file)
			own := components.fileTags[file]
			for mask := range 1 << len(own) {
				assignment := withOwnTags(hostAssignments[component], own, mask)
				key := strings.Join(assignment, ",")
				if seen[key] {
					continue
				}
				compiled, compileError := compiles(under(component, assignment), uncompiled[file])
				if compileError != nil {
					return componentChoice{}, compileError
				}
				if compiled {
					seen[key] = true
					assignments = append(assignments, assignment)
				}
			}
		}
		bestAssignment, bestGain, bestCost := hostAssignments[component], -1, 0
		for _, assignment := range assignments {
			assignmentGain, gainError := countCompiled(under(component, assignment), wantedFiles)
			if gainError != nil {
				return componentChoice{}, gainError
			}
			cost := len(componentTags) - len(agreedTags(componentTags, assignment, hostAssignments[component]))
			better := assignmentGain > bestGain || assignmentGain == bestGain && (cost < bestCost ||
				cost == bestCost && slices.Compare(assignment, bestAssignment) < 0)
			if better {
				bestAssignment, bestGain, bestCost = assignment, assignmentGain, cost
			}
		}
		tags = append(tags, bestAssignment...)
		choice.gain += bestGain
		choice.cost += bestCost
	}
	choice.build = platform
	choice.build.Tags = sortedSet(tags)
	choice.build.Flags = slices.Clone(platform.Flags)
	if choice.gain == 0 || choice.build.sameAs(host) {
		// The host's own build compiles none of the files it leaves uncompiled.
		return componentChoice{}, nil
	}
	return choice, nil
}

// withOwnTags is assignment, a component's tags set on, with each of own set on or off by its bit in mask, sorted.
func withOwnTags(assignment []string, own []string, mask int) []string {
	result := []string{}
	for _, tag := range assignment {
		if !slices.Contains(own, tag) {
			result = append(result, tag)
		}
	}
	for index, tag := range own {
		if mask&(1<<index) != 0 {
			result = append(result, tag)
		}
	}
	return sortedSet(result)
}

// agreedTags is the tags of all that two assignments agree on, on or off alike.
func agreedTags(all []string, left []string, right []string) []string {
	var agreed []string
	for _, tag := range all {
		if slices.Contains(left, tag) == slices.Contains(right, tag) {
			agreed = append(agreed, tag)
		}
	}
	return agreed
}
