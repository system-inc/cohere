package go_engine

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// The standing bodies' switch leaves a body standing only on the census path: on, in a run judging its builds'
// dependence that asked for the body reach census or the body differential, and in no other run, the switch's own
// default, off, included.
func TestTheStandingBodiesSwitchLeavesBodiesStandingOnTheCensusPathAlone(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		name     string
		engine   *pipeline
		standing bool
	}{
		{"off by default, on the census path", &pipeline{bodyReachCensus: "reach.txt",
			dependence: &hostDependence{}}, false},
		{"on, under the census", &pipeline{standingBodies: true, bodyReachCensus: "reach.txt",
			dependence: &hostDependence{}}, true},
		{"on, under the body differential", &pipeline{standingBodies: true, bodyDifferential: true,
			dependence: &hostDependence{}}, true},
		{"on, with neither census nor differential", &pipeline{standingBodies: true,
			dependence: &hostDependence{}}, false},
		{"on, under the census, judging no build's dependence", &pipeline{standingBodies: true,
			bodyReachCensus: "reach.txt"}, false},
	} {
		if standing := each.engine.leavesBodiesStanding(); standing != each.standing {
			test.Errorf("%s: the run leaves bodies standing %t, want %t", each.name, standing, each.standing)
		}
	}
}

// A rule is confined for the standing bodies when it is dispatched and the inputs table declares it PerUnit or
// FileSyntax, and no other is: over the registry, every confined rule has a listener and one of the two inputs, a
// listener of a package-wide rule and a PerUnit rule that walks its package itself are not confined, and a rule a test
// planted is confined by the inputs the test declares for it.
func TestARuleIsConfinedWhenDispatchedAndDeclaredPerUnitOrFileSyntax(test *testing.T) {
	test.Parallel()

	declared, declaredError := rules.DeclaredInputs()
	if declaredError != nil {
		test.Fatal(declaredError)
	}
	planted := plantedProbe(&enteredFiles{entered: map[string]int{}})
	registered := append(rules.All(), planted)
	engine := &pipeline{plantedInputs: map[string]rules.Declared{plantedProbeName: {Inputs: rules.InputsPerUnit}}}
	confined := engine.standingConfinedRules(registered)

	var listenerComposes, walkerPerUnit string
	for _, each := range registered {
		inputs := declared[each.Name].Inputs
		perUnitOrFileSyntax := inputs == rules.InputsPerUnit || inputs == rules.InputsFileSyntax
		if confined[each.Name] != (each.Listener != nil && perUnitOrFileSyntax) && each.Name != plantedProbeName {
			test.Errorf("%s is confined %t, with a listener %t and declared %s", each.Name, confined[each.Name],
				each.Listener != nil, inputs)
		}
		switch {
		case each.Listener != nil && inputs == rules.InputsComposes && listenerComposes == "":
			listenerComposes = each.Name
		case each.Listener == nil && inputs == rules.InputsPerUnit && walkerPerUnit == "":
			walkerPerUnit = each.Name
		}
	}
	if listenerComposes == "" || walkerPerUnit == "" || confined[listenerComposes] || confined[walkerPerUnit] {
		test.Errorf("want a dispatched package-wide rule and a PerUnit rule that walks itself, neither confined, got "+
			"%q and %q", listenerComposes, walkerPerUnit)
	}
	if !confined[plantedProbeName] {
		test.Errorf("the planted probe, declared PerUnit by the test, is not confined")
	}
	if unplanted := (&pipeline{}).standingConfinedRules([]rule.Rule{planted}); unplanted[plantedProbeName] {
		test.Errorf("the planted probe is confined with no inputs declared for it")
	}
}
