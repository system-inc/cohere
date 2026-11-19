package check

import (
	"errors"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// heldHost is a host's side the tests hand a pass and only compare, never ask: its methods are the nil interface's.
type heldHost struct {
	fact_reach.Host
}

// handedHost is the one host's side the tests hand base's passes.
var handedHost = &heldHost{}

// hostAnalyzer reports, on the package clause of each file, that its pass was handed handedHost under rule.FactHost,
// and fails a pass handed any other.
var hostAnalyzer = &analysis.Analyzer{
	Name: "host_handed",
	Doc:  "reports every file of a package whose pass holds the host's side the test handed",
	Run: func(pass *analysis.Pass) (any, error) {
		held, isHeld := pass.ResultOf[rule.FactHost]
		if !isHeld {
			return nil, nil
		}
		if held != fact_reach.Host(handedHost) {
			return nil, errors.New("the pass on " + pass.Pkg.Path() + " holds a host's side the test never handed")
		}
		for _, file := range pass.Files {
			pass.Reportf(file.Name.Pos(), "handed the host's side")
		}
		return nil, nil
	},
}

// Run hands each pass on a package the host's side Options.FactHosts answers for it, under rule.FactHost (#5vbs1np):
// answered for base alone, base's pass holds it and user's does not, and without FactHosts neither does.
func TestRunHandsAPassTheHostsSideFactHostsAnswers(test *testing.T) {
	test.Parallel()
	loadedFixtures(test)
	base := fixturePackage(test, "example.com/fixture/base")
	handed := runRules(test, Options{FactHosts: func(packageId string) fact_reach.Host {
		if packageId != base.ID {
			return nil
		}
		return handedHost
	}}, houseRule(hostAnalyzer))
	if len(handed.Crashes) > 0 {
		test.Fatalf("handing base's passes the host's side crashed: %+v", handed.Crashes)
	}
	if len(handed.Findings) != 1 || handed.Findings[0].File != base.GoFiles[0] {
		test.Errorf("handed base's passes the host's side, the run found %+v, want one finding, in base.go",
			handed.Findings)
	}
	if unhanded := runRules(test, Options{}, houseRule(hostAnalyzer)); len(unhanded.Findings) > 0 {
		test.Errorf("with no FactHosts the run found %+v, want nothing", unhanded.Findings)
	}
}
