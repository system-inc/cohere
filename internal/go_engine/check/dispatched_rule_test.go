package check

import (
	"go/ast"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// A dispatched rule the run was not given never reaches its result, though its listener is registered on the very
// dispatcher an enabled rule requires, so the dispatcher calls it on every package: its finding on base and its
// panic on user are its outcome alone, which no analyzer of the run reads. The enabled rule's findings are all there,
// and nothing else is.
func TestADisabledDispatchedRuleNeverReachesTheResult(test *testing.T) {
	test.Parallel()

	dispatcher := dispatch.NewSyntax()
	enabledListener := &rule.Listener{
		Nodes: []ast.Node{(*ast.FuncDecl)(nil)},
		Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
			declaration, isDeclaration := node.(*ast.FuncDecl)
			if !isDeclaration {
				return
			}
			context.Report(analysis.Diagnostic{Pos: declaration.Name.Pos(), Message: "declares " +
				declaration.Name.Name})
		},
	}
	disabledCalls := map[string]int{}
	disabledListener := &rule.Listener{
		Nodes: []ast.Node{(*ast.FuncDecl)(nil)},
		Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
			disabledCalls[context.Package.Name()]++
			if context.Package.Name() == "user" {
				panic("planted panic in the disabled rule")
			}
			context.Report(analysis.Diagnostic{Pos: node.Pos(), Message: "the disabled rule's finding"})
		},
	}
	enabled := rule.Rule{
		Name:   rule.Namespace + "test-dispatched-enabled",
		Origin: rule.OriginHouse,
		Analyzer: dispatcher.RuleAnalyzer(rule.Namespace+"test-dispatched-enabled", "dispatched_enabled",
			"reports every function declaration", enabledListener),
		Listener: enabledListener,
	}
	disabledName := rule.Namespace + "test-dispatched-disabled"
	dispatcher.RuleAnalyzer(disabledName, "dispatched_disabled", "reports on base and panics on user",
		disabledListener)

	// Single-threaded, since the disabled listener counts its calls on both packages into one map.
	result := runRules(test, Options{SingleThreaded: true}, enabled)
	if disabledCalls["base"] == 0 || disabledCalls["user"] == 0 {
		test.Fatalf("the disabled rule's listener was called %v, so the dispatcher never gave it a finding and a "+
			"panic to keep out of the result", disabledCalls)
	}
	messages := []string{}
	for _, finding := range result.Findings {
		if finding.Rule != enabled.Name {
			test.Errorf("a finding of %s reached the result: %+v", finding.Rule, finding)
			continue
		}
		messages = append(messages, finding.Message)
	}
	slices.Sort(messages)
	if expected := []string{"declares Marked", "declares Target", "declares Use"}; !slices.Equal(messages,
		expected) {
		test.Errorf("the enabled rule reported %q, where exactly %q was expected", messages, expected)
	}
	for _, crash := range result.Crashes {
		test.Errorf("a crash reached the result: %+v", crash)
		if strings.Contains(crash.Error, "disabled") {
			test.Errorf("the disabled rule's panic reached the result as a crash")
		}
	}
	if slices.Contains(result.CrashedRules, disabledName) || len(result.CrashedRules) != 0 {
		test.Errorf("the run lists %q as crashed, where no rule it was given crashed", result.CrashedRules)
	}
	if _, counted := result.FindingsByRule[disabledName]; counted || result.RulesRun != 1 {
		test.Errorf("the run counts %d rules and %v findings by rule, where it was given the enabled rule alone",
			result.RulesRun, result.FindingsByRule)
	}
}
