package check

import (
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// A run that lets the analyzed packages' syntax go as the rules finish on them gives the Result a run that keeps it
// gives, the walk of every file and the fact base marks user with included, and leaves the analyzed packages
// without their syntax and type information. Each run has a load of its own, since the release changes the load.
func TestReleasingTheAnalyzedPackagesChangesNoResult(test *testing.T) {
	test.Parallel()

	rules := []rule.Rule{houseRule(markedAnalyzer), houseRule(targetAnalyzer)}
	kept, keptError := Run(ownLoad(test, "."), rules, Options{})
	if keptError != nil {
		test.Fatal(keptError)
	}
	analyzed := ownLoad(test, ".")
	released, releasedError := Run(analyzed, rules, Options{ReleaseAnalyzed: true})
	if releasedError != nil {
		test.Fatal(releasedError)
	}
	if len(kept.Findings) == 0 || kept.FilesWalked == 0 {
		test.Fatalf("want findings and a walk to compare, got %+v", kept)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(released), withoutTimesOrCache(kept)) {
		test.Fatalf("released\n%+v\nkept\n%+v", withoutTimesOrCache(released), withoutTimesOrCache(kept))
	}
	for _, analyzedPackage := range analyzed {
		if analyzedPackage.Syntax != nil || analyzedPackage.TypesInfo != nil {
			test.Fatalf("%s kept its syntax or type information, so the release let nothing go", analyzedPackage.ID)
		}
	}
}
