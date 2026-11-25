package go_language

import (
	"github.com/system-inc/cohere/internal/go_language/build_dependence"
)

// findingCauses is what a build shows of the causes the later stages own (#w72zxtq, the review's must B), by which a
// findings difference is classed only where its body's cause is shown, never by its rules' names alone:
//   - CalleeFacts, never yet: the build cannot show that a function the body calls holds another fact than the host's.
//     The facts census compares the facts analyzers export, but the fact resolver's answers, purity's and nil-ness's,
//     are recorded only for a run with a store (fact_resolver's answerOf), and the census's builds run without one,
//     so a callee's moved purity reads as no difference. Vet's comparison of the resolver's answers for stage 4's
//     unit B (check.Options.FactReach, #5vbs1np) is what this class will read; until it lands a difference owed to
//     stage 4 alone is a failure, saying so.
//   - PackageWide, where the body's package holds a unit the build judges dependent (Dependence.UnitDependent), as
//     stage 5 checks such a package whole for a package-wide rule (rules.InputsComposes): a unit dependence makes
//     dependent, a body the census found dependent, or a file only one build compiles (filesDiffer), whose units are
//     dependent by definition.
//
// dependence is the build's judgment of the host's graph, and dependentPackages the packages holding a unit it makes
// dependent, read at the first package-wide difference.
type findingCauses struct {
	dependence        *build_dependence.Dependence
	filesDiffer       map[string]bool
	dependentPackages map[string]bool
}

// shown reports whether the build shows stage's cause for body.
func (causes *findingCauses) shown(stage findingStage, body reachedBody) bool {
	if stage != stagePackageWide {
		return false
	}
	if body.unitDependent || causes.filesDiffer[body.packageIdentifier] {
		return true
	}
	if causes.dependentPackages == nil {
		causes.dependentPackages = map[string]bool{}
		for _, unit := range causes.dependence.Units() {
			if unit.Dependent {
				causes.dependentPackages[unit.PackageId] = true
			}
		}
	}
	return causes.dependentPackages[body.packageIdentifier]
}
