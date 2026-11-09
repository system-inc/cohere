package check

import (
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// representationReader walks every source function of its package in the representation the readers walk, as a rule
// on the engine's own representation does, and finds nothing.
var representationReader = &analysis.Analyzer{
	Name:     "representationReader",
	Doc:      "walks every source function of the package in the readers' representation, and finds nothing",
	Requires: []*analysis.Analyzer{build_intermediate_representation.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		built, readError := build_intermediate_representation.ResultOf(pass)
		if readError != nil {
			return nil, readError
		}
		for range built.SourceFunctions() {
		}
		return nil, nil
	},
}

// A cold run that writes entries builds each root once (#wt5vaft): its readers walk it whole, its entry holds its
// answers computed whole through the resolver, and top's readers ask the resolver of bottom, yet each package's source
// functions are built in one program, the one the readers and the resolver share.
func TestARootIsBuiltOnceInARunThatWritesEntries(test *testing.T) {
	test.Parallel()
	rules := []rule.Rule{houseRule(representationReader), houseRule(purityProbe)}
	census := intermediate_representation.OpenBuildCensus()
	defer census.Close()
	analyzed := answerModule(test, test.TempDir(), "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	result := runCached(test, analyzed, testCaching(test, rules), rules)
	if len(result.Crashes) > 0 || result.Cache.Written != len(analyzed) {
		test.Fatalf("want every package's entry written and no crash, got crashes %+v, %+v", result.Crashes,
			result.Cache)
	}
	if len(result.Findings) != 1 || result.Findings[0].Message != "example.com/answers/bottom.Double pure true" {
		test.Fatalf("want top's question of bottom.Double answered pure, got %+v", result.Findings)
	}
	for _, root := range analyzed {
		if programs := census.Programs(root.Types); programs != 1 {
			test.Errorf("%s was built in %d programs, want 1", root.ID, programs)
		}
	}
}

// A census counts each program a package's source functions are built in, once however many builds reach them, and
// never a stub, a package's functions from types alone in an importer's program, built too.
func TestABuildCensusCountsEachProgramOnce(test *testing.T) {
	test.Parallel()
	census := intermediate_representation.OpenBuildCensus()
	analyzed := answerModule(test, test.TempDir(), "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	for _, loaded := range analyzed {
		for range 2 {
			created := build_intermediate_representation.NewPackage(loaded.Fset, loaded.Types, loaded.Syntax,
				loaded.TypesInfo, nil, false)
			created.Build()
			for _, function := range created.Functions {
				function.Build()
			}
			for _, imported := range loaded.Types.Imports() {
				for _, stub := range created.Program.Package(imported).Functions {
					// A stub package's initializer has no body to build from types; its declared functions build
					// their parameters alone.
					if stub.Object() != nil {
						stub.Build()
					}
				}
			}
		}
	}
	census.Close()
	for _, loaded := range analyzed {
		if programs := census.Programs(loaded.Types); programs != 2 {
			test.Errorf("%s was counted in %d programs, want the 2 it was built in", loaded.ID, programs)
		}
	}
}
