package go_engine

import "testing"

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
