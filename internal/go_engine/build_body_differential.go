package go_engine

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// The body differential, stage 2's own soundness machine (#w72zxtq), under `COHERE_BODY_DIFFERENTIAL=1` with
// `--build-dependence`: the object differential covers declarations, not bodies, so every body the reach
// would leave unreached in a build is checked against the host's, two ways. Its types: the build checks every package
// from source whole today, so its type information holds the body, rendered as the host's walk rendered the host's
// (build_reads.BodyRenderings), its signature's and its block's every expression's type, each named type in it by its
// name and the digest of its whole lines as the object differential renders a type name (check.NamedTypesWritten),
// and every identifier's use, and the two must be the same: a use of the declaration's own object by its offset, and
// one whose identity is unknown never alike, another unknown's included. Its findings: the build's raw findings within
// the body's lines must be the host's. A body that differs is a failure, named by its build, package, file,
// declaration and first difference, written to stderr and counted in the census. So is a build with recorded packages
// that compares no body, unless its sizes reached every body, and a build the census has no judgment of
// (writeBodyReachCensus), since a machine that compares nothing passes nothing. A body whose findings differ only by
// rules a later stage owns, those that read a callee's facts (stage 4's, #h0k644s) or judge the package whole (stage
// 5's, #xeafx79), all of one stage, is classed apart under that stage, counted and listed, and no failure: the reach
// does not stand for them.

// buildBodyDifferential is one build's body differential: how many unreached bodies it compared, each one's failure
// line where it differed, each findings difference a later stage owns by that stage (findingStage), and the bodies
// compared by their types, whose findings are compared once the build's rules have run.
type buildBodyDifferential struct {
	compared int
	failures []string
	classed  map[findingStage][]string
	bodies   []comparedBody
}

// findingStage is the later stage of #9paaen9 a rule's findings are owed to, where the reach does not stand for them.
type findingStage string

const (
	// stageCalleeFacts is a rule that reads facts about the functions a body calls, which move with the callees' own
	// bodies: stage 4's (#h0k644s).
	stageCalleeFacts findingStage = "CalleeFacts"
	// stagePackageWide is a rule that judges its package whole (rules.InputsComposes): stage 5's (#xeafx79).
	stagePackageWide findingStage = "PackageWide"
)

// findingStages are the stages, in the order the census writes them, each with the words it is written with.
var findingStages = []struct {
	stage   findingStage
	written string
}{
	{stageCalleeFacts, "turning on a callee's fact, stage 4's (#h0k644s)"},
	{stagePackageWide, "of a package-wide rule, stage 5's (#xeafx79)"},
}

// findingStagesOf are the stages enabled's rules' findings are owed to, by rule name, those of neither left out: a
// rule that reads facts of its own (readsFacts) is stage 4's, and one the inputs table declares Composes, stage 5's.
// Facts are asked first, so printf, which is both, is stage 4's.
func findingStagesOf(enabled []rule.Rule) map[string]findingStage {
	// Ignored: a table that does not parse is TestTheInputsTableParses's to fail, and leaves every rule unclassed,
	// so its differences stay failures.
	declared, _ := rules.DeclaredInputs()
	stages := map[string]findingStage{}
	for _, each := range enabled {
		switch {
		case readsFacts(each.Analyzer):
			stages[each.Name] = stageCalleeFacts
		case declared[each.Name].Inputs == rules.InputsComposes:
			stages[each.Name] = stagePackageWide
		}
	}
	return stages
}

