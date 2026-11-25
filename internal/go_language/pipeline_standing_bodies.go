package go_language

import (
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules"
)

// The standing bodies on the census path (#e4rd5dw), behind `COHERE_STANDING_BODIES=1` with the body reach census or
// the body differential under `--build-dependence --other-builds`: an extra build whose passes are handed a host's side
// for the facts' reach lints only the function bodies that do not stand, and takes the host's findings for the rest.
// What stands in a package is standing_facts' answer there, the bodies the reach leaves unreached once every differing
// fact and answer has taken its users out (#aswfm7a). Off, which is the default, every build lints every body, as
// before.
//
// Only a confined rule takes the host's findings: one that is dispatched, so its dispatcher's walk can leave a standing
// body unheard by it, and that the inputs table declares PerUnit or FileSyntax, so its findings in a unit the reach and
// the facts leave alike are the host's there. Every other rule hears every body and keeps its own findings.

// standingBodiesVariable, set to 1, turns on the standing bodies on the census path (pipeline_standing_bodies.go).
const standingBodiesVariable = "COHERE_STANDING_BODIES"

// leavesBodiesStanding is whether the run's extra builds leave the bodies standing_facts settles as standing to the
// host's findings: the switch is on and the run takes the census path, whose builds are handed the host's sides for the
// facts' reach (takesBodyReach). A run off the census path never leaves a body standing, the switch or not.
func (engine *pipeline) leavesBodiesStanding() bool {
	return engine.standingBodies && engine.takesBodyReach()
}

// standingConfinedRules are the rules of enabled confined for the standing bodies, by name: each with a Listener that
// rules.DeclaredInputs, with the declarations a test planted (plantedInputs) laid over it, declares PerUnit or
// FileSyntax. It is asked once a run, as the extra builds start, and only when the run leaves bodies standing.
func (engine *pipeline) standingConfinedRules(enabled []rule.Rule) map[string]bool {
	// Ignored: a table that does not parse is TestTheInputsTableParses's to fail, and confines only what a test
	// planted, so every other rule hears every body.
	declared, _ := rules.DeclaredInputs()
	confined := map[string]bool{}
	for _, each := range enabled {
		declaration, isDeclared := engine.plantedInputs[each.Name]
		if !isDeclared {
			declaration = declared[each.Name]
		}
		if each.Listener != nil &&
			(declaration.Inputs == rules.InputsPerUnit || declaration.Inputs == rules.InputsFileSyntax) {
			confined[each.Name] = true
		}
	}
	return confined
}
