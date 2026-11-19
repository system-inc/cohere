package go_engine

// The standing bodies on the census path (#e4rd5dw), behind `COHERE_STANDING_BODIES=1` with the body reach census or
// the body differential under `--build-dependence --other-builds`: an extra build whose passes are handed a host's side
// for the facts' reach lints only the function bodies that do not stand, and takes the host's findings for the rest.
// What stands in a package is standing_facts' answer there, the bodies the reach leaves unreached once every differing
// fact and answer has taken its users out (#aswfm7a). Off, which is the default, every build lints every body, as
// before.

// standingBodiesVariable, set to 1, turns on the standing bodies on the census path (pipeline_standing_bodies.go).
const standingBodiesVariable = "COHERE_STANDING_BODIES"

// leavesBodiesStanding is whether the run's extra builds leave the bodies standing_facts settles as standing to the
// host's findings: the switch is on and the run takes the census path, whose builds are handed the host's sides for the
// facts' reach (takesBodyReach). A run off the census path never leaves a body standing, the switch or not.
func (engine *pipeline) leavesBodiesStanding() bool {
	return engine.standingBodies && engine.takesBodyReach()
}
