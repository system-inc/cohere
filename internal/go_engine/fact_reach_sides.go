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
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// factReachSides are the host's side of each package for the facts' reach (#aswfm7a), made once as the extra builds
// start, on the pipeline's goroutine, and only read after, by every build at once (factReachPackage). packages are the
// sides by package ID: one for each package the host analyzed whose side could be made. Every other package gets
// none, and a build lints it whole; the census counts why: uncaptured, a package the host's run holds no side of,
// restored or handed an unsure answer (check.FactReachCapture.Packages); storeAnswered, one the run learned and the
// store answered a question about, whose record no kind can call complete (check.FactReachCapture.StoreAnswered);
// unrecorded, one with no record of what each of its declarations uses; and orderUnknown, one whose answers hang on the
// host's order in a way no kind could say (check.FactReachPackage.OrderKnown).
type factReachSides struct {
	packages      map[string]*factReachPackage
	uncaptured    int
	storeAnswered int
	unrecorded    int
	orderUnknown  int
}

// newFactReachSides is the host's side of each of analyzed, the packages the host's lint ran over, from host, the body
// reach's side, whose reads hold each package's record and whose packages its file set, and capture, what the host's
// run held of its facts. Each package's identities are rendered by a renderer of its own, on this goroutine. Nothing of
// capture is kept: its no-return predicates are read here, as each side's functions are met, and never again.
func newFactReachSides(
	host *bodyReachHost,
	capture *check.FactReachCapture,
	analyzed []*packages.Package,
) *factReachSides {
	sides := &factReachSides{packages: make(map[string]*factReachPackage, len(capture.Packages))}
	for _, analyzedPackage := range analyzed {
		captured := capture.Packages[analyzedPackage.ID]
		if captured == nil {
			if capture.StoreAnswered[analyzedPackage.ID] {
				sides.storeAnswered++
			} else {
				sides.uncaptured++
			}
			continue
		}
		hosted := host.packages[analyzedPackage.ID]
		var record *build_reads.FunctionUses
		isRecorded := false
		if reads := host.reads[hosted]; hosted != nil && reads != nil {
			record, isRecorded = reads.FunctionUses()
		}
		if !isRecorded {
			sides.unrecorded++
			continue
		}
		if !captured.OrderKnown {
			sides.orderUnknown++
			continue
		}
		// The answers about other packages' objects: the package's own are its captured Answers.
		imported := make([]check.ExportedFact, 0, len(capture.Answers))
		for _, answer := range capture.Answers {
			if answer.Object.Pkg() != hosted.Types {
				imported = append(imported, answer)
			}
		}
		sides.packages[analyzedPackage.ID] = newFactReachPackage(factReachInputs{
			fileSet:      hosted.Fset,
			record:       record,
			renderer:     build_reads.NewIdentityRenderer(hosted.Fset, nil),
			answers:      captured.Answers,
			imported:     imported,
			noReturn:     captured.NoReturn,
			pushed:       captured.Pushed,
			orderBound:   captured.OrderBound,
			presence:     captured.Presence,
			presenceRead: captured.PresenceRead,
			markers:      captured.Markers,
			identifier:   analyzedPackage.ID,
			bodies:       captured.Bodies,
			recordsHeld:  captured.RecordsHeld,
		})
	}
	return sides
}

// inBuild is, by package ID, the side the build whose body reach census is reach hands its passes on each package that
// has one and that the census judged: the package's bodies the reach leaves unreached are its standing set, and the
// build settles the package when any stands or any of its variable initializers is unreached, and lints it whole,
// asking nothing, otherwise (factReachPackage.inBuild). Only the side's own places stand, its function bodies, never
// a value's initializer, which the census judges too: the facts' reach follows a package's variable initializers as
// one asker that is no place and never stands (factReachInitializers). So a package with initializers alone unreached
// is settled with nothing standing (#bed0mnk): the build still lints all of it, and the settlement says whether its
// initializers leave, which the body differential would else compare with the host's whatever a fact reads there. Two
// bodies at one place, as a cgo package may hold, stand once. Any other package has no side here, and the build lints
// it whole. It only reads the sides, on the build's own goroutine, so every build may call it at once.
func (sides *factReachSides) inBuild(reach *buildBodyReach) map[string]fact_reach.Host {
	unreached := map[string]map[fact_reach.Place]bool{}
	for _, body := range reach.bodies {
		if sides.packages[body.packageIdentifier] == nil {
			continue
		}
		if unreached[body.packageIdentifier] == nil {
			unreached[body.packageIdentifier] = map[fact_reach.Place]bool{}
		}
		if body.reason == "" {
			unreached[body.packageIdentifier][fact_reach.Place{File: body.file, Offset: body.offset}] = true
		}
	}
	hosts := make(map[string]fact_reach.Host, len(unreached))
	for identifier, bodies := range unreached {
		standing := map[fact_reach.Place]bool{}
		for _, place := range sides.packages[identifier].Places() {
			if bodies[place] {
				standing[place] = true
			}
		}
		hosts[identifier] = sides.packages[identifier].inBuild(standing,
			len(standing) > 0 || sides.packages[identifier].initializers.anyAmong(bodies))
	}
	return hosts
}

