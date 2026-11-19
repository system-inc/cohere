package go_engine

import (
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
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
