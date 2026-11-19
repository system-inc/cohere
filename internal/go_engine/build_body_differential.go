package go_engine

import (
	"bytes"
	"cmp"
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
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
// 5's, #xeafx79), all owed to one stage, is classed apart under that stage, counted and listed, and no failure, but
// only where the build shows that stage's cause for the body (findingCauses): the reach does not stand for them, and
// the later stage redoes the body only where its cause holds. Where the facts' reach settled a package (#5vbs1np), a
// body it took out of the standing set is the build's to lint, so its findings are the build's own and no comparison
// with the host's: it is counted apart, taken out by the facts' reach, and its findings are not compared.
//
// Its second leg (#adq5ahd) checks what the facts' reach left standing (#aswfm7a), once the build's rules have run:
// the build lints whole today, so every answer its own run holds is real, and of each body the facts' fixed point left
// standing, every answer the build holds about the function declared there, or about one of its parameters, must be
// the host's, byte for byte, and so must the function's no-return fact and its other pushed facts.
//
// An answer about a standing function the build holds and the host's side holds none of is judged by who asked it in
// the build (#5vbs1np, must 5). A finding reads an answer only through a rule's question, whose asker, the body the
// rule was checking, the build's resolver records (fact_resolver.Resolver.RecordAskers). An answer a kind's walk kept,
// or one asked inside another answer's judgment, is read by no rule: it reaches a finding only through the top-level
// answer it fed, whose asker is recorded. So such an answer is a failure when a body that asked it stands, since that
// body's findings are the host's and rest on an answer the host never had; it is counted apart, asked only in the
// build by reached bodies, when every body that asked it is one the build lints; and it is counted apart again, held
// only in the build and asked by no rule, when no rule asked it, never a failure. A body no place names, as a wrapper
// the representation makes, is taken as standing, the side that fails.

// standingNoReturn is the kind the second leg counts a no-return fact's comparison under.
const standingNoReturn = "no-return"

// buildBodyDifferential is one build's body differential: how many unreached bodies it compared, each one's failure
// line where it differed, each findings difference a later stage owns by that stage (findingStage), and the bodies
// compared by their types, whose findings are compared once the build's rules have run.
type buildBodyDifferential struct {
	compared int
	failures []string
	classed  map[findingStage][]string
	bodies   []comparedBody

	// factsSettled is whether the findings were compared beside the facts' reach's settlement, and takenOut how many
	// bodies compared by their types it took out of the standing set, whose findings were then left alone.
	factsSettled bool
	takenOut     int

	// standingCompared and standingFailed are the second leg's comparisons and failures by kind, an answer's kind or
	// standingNoReturn; each failure's line is among failures too.
	standingCompared map[string]int
	standingFailed   map[string]int

	// askedByReached and heldUnasked count the answers about standing functions only the build holds: those every
	// asker of which the build lints, and those no rule asked.
	askedByReached int
	heldUnasked    int
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

// findingStagesOf are the stages enabled's rules' findings may be owed to, by rule name, each rule's in findingStages's
// order, those of neither left out: a rule that reads facts of its own (readsFacts) may be stage 4's, and one the
// inputs table declares Composes stage 5's. printf, which is both, may be either, whichever's cause the build shows
// for the body (findingCauses).
func findingStagesOf(enabled []rule.Rule) map[string][]findingStage {
	// Ignored: a table that does not parse is TestTheInputsTableParses's to fail, and leaves every rule unclassed,
	// so its differences stay failures.
	declared, _ := rules.DeclaredInputs()
	stages := map[string][]findingStage{}
	for _, each := range enabled {
		if readsFacts(each.Analyzer) {
			stages[each.Name] = append(stages[each.Name], stageCalleeFacts)
		}
		if declared[each.Name].Inputs == rules.InputsComposes {
			stages[each.Name] = append(stages[each.Name], stagePackageWide)
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
// every finding one side has and the other lacks is of rules stages owes to one later stage whose cause causes shows
// for the body, which classes it there. A difference its rules owe to a stage whose cause is not shown stays a
// failure, saying so. settled is the facts' reach's result by package ID (check.Result.FactReachSettled), nil where
// it never ran: a body it took out of a package it settled in part is counted, never compared (takenOutByFacts).
func (differential *buildBodyDifferential) compareFindings(
	hostFindings map[string][]check.Finding,
	builtFindings map[string][]check.Finding,
	stages map[string][]findingStage,
	causes *findingCauses,
	settled map[string]any,
) {
	differential.factsSettled = settled != nil
	takenOut := takenOutByFacts(settled)
	for _, compared := range differential.bodies {
		if takenOut[compared.body.packageIdentifier][fact_reach.Place{File: compared.body.file,
			Offset: compared.body.offset}] {
			differential.takenOut++
			continue
		}
		hostWritten := findingsWithin(hostFindings[compared.body.file], compared.firstLine, compared.lastLine)
		builtWritten := findingsWithin(builtFindings[compared.body.file], compared.firstLine, compared.lastLine)
		if slices.Equal(hostWritten, builtWritten) {
			continue
		}
		line := fmt.Sprintf("%s: findings differ: host [%s], build [%s]", compared.body,
			strings.Join(hostWritten, "; "), strings.Join(builtWritten, "; "))
		owed := differingStages(hostWritten, builtWritten, stages)
		if stage := classingStage(owed, compared.body, causes); stage != "" {
			if differential.classed == nil {
				differential.classed = map[findingStage][]string{}
			}
			differential.classed[stage] = append(differential.classed[stage], line)
			continue
		}
		if len(owed) > 0 {
			written := make([]string, 0, len(owed))
			for _, stage := range owed {
				written = append(written, string(stage))
			}
			line += ", owed by its rules to " + strings.Join(written, " or ") + ", whose cause the build does not show"
		}
		differential.failures = append(differential.failures, line)
	}
}

// takenOutByFacts are the bodies the facts' reach took out of the standing set, by package ID and place, of each
// package settled holds a result of in part: the build lints them, so their findings are the build's own. A body the
// reach never held standing, as a value's initializer, is not among them, nor is any of a package it did not settle.
func takenOutByFacts(settled map[string]any) map[string]map[fact_reach.Place]bool {
	takenOut := map[string]map[fact_reach.Place]bool{}
	for identifier, result := range settled {
		packageSettled, isResult := result.(*standing_facts.Result)
		if !isResult || !packageSettled.Partial || packageSettled.Settlement == nil {
			continue
		}
		takenOut[identifier] = map[fact_reach.Place]bool{}
		for _, removal := range packageSettled.Settlement.Removed {
			takenOut[identifier][removal.Place] = true
		}
	}
	return takenOut
}

// classingStage is the first of owed whose cause causes shows for body, empty when none is.
func classingStage(owed []findingStage, body reachedBody, causes *findingCauses) findingStage {
	for _, stage := range owed {
		if causes.shown(stage, body) {
			return stage
		}
	}
	return ""
}

// differingStages are the stages, in findingStages's order, that every finding hostWritten or builtWritten holds and
// the other lacks may be owed to, by its rule, the written finding's first word (findingsWithin): none when one of
// them is owed to no stage, so a body mixing a classed difference with an unclassed one is never classed, nor one
// whose differences no one stage owns all of.
func differingStages(
	hostWritten []string,
	builtWritten []string,
	stages map[string][]findingStage,
) []findingStage {
	var shared []findingStage
	isFirst := true
	for _, sides := range [][2][]string{{hostWritten, builtWritten}, {builtWritten, hostWritten}} {
		for _, written := range sides[0] {
			if slices.Contains(sides[1], written) {
				continue
			}
			ruleName, _, _ := strings.Cut(written, " ")
			owed := stages[ruleName]
			if isFirst {
				shared, isFirst = slices.Clone(owed), false
				continue
			}
			shared = slices.DeleteFunc(shared, func(stage findingStage) bool { return !slices.Contains(owed, stage) })
		}
	}
	return shared
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

// compareStanding is the second leg (see the file's comment) over the build named build: each package settled, by its
// ID, that the build settled in part, whose host's side sides holds, against what captured, the build's own run's
// capture of its facts, holds of its functions, found in built, the packages the build's last load checked from source,
// by ID, whose types outlive its rules. Each side's identities are rendered by a renderer of its own, on the build's
// goroutine.
func (differential *buildBodyDifferential) compareStanding(
	build string,
	sides *factReachSides,
	settled map[string]any,
	captured *check.FactReachCapture,
	built map[string]*packages.Package,
) {
	differential.standingCompared, differential.standingFailed = map[string]int{}, map[string]int{}
	askerStands := standingAskers(settled, built)
	for _, identifier := range slices.Sorted(maps.Keys(settled)) {
		packageSettled, isResult := settled[identifier].(*standing_facts.Result)
		if !isResult || !packageSettled.Partial || packageSettled.Settlement == nil ||
			len(packageSettled.Settlement.Standing) == 0 {
			continue
		}
		hosted, builtPackage := sides.packages[identifier], built[identifier]
		capturedPackage := captured.Packages[identifier]
		if hosted == nil || builtPackage == nil || builtPackage.Types == nil || capturedPackage == nil {
			differential.standingFailed["capture"]++
			differential.failures = append(differential.failures, fmt.Sprintf("%s %s: not comparable (host's side "+
				"%t, built %t, captured %t)", build, identifier, hosted != nil, builtPackage != nil,
				capturedPackage != nil))
			continue
		}
		differential.compareStandingPackage(build, identifier, hosted, packageSettled.Settlement.Standing,
			capturedPackage, builtPackage, captured.Askers, askerStands)
	}
}

// standingAskers is whether an asker, a body whose rule asked an answer, stands in the build whose facts' reach
// settled the packages settled holds a result of, by ID, of built, the packages its last load checked: placed in no
// file, it is taken as standing, the side that fails; in a package the build lints whole, or that has no
// settlement, nothing stands.
func standingAskers(settled map[string]any, built map[string]*packages.Package) func(asker fact_resolver.Asker) bool {
	identifiers := make(map[*types.Package]string, len(built))
	for identifier, builtPackage := range built {
		identifiers[builtPackage.Types] = identifier
	}
	return func(asker fact_resolver.Asker) bool {
		if asker.File == "" {
			return true
		}
		askerSettled, _ := settled[identifiers[asker.Package]].(*standing_facts.Result)
		return askerSettled.Stands(asker.File, asker.Offset)
	}
}

// compareStandingPackage is the second leg over one package, identifier, whose host's side is hosted, whose bodies
// standing still stand, and whose capture and types in the build are capturedPackage's and builtPackage's.
func (differential *buildBodyDifferential) compareStandingPackage(
	build string,
	identifier string,
	hosted *factReachPackage,
	standing map[fact_reach.Place]bool,
	capturedPackage *check.FactReachPackage,
	builtPackage *packages.Package,
	askers map[string]map[types.Object][]fact_resolver.Asker,
	askerStands func(asker fact_resolver.Asker) bool,
) {
	renderer := build_reads.NewIdentityRenderer(builtPackage.Fset, nil)
	// The build's functions by key: every function and method its package declares at package level, as a body of the
	// record is declared, rendered once.
	functions := map[fact_reach.Object]*types.Func{}
	keep := func(function *types.Func) {
		if key, known := factReachObject(renderer.Object(function)); known {
			functions[key] = function
		}
	}
	scope := builtPackage.Types.Scope()
	for _, name := range scope.Names() {
		switch object := scope.Lookup(name).(type) {
		case *types.Func:
			keep(object)
		case *types.TypeName:
			if named, isNamed := object.Type().(*types.Named); isNamed && !object.IsAlias() {
				for index := range named.Origin().NumMethods() {
					keep(named.Origin().Method(index))
				}
			}
		}
	}
	answered := map[types.Object][]check.ExportedFact{}
	for _, answer := range capturedPackage.Answers {
		answered[answer.Object] = append(answered[answer.Object], answer)
	}
	hostFunctions := map[fact_reach.Object]fact_reach.Function{}
	for _, function := range hosted.Functions() {
		hostFunctions[function.Object] = function
	}
	fail := func(place fact_reach.Place, name string, kind string, host string, built string) {
		differential.standingFailed[kind]++
		differential.failures = append(differential.failures, fmt.Sprintf("%s %s %s:%d %s %s: host %s, build %s",
			build, identifier, place.File, place.Offset, name, kind, host, built))
	}
	for _, place := range slices.SortedFunc(maps.Keys(standing), func(first, second fact_reach.Place) int {
		return cmp.Or(cmp.Compare(first.File, second.File), cmp.Compare(first.Offset, second.Offset))
	}) {
		for _, key := range hosted.declared[place] {
			function := functions[key]
			differential.standingCompared[standingNoReturn]++
			hostFunction, isHosted := hostFunctions[key]
			if function == nil {
				// The build declares no function the key names, so nothing of it can be compared, which is a failure.
				hostNoReturn := "none"
				if isHosted {
					hostNoReturn = fmt.Sprint(hostFunction.NoReturn)
				}
				fail(place, hostFunction.Identity.String(), standingNoReturn, hostNoReturn, "none")
				continue
			}
			name := standingName(function)
			switch {
			case !isHosted:
				fail(place, name, standingNoReturn, "none", fmt.Sprint(capturedPackage.NoReturn(function)))
			case hostFunction.NoReturn != capturedPackage.NoReturn(function):
				fail(place, name, standingNoReturn, fmt.Sprint(hostFunction.NoReturn),
					fmt.Sprint(capturedPackage.NoReturn(function)))
			}
			// The function's other pushed facts, by their analyzer's name, wherever both runs held its facts of the
			// package: a wrapper's, which the facts' reach compares, and a deprecation, which it leaves to the reach by
			// its written argument (standing_facts' pushedFacts), proven here on every census.
			for _, analyzer := range slices.Sorted(maps.Keys(hostFunction.Pushed)) {
				builtFacts, isHeld := capturedPackage.Pushed[analyzer]
				if !isHeld {
					continue
				}
				differential.standingCompared[analyzer]++
				if built := builtFacts[function]; built != hostFunction.Pushed[analyzer] {
					fail(place, name, analyzer, strconv.Quote(hostFunction.Pushed[analyzer]), strconv.Quote(built))
				}
			}
			// The answers about the function, then about each of its parameters, as fact_reach asks them.
			objects := []types.Object{function}
			for index := range function.Signature().Params().Len() {
				objects = append(objects, function.Signature().Params().At(index))
			}
			for parameter, object := range objects {
				for _, answer := range answered[object] {
					differential.standingCompared[answer.Analyzer]++
					question := factReachQuestion(answer.Analyzer, key, parameter-1)
					answeredName := name
					if parameter > 0 {
						answeredName = fmt.Sprintf("%s parameter %d", name, parameter-1)
					}
					asking, isAsked := hosted.Asking(question.Key)
					switch {
					case !isAsked:
						// Only the build holds the answer: judged by who asked it (see the file's comment).
						asked := askers[answer.Analyzer][object]
						switch {
						case len(asked) == 0:
							differential.heldUnasked++
						case slices.ContainsFunc(asked, askerStands):
							fail(place, answeredName, answer.Analyzer, "none", string(answer.Encoded))
						default:
							differential.askedByReached++
						}
					case !bytes.Equal(asking.Encoded, answer.Encoded):
						fail(place, answeredName, answer.Analyzer, string(asking.Encoded), string(answer.Encoded))
					}
				}
			}
		}
	}
}

// standingName is how the second leg names function: its name, after its receiver's type's for a method.
func standingName(function *types.Func) string {
	if receiver := function.Signature().Recv(); receiver != nil {
		return types.TypeString(receiver.Type(), func(*types.Package) string { return "" }) + "." + function.Name()
	}
	return function.Name()
}