// buildFactReach is one build's census of the facts' reach, of its last load: sides, the packages it handed a host's
// side, and of them settled, those it settled, with bodies standing or with none
// (standing_facts.Result.Partial), crashed, those it failed to settle on; and, summed over the settled ones, the
// no-return facts compared and the objects they named, the other pushed facts compared and the functions they named,
// the answers the host read from other packages asked before the
// walk and the objects they named, the order-bound objects and the members of the build's own cycles of calls named
// before anything was asked, the answers the walk asked and those that read differing or unknown, by kind, the
// functions of a body that left with no record of a kind, by kind, the bodies the facts took out of the standing
// set, and the objects each presence row named and the bodies that left for it, by row.
type buildFactReach struct {
	// unsettled is why the build settled nothing though it was asked to (check.Result.FactReachUnsettled), and every
	// other count is zero then.
	unsettled string

	sides             int
	settled           int
	crashed           int
	functionsCompared int
	functionsNamed    int
	pushedCompared    int
	pushedNamed       int
	importedAsked     int
	importedNamed     int
	orderBoundNamed   int
	buildOrderNamed   int
	asked             int
	differing         map[string]int
	unknown           map[string]int
	unrecordedKinds   map[string]int
	removed           int
	presenceNamed     map[string]int
	presenceRemoved   map[string]int
}

// newBuildFactReach is the census of the build whose passes were handed hosts, by package ID, and whose check.Run gave
// result, the last load's.
func newBuildFactReach(hosts map[string]fact_reach.Host, result *check.Result) *buildFactReach {
	census := &buildFactReach{sides: len(hosts), crashed: len(result.FactReachCrashes), differing: map[string]int{},
		unknown: map[string]int{}, unrecordedKinds: map[string]int{}, presenceNamed: map[string]int{},
		presenceRemoved: map[string]int{}}
	for identifier, settled := range result.FactReachSettled {
		packageSettled, isResult := settled.(*standing_facts.Result)
		if !isResult || !packageSettled.Partial || packageSettled.Settlement == nil {
			continue
		}
		census.settled++
		census.functionsCompared += packageSettled.FunctionsCompared
		census.functionsNamed += packageSettled.FunctionsNamed
		census.pushedCompared += packageSettled.PushedCompared
		census.pushedNamed += packageSettled.PushedNamed
		census.importedAsked += packageSettled.ImportedAsked
		census.importedNamed += packageSettled.ImportedNamed
		census.buildOrderNamed += packageSettled.BuildOrderNamed
		if hosted := hosts[identifier]; hosted != nil {
			census.orderBoundNamed += len(hosted.OrderBound())
		}
		census.asked += packageSettled.Settlement.Asked
		for kind, count := range packageSettled.Settlement.Differing {
			census.differing[kind] += count
		}
		for kind, count := range packageSettled.Settlement.Unknown {
			census.unknown[kind] += count
		}
		for kind, count := range packageSettled.Settlement.Unrecorded {
			census.unrecordedKinds[kind] += count
		}
		census.removed += len(packageSettled.Settlement.Removed)
		for row, count := range packageSettled.PresenceNamed {
			census.presenceNamed[string(row)] += count
		}
		for row, count := range packageSettled.PresenceRemoved {
			census.presenceRemoved[string(row)] += count
		}
	}
	return census
}

// writeBuildFactReach writes one build's census of the facts' reach to written.
func writeBuildFactReach(written *strings.Builder, census *buildFactReach) {
	if census.unsettled != "" {
		fmt.Fprintf(written, "  fact reach: no settle: %s\n", census.unsettled)
		return
	}
	fmt.Fprintf(written, "  fact reach: sides %d, settled %d, crashed %d\n", census.sides, census.settled,
		census.crashed)
	fmt.Fprintf(written, "    functions compared %d, named %d; imported asked %d, named %d; order bound named %d\n",
		census.functionsCompared, census.functionsNamed, census.importedAsked, census.importedNamed,
		census.orderBoundNamed)
	fmt.Fprintf(written, "    answers asked %d, differing %s, unknown %s; bodies taken out %d\n", census.asked,
		byKind(census.differing), byKind(census.unknown), census.removed)
	fmt.Fprintf(written, "    functions with no record of a kind %s; build order named %d; pushed facts compared %d, "+
		"named %d\n", byKind(census.unrecordedKinds), census.buildOrderNamed, census.pushedCompared, census.pushedNamed)
	fmt.Fprintf(written, "    presence named %s; bodies taken out for presence %s\n", byKind(census.presenceNamed),
		byKind(census.presenceRemoved))
}

// byKind is counts written by kind, the kinds in order, or none.
func byKind(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	kinds := slices.SortedFunc(maps.Keys(counts), cmp.Compare[string])
	written := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		written = append(written, fmt.Sprintf("%s %d", kind, counts[kind]))
	}
	return strings.Join(written, ", ")
}
