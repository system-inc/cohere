package go_engine

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

// bodyReachListed is how many disagreements of each direction the census file lists by name per build: every one
// stage 2 leaves unreached that the graph calls dependent is listed, being a soundness lead, and this many of the
// other direction, where stage 2 is wider, which is expected.
const bodyReachListed = 20

// writeBodyReachCensus writes each extra build's census of runs to the census file, when the run asked for one and
// judged its builds' dependence, after a line of what the reach does not stand for and one of the host's sides for the
// facts' reach (factReachSides), and notes each body differential failure on stderr, in build order, under the
// differential. An extra build with no census, judged by nothing or stopped by its type errors before its rules, is
// written as such, and under the differential is a failure: nothing of it was compared. A file that cannot be written
// is noted and changes nothing else.
func (engine *pipeline) writeBodyReachCensus(runs []buildRun) {
	if engine.bodyReachHost == nil {
		return
	}
	var written strings.Builder
	fmt.Fprintf(&written, "The reach stands for no finding that turns on a callee's fact, stage 4's (#h0k644s), nor "+
		"for a package-wide rule's, stage 5's (#xeafx79): the body differential classes a package-wide rule's "+
		"findings apart where the package holds a dependent unit, and fails a callee's fact's until the build can "+
		"show one differs (#5vbs1np).\n")
	// The host's sides for the facts' reach, under the census, ahead of every build that reads them.
	if engine.factReachSides != nil {
		fmt.Fprintf(&written, "fact reach: packages given a side %d; none, uncaptured %d, unrecorded %d, order "+
			"unknown %d\n", len(engine.factReachSides.packages), engine.factReachSides.uncaptured,
			engine.factReachSides.unrecorded, engine.factReachSides.orderUnknown)
	}
	for index, run := range runs {
		if run.bodyReach == nil {
			if index == 0 {
				continue
			}
			fmt.Fprintf(&written, "build %s\n  not judged: the build has no census\n", run.build)
			if engine.bodyDifferential {
				failure := "the build was not judged, so none of its bodies was compared"
				fmt.Fprintf(&written, "  body differential: compared 0, failures 1\n    %s\n", failure)
				engine.note("the body differential of the build %s: %s", run.build, failure)
			}
			continue
		}
		writeBuildBodyReach(&written, "build "+run.build.String(), run.bodyReach)
		if run.factReach != nil {
			writeBuildFactReach(&written, run.factReach)
		}
		if run.bodyReachFromSource != nil {
			writeBuildBodyReach(&written, "build "+run.build.String()+", within the packages a plain run checks from "+
				"source", run.bodyReachFromSource)
		}
		if run.bodyDifferential == nil {
			continue
		}
		fmt.Fprintf(&written, "  body differential: compared %d, failures %d\n", run.bodyDifferential.compared,
			len(run.bodyDifferential.failures))
		for _, failure := range run.bodyDifferential.failures {
			fmt.Fprintf(&written, "    %s\n", failure)
			engine.note("the body differential of the build %s: %s", run.build, failure)
		}
		for _, owed := range findingStages {
			classed := run.bodyDifferential.classed[owed.stage]
			fmt.Fprintf(&written, "  findings differing %s: %d\n", owed.written, len(classed))
			for _, line := range classed {
				fmt.Fprintf(&written, "    %s\n", line)
			}
		}
	}
	if engine.bodyReachCensus == "" {
		return
	}
	if writeError := os.WriteFile(engine.bodyReachCensus, []byte(written.String()), 0o644); writeError != nil {
		engine.note("the body reach census: %v", writeError)
	}
}