// readsFacts reports whether analyzer, or what it requires, learns facts, as staticcheck's purity does through
// ctrlflow's, but for what a dispatcher requires for its walk and every listener on it: a dispatched rule's analyzer
// requires what its own listener does beside the dispatcher (dispatch.ListenerRequirement), and the dispatcher's
// requirements are every rule's on it, which would class nearly every rule.
func readsFacts(analyzer *analysis.Analyzer) bool {
	seen := map[*analysis.Analyzer]bool{}
	var reads func(current *analysis.Analyzer) bool
	reads = func(current *analysis.Analyzer) bool {
		if seen[current] {
			return false
		}
		seen[current] = true
		if len(current.FactTypes) > 0 {
			return true
		}
		for _, required := range current.Requires {
			if !dispatch.IsDispatcher(required) && reads(required) {
				return true
			}
		}
		return false
	}
	return reads(analyzer)
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
	// The build's named types are digested in a memo of its own, as the host's are in the host's.
	namedTypes := check.NewReadsBuild()
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
				}, namedTypes.NamedTypesWritten)
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
	if differential.compared == 0 && census.recorded > 0 && census.byReason[reachedSizes] < census.total {
		differential.failures = append(differential.failures, fmt.Sprintf("compared no body, though %d packages "+
			"are recorded and their sizes reach none whole", census.recorded))
	}
	return differential
}

// compareBodyPlaces orders two places by file, then offset.
func compareBodyPlaces(first build_reads.BodyPlace, second build_reads.BodyPlace) int {
	return cmp.Or(cmp.Compare(first.File, second.File), cmp.Compare(first.Offset, second.Offset))
}

// firstFactDifference is the first place the host's facts and the build's differ, each sorted, as `offset N: host
// <rendering>, build <rendering>`, `(none)` for a side that has no fact there, and empty when they are the same. A use
// of an unknown identity on either side differs, written alike or not, since it names no object to match.
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
		unknown := false
		if index < len(hostFacts) {
			hostWritten, offset = written(hostRenderings, hostFacts[index]), hostFacts[index].Start
			unknown = hostFacts[index].Unknown
		}
		if index < len(builtFacts) {
			builtWritten = written(builtRenderings, builtFacts[index])
			unknown = unknown || builtFacts[index].Unknown
			if index >= len(hostFacts) {
				offset = builtFacts[index].Start
			}
		}
		if unknown {
			return fmt.Sprintf("offset %d: host %s, build %s, an unknown identity matching none", offset,
				hostWritten, builtWritten)
		}
		if hostWritten != builtWritten {
			return fmt.Sprintf("offset %d: host %s, build %s", offset, hostWritten, builtWritten)
		}
	}
	return ""
}

// compareFindings compares, for each body compared by its types, the build's findings within its lines with the
// host's, hostFindings and builtFindings each by file, adding a failure for each body where they differ, but where
// every finding one side has and the other lacks is of rules stages owes to one later stage, which classes it there.
func (differential *buildBodyDifferential) compareFindings(
	hostFindings map[string][]check.Finding,
	builtFindings map[string][]check.Finding,
	stages map[string]findingStage,
) {
	for _, compared := range differential.bodies {
		hostWritten := findingsWithin(hostFindings[compared.body.file], compared.firstLine, compared.lastLine)
		builtWritten := findingsWithin(builtFindings[compared.body.file], compared.firstLine, compared.lastLine)
		if slices.Equal(hostWritten, builtWritten) {
			continue
		}
		line := fmt.Sprintf("%s: findings differ: host [%s], build [%s]", compared.body,
			strings.Join(hostWritten, "; "), strings.Join(builtWritten, "; "))
		if stage := differingStage(hostWritten, builtWritten, stages); stage != "" {
			if differential.classed == nil {
				differential.classed = map[findingStage][]string{}
			}
			differential.classed[stage] = append(differential.classed[stage], line)
			continue
		}
		differential.failures = append(differential.failures, line)
	}
}

// differingStage is the one stage every finding hostWritten or builtWritten holds and the other lacks is owed to, by
// its rule, the written finding's first word (findingsWithin), empty when one is owed to none or they are owed to two.
func differingStage(hostWritten []string, builtWritten []string, stages map[string]findingStage) findingStage {
	var stage findingStage
	for _, sides := range [][2][]string{{hostWritten, builtWritten}, {builtWritten, hostWritten}} {
		for _, written := range sides[0] {
			if slices.Contains(sides[1], written) {
				continue
			}
			ruleName, _, _ := strings.Cut(written, " ")
			owed := stages[ruleName]
			if owed == "" || stage != "" && owed != stage {
				return ""
			}
			stage = owed
		}
	}
	return stage
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
