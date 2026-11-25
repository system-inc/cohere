package rule

import (
	"fmt"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// Each key the checker hands every pass never runs, and an analyzer that required one would fail on every package,
// naming it: the key's Run, on a pass of its own over a planted package, returns no result and the refusal that names
// the package and the key (#2br8dpa).
func TestAKeyRefusesToRun(test *testing.T) {
	test.Parallel()
	keys := []*analysis.Analyzer{BodyRecords, Dependents, FactHost, Failed, Inherited, Recorded, Resolved, Standing,
		Timed}
	for _, key := range keys {
		test.Run(key.Name, func(test *testing.T) {
			test.Parallel()
			pass := &analysis.Pass{Analyzer: key, Pkg: types.NewPackage("example.com/planted", "planted")}
			result, runError := key.Run(pass)
			if result != nil {
				test.Errorf("%s returned the result %v, want none", key.Name, result)
			}
			refusal := fmt.Sprintf("an analyzer on example.com/planted requires %s, which is the key of ", key.Name)
			if runError == nil || !strings.HasPrefix(runError.Error(), refusal) {
				test.Errorf("%s returned the error %v, want the refusal starting %q", key.Name, runError, refusal)
			}
		})
	}
}