// writeBuildBodyReach writes one build's census to written under heading.
func writeBuildBodyReach(written *strings.Builder, heading string, census *buildBodyReach) {
	fmt.Fprintf(written, "%s\n", heading)
	fmt.Fprintf(written, "  packages checked from source: %d, recorded %d, unrecorded %d, only the build's %d\n",
		census.checkedFromSource, census.recorded,
		census.checkedFromSource-census.recorded-census.buildOnlyPackages, census.buildOnlyPackages)
	fmt.Fprintf(written, "  bodies: %d, reached %d, unreached %d, absent %d, new %d\n", census.total, census.reached,
		census.total-census.reached, census.absent, census.new)
	reasons := make([]string, 0, len(bodyReachReasons))
	for _, reason := range bodyReachReasons {
		reasons = append(reasons, fmt.Sprintf("%s %d", reason, census.byReason[reason]))
	}
	fmt.Fprintf(written, "  reached by reason: %s\n", strings.Join(reasons, ", "))
	writeLoadModeCost(written, census)
	writeBodyReachDrivers(written, census)
	fmt.Fprintf(written, "  declarations only: %d packages\n", len(census.declarationsOnly))
	for _, identifier := range census.declarationsOnly {
		fmt.Fprintf(written, "    %s\n", identifier)
	}
	fmt.Fprintf(written, "  bodies judged not the build's count, each package reached whole: %d packages\n",
		len(census.miscounted))
	for _, identifier := range census.miscounted {
		fmt.Fprintf(written, "    %s\n", identifier)
	}
	var unreachedDependent []reachedBody
	var reachedIndependent []reachedBody
	for _, body := range census.bodies {
		switch {
		case body.reason == "" && body.unitDependent:
			unreachedDependent = append(unreachedDependent, body)
		case body.reason != "" && !body.unitDependent:
			reachedIndependent = append(reachedIndependent, body)
		}
	}
	fmt.Fprintf(written, "  unreached, and the graph's unit verdict calls dependent: %d\n", len(unreachedDependent))
	for _, body := range unreachedDependent {
		fmt.Fprintf(written, "    %s\n", body)
	}
	fmt.Fprintf(written, "  reached, and the graph's unit verdict calls independent: %d\n", len(reachedIndependent))
	for _, body := range reachedIndependent[:min(len(reachedIndependent), bodyReachListed)] {
		fmt.Fprintf(written, "    %s (%s)\n", body, body.reason)
	}
}

// bodyReachDrivers is how many of what reaches the most bodies first the census lists per build.
const bodyReachDrivers = 10

// writeBodyReachDrivers writes to written the drivers that reach the most of census's bodies first
// (reachedBody.driver), each with how many, the most first, ties by driver.
func writeBodyReachDrivers(written *strings.Builder, census *buildBodyReach) {
	counts := map[string]int{}
	for _, body := range census.bodies {
		if body.driver != "" {
			counts[body.driver]++
		}
	}
	drivers := slices.SortedFunc(maps.Keys(counts), func(first string, second string) int {
		return cmp.Or(cmp.Compare(counts[second], counts[first]), cmp.Compare(first, second))
	})
	fmt.Fprintf(written, "  reached first by, the top %d of %d:\n", min(len(drivers), bodyReachDrivers), len(drivers))
	for _, driver := range drivers[:min(len(drivers), bodyReachDrivers)] {
		fmt.Fprintf(written, "    %d %s\n", counts[driver], driver)
	}
}

// writeLoadModeCost writes to written what the general load mode rule costs beside the constants-only rule it
// replaced (bodyReacher.loadModeReaches): the bodies each reaches first by the load mode, and the bodies the general
// rule reaches that the constants-only one leaves unreached.
func writeLoadModeCost(written *strings.Builder, census *buildBodyReach) {
	general, constantsOnly, beyond := 0, 0, 0
	for _, body := range census.bodies {
		if body.reason == reachedLoadMode {
			general++
		}
		if body.constantsOnlyReason == reachedLoadMode {
			constantsOnly++
		}
		if body.reason != "" && body.constantsOnlyReason == "" {
			beyond++
		}
	}
	fmt.Fprintf(written, "  load mode: general %d, constants only %d; reached by the general rule alone %d\n", general,
		constantsOnly, beyond)
}
