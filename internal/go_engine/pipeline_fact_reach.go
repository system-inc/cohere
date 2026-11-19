package go_engine

import (
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// factReachSides are the host's side of each package for the facts' reach (#aswfm7a), made once as the extra builds
// start, on the pipeline's goroutine, and only read after, by every build at once (factReachPackage). packages are the
// sides by package ID: one for each package the host analyzed whose side could be made. Every other package gets
// none, and a build lints it whole; the census counts why: uncaptured, a package the host's run holds no side of,
// restored or handed an unsure answer (check.FactReachCapture.Packages); unrecorded, one with no record of what each
// of its declarations uses; and orderUnknown, one whose answers hang on the host's order in a way no kind could say
// (check.FactReachPackage.OrderKnown).
type factReachSides struct {
	packages     map[string]*factReachPackage
	uncaptured   int
	unrecorded   int
	orderUnknown int
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
			sides.uncaptured++
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
			fileSet:    hosted.Fset,
			record:     record,
			renderer:   build_reads.NewIdentityRenderer(hosted.Fset, nil),
			answers:    captured.Answers,
			imported:   imported,
			noReturn:   captured.NoReturn,
			orderBound: captured.OrderBound,
		})
	}
	return sides
}

// inBuild is, by package ID, the side the build whose body reach census is reach hands its passes on each package that
// has one and that the census judged: the package's bodies the reach leaves unreached are its standing set, and the
// build lints it in part when any stands, whole when none does (factReachPackage.inBuild). Only the side's own places
// stand, its function bodies, never a value's initializer, which the census judges too and no fact's reach follows;
// two bodies at one place, as a cgo package may hold, stand once. Any other package has no side here, and the build
// lints it whole. It only reads the sides, on the build's own goroutine, so every build may call it at once.
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
		hosts[identifier] = sides.packages[identifier].inBuild(standing, len(standing) > 0)
	}
	return hosts
}
