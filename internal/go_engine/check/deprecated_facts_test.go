package check

import (
	"reflect"
	"testing"

	"honnef.co/go/tools/analysis/facts/deprecated"
)

// A deprecation is its own package's alone, the premise the walk reads a Deprecated lookup by
// (DependenceSet.OwnFactPaths): the deprecated facts analyzer requires no other analyzer, so none of another's facts,
// and learns its one fact type. That its run reads only its own files' doc comments, and imports no fact of another
// package (honnef.co/go/tools v0.8.1, analysis/facts/deprecated/deprecated.go:31-139), no field of the analyzer says;
// this pins what does, so a version that came to build on another analyzer's facts goes red here.
func TestADeprecationIsItsOwnPackagesAlone(test *testing.T) {
	test.Parallel()

	if len(deprecated.Analyzer.Requires) != 0 {
		test.Errorf("%s requires %d analyzers, want none, so its facts are its own package's", deprecated.Analyzer.Name,
			len(deprecated.Analyzer.Requires))
	}
	if len(deprecated.Analyzer.FactTypes) != 1 ||
		reflect.TypeOf(deprecated.Analyzer.FactTypes[0]) != deprecatedFactType {
		test.Errorf("%s learns %v, want its deprecation alone", deprecated.Analyzer.Name, deprecated.Analyzer.FactTypes)
	}
}
