package dispatch

import (
	"go/ast"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/timing"
	"github.com/system-inc/cohere/internal/go_engine/timing_testing"
)

// Under --timing a dispatched listener's calls are billed to its rule, never to the dispatcher that made them: a
// listener that spins a known amount of its own thread's CPU on entering each file is billed at least that under its
// rule's name, with a call for each file, and the dispatcher's own row, its walk less its listeners' calls, holds
// almost none of it. The dispatcher is a NewSyntax of the test's own, so no other test's rules are on it.
func TestADispatchedListenersCpuIsBilledToItsRuleNotToTheDispatcher(test *testing.T) {
	test.Parallel()
	timing_testing.RequireThreadClock(test)
	const spinPerFile = 10 * time.Millisecond
	var spun time.Duration
	files := 0
	dispatcher := NewSyntax()
	spinning := dispatcher.RuleAnalyzer(rule.Namespace+"spinning", "spinning", "spins on entering each file",
		&rule.Listener{
			FileEnter: func(*rule.Context, *ast.File) {
				spun += timing_testing.Spin(spinPerFile)
				files++
			},
		})
	quiet := dispatcher.RuleAnalyzer(rule.Namespace+"quiet", "quiet", "hears each identifier and does nothing",
		&rule.Listener{
			Nodes: []ast.Node{(*ast.Ident)(nil)},
			Node: func(*rule.Context, ast.Node, []ast.Node) {
				// Heard only to be called, so its rule has calls and next to no CPU of its own.
			},
		})
	collector := timing.New()
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{spinning, quiet}, loadFixture(test, "walked"),
		&checker.Options{Timing: collector, Timed: rule.Timed})
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}
	if files != 2 {
		test.Fatalf("the spinning listener entered %d files, want the fixture's 2", files)
	}

	record := collector.Record(func(analyzer string) (string, contract.SharedKind) {
		switch analyzer {
		case spinning.Name, quiet.Name:
			return rule.Namespace + analyzer, ""
		case dispatcher.Analyzer().Name:
			return "", contract.SharedDispatcher
		}
		return "", contract.SharedAnalyzer
	})
	rules := map[string]contract.RuleTiming{}
	for _, row := range record.Rules {
		rules[row.Name] = row
	}
	var dispatcherRow contract.SharedTiming
	for _, row := range record.Shared {
		if row.Kind == contract.SharedDispatcher {
			dispatcherRow = row
		}
	}
	spinningRow := rules[rule.Namespace+"spinning"]
	if spinningRow.Calls != int64(files) || spinningRow.Packages != 1 || dispatcherRow.Packages != 1 {
		test.Fatalf("the spinning rule's row is %+v and the dispatcher's %+v, want %d calls and 1 package each",
			spinningRow, dispatcherRow, files)
	}
	if rules[rule.Namespace+"quiet"].Calls == 0 {
		test.Errorf("the quiet rule's row is %+v, with no call to its listener", rules[rule.Namespace+"quiet"])
	}
	billed := time.Duration(spinningRow.CpuMicroseconds) * time.Microsecond
	// Less the calibrated reads each measurement takes out, and what the record rounds away.
	if allowance := time.Duration(files+2) * time.Microsecond; billed < spun-allowance {
		test.Errorf("a listener that measured %v of its own CPU was billed %v to its rule", spun, billed)
	}
	if walked := time.Duration(dispatcherRow.CpuMicroseconds) * time.Microsecond; walked > spun/4 {
		test.Errorf("the dispatcher's own row is %v, after its listener spun %v, so the listener's calls were not "+
			"taken out of it", walked, spun)
	}
	test.Logf("the spinning rule billed %v against %v it measured itself; the dispatcher's walk %dµs", billed, spun,
		dispatcherRow.CpuMicroseconds)
}
