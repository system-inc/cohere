package checker

import (
	"fmt"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// Analyze makes its actions in blocks, so a block must be its own call's: graphs analyzed side by side over the same
// packages, each with more actions than a block holds, hold no action in common, and each action is the one its
// graph asked for, its analyzer and package where the graph walks to it. A block shared between calls would hand two
// graphs the same action, or write one's analyzer over another's.
func TestGraphsAnalyzedSideBySideShareNoAction(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	// A chain of analyzers, each requiring the one before, so every graph holds analyzers times packages actions,
	// more than one block's worth.
	const chained = 20
	analyzers := make([]*analysis.Analyzer, 0, chained)
	for index := range chained {
		chainedAnalyzer := &analysis.Analyzer{
			Name: fmt.Sprintf("chained%d", index),
			Doc:  "chained does nothing but require the one before it",
			Run:  func(*analysis.Pass) (any, error) { return nil, nil },
		}
		if index > 0 {
			chainedAnalyzer.Requires = []*analysis.Analyzer{analyzers[index-1]}
		}
		analyzers = append(analyzers, chainedAnalyzer)
	}
	const graphsSideBySide = 16
	graphs := make([]*Graph, graphsSideBySide)
	analyzeErrors := make([]error, graphsSideBySide)
	var group sync.WaitGroup
	for index := range graphsSideBySide {
		group.Go(func() {
			// Each graph roots a different stretch of the chain, so the calls make their actions in different
			// orders, and a shared block would be taken from at different points.
			graphs[index], analyzeErrors[index] = Analyze(analyzers[index%chained:], roots, &Options{})
		})
	}
	group.Wait()
	owner := map[*Action]int{}
	for index, graph := range graphs {
		if analyzeErrors[index] != nil {
			test.Fatalf("analyzing graph %d: %v", index, analyzeErrors[index])
		}
		actions := 0
		for action := range graph.All() {
			actions++
			if previous, owned := owner[action]; owned {
				test.Fatalf("graphs %d and %d hold the same action, %s", previous, index, action)
			}
			owner[action] = index
			for _, dependency := range action.Deps {
				if dependency.Package != action.Package || dependency.Analyzer != action.Analyzer.Requires[0] {
					test.Fatalf("graph %d: %s depends on %s, want the analyzer it requires on its own package", index,
						action, dependency)
				}
			}
		}
		// Every analyzer of the chain requires those before it, so each graph holds the whole chain on every package.
		if expected := chained * len(roots); actions != expected {
			test.Errorf("graph %d holds %d actions, want %d", index, actions, expected)
		}
	}
}
