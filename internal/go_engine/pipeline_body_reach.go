package go_engine

import (
	"fmt"
	"os"
	"strings"
)

// bodyReachListed is how many disagreements of each direction the census file lists by name per build: every one
// stage 2 leaves unreached that the graph calls dependent is listed, being a soundness lead, and this many of the
// other direction, where stage 2 is wider, which is expected.
const bodyReachListed = 20

// writeBodyReachCensus writes each extra build's census of runs to the census file, when the run asked for one and
// judged its builds' dependence. A file that cannot be written is noted and changes nothing else.
func (engine *pipeline) writeBodyReachCensus(runs []buildRun) {
	if engine.bodyReachCensus == "" || engine.bodyReachHost == nil {
		return
	}
	var written strings.Builder
	for _, run := range runs {
		if run.bodyReach == nil {
			continue
		}
		writeBuildBodyReach(&written, run.build.String(), run.bodyReach)
	}
	if writeError := os.WriteFile(engine.bodyReachCensus, []byte(written.String()), 0o644); writeError != nil {
		engine.note("the body reach census: %v", writeError)
	}
}

// writeBuildBodyReach writes one build's census to written.
func writeBuildBodyReach(written *strings.Builder, build string, census *buildBodyReach) {
	fmt.Fprintf(written, "build %s\n", build)
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
	fmt.Fprintf(written, "  declarations only: %d packages\n", len(census.declarationsOnly))
	for _, identifier := range census.declarationsOnly {
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
