package go_engine

import (
	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
)

// buildJudgment is one extra build's judgment on the host's graph (dependenceOf), and what its facts are compared
// through: the files both builds compile, by package ID and path as written, and the packages both have by one ID.
// standardLibrary is its judgment of the standard library's graph, nil when the host has none, and counterparts find
// the host's objects of a restored package in that graph, the functions its units call among them (#ss4t5g6).
// filesDiffer are the IDs of the packages both builds have whose files differ, a file one compiles and the other does
// not, every unit of which is dependent (build_dependence.PackageDifference), which the body differential's
// package-wide class reads (findingCauses). mustCheck is the build's must-check set as the census writes it, nil
// unless the run was asked for the census (pipeline_must_check_census.go).
type buildJudgment struct {
	dependence      *build_dependence.Dependence
	standardLibrary *build_dependence.Dependence
	counterparts    *build_dependence.Counterparts
	shared          map[sharedFile]bool
	sharedPackages  map[string]bool
	filesDiffer     map[string]bool
	mustCheck       *mustCheckSet
}
