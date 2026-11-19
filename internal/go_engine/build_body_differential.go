package go_engine

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// The body differential, stage 2's own soundness machine (#w72zxtq), under `COHERE_BODY_DIFFERENTIAL=1` with
// `--build-dependence --other-builds`: the object differential covers declarations, not bodies, so every body the reach
// would leave unreached in a build is checked against the host's, two ways. Its types: the build checks every package
// from source whole today, so its type information holds the body, rendered as the host's walk rendered the host's
// (build_reads.BodyRenderings), every expression's type and every identifier's use, and the two must be the same.
// Its findings: the build's raw findings within the body's lines must be the host's. A body that differs is a failure,
// named by its build, package, file, declaration and first difference, written to stderr and counted in the census.

// buildBodyDifferential is one build's body differential: how many unreached bodies it compared, each one's failure
// line where it differed, and the bodies compared by their types, whose findings are compared once the build's rules
// have run.
type buildBodyDifferential struct {
	compared int
	failures []string
	bodies   []comparedBody
}

// comparedBody is a body the differential compared, by where it lies, its declaration's first line to its body's
// last, and how the census names it.
type comparedBody struct {
	body      reachedBody
	firstLine int
	lastLine  int
}

// newBuildBodyDifferential compares the types of every body census leaves unreached in the build of loaded, its last
// load, rendered from the build's own type information while its rules have not let it go, with the host's rendering
// of the same body.
func newBuildBodyDifferential(
	host *bodyReachHost,
	loaded *load.Loaded,
	census *buildBodyReach,
) *buildBodyDifferential {
	differential := &buildBodyDifferential{}
	unreached := map[string]map[build_reads.BodyPlace]reachedBody{}
	for _, body := range census.bodies {
		if body.reason != "" {
			continue
		}
		if unreached[body.packageIdentifier] == nil {
			unreached[body.packageIdentifier] = map[build_reads.BodyPlace]reachedBody{}
		}
		unreached[body.packageIdentifier][build_reads.BodyPlace{File: body.file, Offset: body.offset}] = body
	}
	built := map[string]*packages.Package{}
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		if each.TypesInfo != nil {
			built[each.ID] = each
		}
	})
	for _, identifier := range slices.Sorted(maps.Keys(unreached)) {
		places := unreached[identifier]
		var hostRenderings *build_reads.BodyRenderings
		if reads := host.reads[host.packages[identifier]]; reads != nil {
			hostRenderings, _ = reads.BodyRenderings()
		}
		builtPackage := built[identifier]
		var builtRenderings *build_reads.BodyRenderings
		if builtPackage != nil {
			builtRenderings = build_reads.RenderBodies(builtPackage.Fset, builtPackage.Types,
				builtPackage.TypesInfo, builtPackage.Syntax, func(place build_reads.BodyPlace) bool {
					_, isUnreached := places[place]
					return isUnreached
				})
		}
		for _, place := range slices.SortedFunc(maps.Keys(places), compareBodyPlaces) {
			body := places[place]
			differential.compared++
			if hostRenderings == nil || builtRenderings == nil {
				differential.failures = append(differential.failures,
					fmt.Sprintf("%s: not rendered (host %t, build %t)", body, hostRenderings != nil,
						builtRenderings != nil))
				continue
			}
			hostBody, isHostRendered := hostRenderings.Body(place)
			builtBody, isBuiltRendered := builtRenderings.Body(place)
			if !isHostRendered || !isBuiltRendered {
				differential.failures = append(differential.failures,
					fmt.Sprintf("%s: body not rendered (host %t, build %t)", body, isHostRendered, isBuiltRendered))
				continue
			}
			if difference := firstFactDifference(hostRenderings, hostBody.Facts, builtRenderings,
				builtBody.Facts); difference != "" {
				differential.failures = append(differential.failures, body.String()+": "+difference)
			}
			differential.bodies = append(differential.bodies,
				comparedBody{body: body, firstLine: body.line, lastLine: builtBody.LastLine})
		}
	}
	return differential
}

// compareBodyPlaces orders two places by file, then offset.
func compareBodyPlaces(first build_reads.BodyPlace, second build_reads.BodyPlace) int {
	return cmp.Or(cmp.Compare(first.File, second.File), cmp.Compare(first.Offset, second.Offset))
}

// firstFactDifference is the first place the host's facts and the build's differ, each sorted, as `offset N: host
// <rendering>, build <rendering>`, `(none)` for a side that has no fact there, and empty when they are the same.
func firstFactDifference(
	hostRenderings *build_reads.BodyRenderings,
	hostFacts []build_reads.RenderedFact,
	builtRenderings *build_reads.BodyRenderings,
	builtFacts []build_reads.RenderedFact,
) string {
	written := func(renderings *build_reads.BodyRenderings, fact build_reads.RenderedFact) string {
		kind := "type"
		if fact.Use {
			kind = "use"
		}
		return fmt.Sprintf("%d-%d %s %s", fact.Start, fact.End, kind, renderings.Text(fact))
	}
	for index := range max(len(hostFacts), len(builtFacts)) {
		hostWritten, builtWritten := "(none)", "(none)"
		offset := int32(0)
		if index < len(hostFacts) {
			hostWritten, offset = written(hostRenderings, hostFacts[index]), hostFacts[index].Start
		}
		if index < len(builtFacts) {
			builtWritten = written(builtRenderings, builtFacts[index])
			if index >= len(hostFacts) {
				offset = builtFacts[index].Start
			}
		}
		if hostWritten != builtWritten {
			return fmt.Sprintf("offset %d: host %s, build %s", offset, hostWritten, builtWritten)
		}
	}
	return ""
}

// compareFindings compares, for each body compared by its types, the build's findings within its lines with the
// host's, hostFindings and builtFindings each by file, adding a failure for each body where they differ.
func (differential *buildBodyDifferential) compareFindings(
	hostFindings map[string][]check.Finding,
	builtFindings map[string][]check.Finding,
) {
	for _, compared := range differential.bodies {
		hostWritten := findingsWithin(hostFindings[compared.body.file], compared.firstLine, compared.lastLine)
		builtWritten := findingsWithin(builtFindings[compared.body.file], compared.firstLine, compared.lastLine)
		if slices.Equal(hostWritten, builtWritten) {
			continue
		}
		differential.failures = append(differential.failures, fmt.Sprintf("%s: findings differ: host [%s], build [%s]",
			compared.body, strings.Join(hostWritten, "; "), strings.Join(builtWritten, "; ")))
	}
}

// findingsWithin are findings that start between firstLine and lastLine, each written as `rule line:column message`,
// sorted and each once, as the merge dedupes them.
func findingsWithin(findings []check.Finding, firstLine int, lastLine int) []string {
	var written []string
	for _, finding := range findings {
		if finding.Line >= firstLine && finding.Line <= lastLine {
			written = append(written, fmt.Sprintf("%s %d:%d %s", finding.Rule, finding.Line, finding.Column,
				finding.Message))
		}
	}
	slices.Sort(written)
	return slices.Compact(written)
}

// findingsByFile are findings by the file each is in.
func findingsByFile(findings []check.Finding) map[string][]check.Finding {
	byFile := map[string][]check.Finding{}
	for _, finding := range findings {
		byFile[finding.File] = append(byFile[finding.File], finding)
	}
	return byFile
}
