package check

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_language/rules/standing_facts"
)

// standingRepresentationSource is one package's one file, with a function that stands and one the build lints.
const standingRepresentationSource = "package lazy\n\n" +
	"// Stood is left to the host.\nfunc Stood(value int) int {\n\tif value > 1 {\n\t\treturn value * 2\n\t}\n" +
	"\treturn value\n}\n\n" +
	"// Kept is the build's own.\nfunc Kept(value int) int {\n\treturn Stood(value) + 1\n}\n"

// stoodFunction is a package's standing bodies under which the function declared at one place stands, every rule is
// confined, and the host holds findings, none, on every file, so a confined rule is confined on each.
type stoodFunction struct {
	filename string
	offset   int
}

// Stands is whether the function at offset of filename is the one that stands.
func (stood stoodFunction) Stands(filename string, offset int) bool {
	return filename == stood.filename && offset == stood.offset
}

// Confines answers that every rule is confined.
func (stoodFunction) Confines(string) bool { return true }

// Findings answers that the host holds no finding on the file, a holding all the same.
func (stoodFunction) Findings(string, string) ([]rule.InheritedFinding, bool) {
	return []rule.InheritedFinding{}, true
}

// Took records nothing: no count is read here.
func (stoodFunction) Took(string, string, int, int) {
	// The test reads what was built, never what was taken.
}

// On the census path, check.Run with Options.FactReachSettle and Options.StandingAfter both standing_facts, as the
// engine sets them, the build of the representation follows standing_facts as the dispatchers do (#e4rd5dw), so it
// holds the package's standing bodies and leaves the package unbuilt: where every run the representation's dispatcher
// starts is confined on the file, the dispatcher builds only Kept, and Stood, which stands, is never built. Were the
// build not to follow, it would hold no answer and build the package whole, Stood included.
func TestOnTheCensusPathAStandingFunctionIsNeverBuilt(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, map[string]string{
		"go.mod":  "module example.com/lazy\n\ngo 1.24\n",
		"lazy.go": standingRepresentationSource,
	})
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 || len(loaded.Packages) != 1 {
		test.Fatalf("loading the fixture: %v %+v, %d packages", loadError, loaded.Diagnostics, len(loaded.Packages))
	}
	stood := stoodFunction{filename: filepath.Join(root, "lazy.go"),
		offset: strings.Index(standingRepresentationSource, "func Stood")}

	dispatcher := dispatch.NewIntermediateRepresentation()
	var seenLock sync.Mutex
	var seen *intermediate_representation.Package
	listener := &rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{
		Function: func(context *rule.Context, function *intermediate_representation.Function) {
			seenLock.Lock()
			seen = function.Package
			seenLock.Unlock()
			if function.Synthetic == "" {
				context.Report(analysis.Diagnostic{Pos: function.Pos(), Message: "declares " + function.Name()})
			}
		},
	}}
	ruleName := rule.Namespace + "test-standing-representation"
	confined := rule.Rule{Name: ruleName, Origin: rule.OriginHouse, Listener: listener,
		Analyzer: dispatcher.RuleAnalyzer(ruleName, "standing_representation", "reports every source function",
			listener)}

	var asked atomic.Bool
	result, runError := Run(loaded.Packages, []rule.Rule{confined}, Options{
		FactReachSettle: standing_facts.Analyzer,
		StandingAfter:   standing_facts.Analyzer,
		StandingBodies: func(_ string, after any) rule.StandingBodies {
			// Asked once for the package, by whichever follower reads the answer first.
			asked.Store(true)
			if _, settled := after.(*standing_facts.Result); !settled {
				return nil
			}
			return stood
		},
	})
	if runError != nil || len(result.Crashes) > 0 || len(result.FactReachCrashes) > 0 {
		test.Fatalf("the run failed: %v, %+v, %+v", runError, result.Crashes, result.FactReachCrashes)
	}
	if !asked.Load() || seen == nil {
		test.Fatalf("the standing bodies were asked: %t, and the dispatcher handed the rule a function: %t, want both",
			asked.Load(), seen != nil)
	}
	built := map[string]bool{}
	for _, function := range seen.Functions {
		built[function.Name()] = function.Blocks != nil
	}
	if !built["Kept"] || built["Stood"] {
		test.Errorf("Kept is built: %t, and Stood: %t, want Kept built and Stood, which stands, never", built["Kept"],
			built["Stood"])
	}
}
