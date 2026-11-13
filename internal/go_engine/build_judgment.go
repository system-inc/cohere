package go_engine

import (
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
)

// buildJudgment is one extra build's judgment on the host's graph (dependenceOf), and what its facts are compared
// through: the files both builds compile, by package ID and path as written, and the packages both have by one ID.
// standardLibrary is its judgment of the standard library's graph, nil when the host has none, and counterparts find
// the host's objects of a restored package in that graph, the functions its units call among them (#ss4t5g6).
type buildJudgment struct {
	dependence      *build_dependence.Dependence
	standardLibrary *build_dependence.Dependence
	counterparts    *build_dependence.Counterparts
	shared          map[sharedFile]bool
	sharedPackages  map[string]bool
}

// census is the units the facts census counts, the host's graph's and then the standard library's, each graph's own
// static calls by their index here and every call of a host unit into the standard library found through its
// callee's counterpart, as one graph holding both would have found it; and the unit of a package and file holding an
// offset, read from the graph that holds the package.
func (judged *buildJudgment) census() ([]build_dependence.Unit, func(string, string, int) (int, bool)) {
	units := judged.dependence.Units()
	hostUnits := len(units)
	if judged.standardLibrary == nil {
		return units, judged.dependence.UnitContaining
	}
	for _, unit := range judged.standardLibrary.Units() {
		calls := make([]int, 0, len(unit.Calls))
		for _, callee := range unit.Calls {
			calls = append(calls, hostUnits+callee)
		}
		unit.Calls = calls
		units = append(units, unit)
	}
	for index := range hostUnits {
		for _, callee := range units[index].ExternalCalls {
			counterpart, isFound := judged.counterparts.Counterpart(callee)
			if !isFound {
				continue
			}
			declaring, isDeclared := judged.standardLibrary.UnitDeclaring(counterpart)
			if isDeclared && !slices.Contains(units[index].Calls, hostUnits+declaring) {
				// A copy, since Units hands every caller the graph's own slice.
				units[index].Calls = append(slices.Clone(units[index].Calls), hostUnits+declaring)
			}
		}
	}
	unitContaining := func(packageIdentifier string, file string, offset int) (int, bool) {
		if index, isUnit := judged.dependence.UnitContaining(packageIdentifier, file, offset); isUnit {
			return index, true
		}
		index, isUnit := judged.standardLibrary.UnitContaining(packageIdentifier, file, offset)
		return hostUnits + index, isUnit
	}
	return units, unitContaining
}
