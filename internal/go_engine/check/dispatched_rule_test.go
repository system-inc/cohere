package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// A dispatched rule the run was not given never reaches its result, though its listener is registered on the very
// dispatcher an enabled rule requires. The dispatcher never calls it (TestADispatchedRuleTheRunLeavesOutIsNeverCalled
// counts every callback), and were it called, as it is by a driver that hands the dispatcher no set of dependents,
// its finding on base and its panic on user would be its outcome alone, which no analyzer of the run reads. The
// enabled rule's findings are all there, and nothing else is.
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
	// An error rather than a fatal, so a dispatcher that called it all the same still has its outcome held out below.
	if len(disabledCalls) != 0 {
		test.Errorf("the disabled rule's listener was called %v, where the dispatcher calls only the run's",
			disabledCalls)
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

// A listener whose rule the run leaves out is never called, on either dispatcher, though it registered on the very
// dispatcher the run's enabled rule requires, as every rule a process imports registers: no context, no FileEnter, no
// Node, no Function, no Instruction, no exit, while the enabled rule beside it reports a finding for every function
// the fixtures declare. The left-out listener hears kinds the enabled one does not, so a dispatcher that walked for
// them would call it.
func TestADispatchedRuleTheRunLeavesOutIsNeverCalled(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name string
		// register registers the enabled listener and the left-out one, each counting into its own counter, on a
		// fresh dispatcher of the walk the case is for, and returns the enabled rule.
		register func(enabledName string, leftOutCalls *atomic.Int64) rule.Rule
	}{
		{"syntax", func(enabledName string, leftOutCalls *atomic.Int64) rule.Rule {
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
			dispatcher.RuleAnalyzer(rule.Namespace+"test-left-out-syntax", "left_out_syntax", "counts its calls",
				&rule.Listener{
					FileEnter: func(*rule.Context, *ast.File) { leftOutCalls.Add(1) },
					Nodes:     []ast.Node{(*ast.FuncDecl)(nil), (*ast.CallExpr)(nil), (*ast.Ident)(nil)},
					Node:      func(*rule.Context, ast.Node, []ast.Node) { leftOutCalls.Add(1) },
					FileExit:  func(*rule.Context, *ast.File) { leftOutCalls.Add(1) },
					PackageExit: func(*rule.Context) {
						leftOutCalls.Add(1)
					},
				})
			return rule.Rule{Name: enabledName, Origin: rule.OriginHouse, Listener: enabledListener,
				Analyzer: dispatcher.RuleAnalyzer(enabledName, "enabled_syntax", "reports every declaration",
					enabledListener)}
		}},
		{"intermediate representation", func(enabledName string, leftOutCalls *atomic.Int64) rule.Rule {
			dispatcher := dispatch.NewIr()
			enabledListener := &rule.Listener{
				Function: func(context *rule.Context, function *ir.Function) {
					// The package initializer is synthetic and declared by no one.
					if function.Synthetic != "" {
						return
					}
					context.Report(analysis.Diagnostic{Pos: function.Pos(), Message: "declares " + function.Name()})
				},
			}
			dispatcher.RuleAnalyzer(rule.Namespace+"test-left-out-ir", "left_out_ir", "counts its calls",
				&rule.Listener{
					Function:     func(*rule.Context, *ir.Function) { leftOutCalls.Add(1) },
					Instructions: []ir.Instruction{(*ir.Call)(nil), (*ir.Return)(nil)},
					Instruction:  func(*rule.Context, ir.Instruction) { leftOutCalls.Add(1) },
					FunctionExit: func(*rule.Context, *ir.Function) { leftOutCalls.Add(1) },
					PackageExit: func(*rule.Context) {
						leftOutCalls.Add(1)
					},
				})
			return rule.Rule{Name: enabledName, Origin: rule.OriginHouse, Listener: enabledListener,
				Analyzer: dispatcher.RuleAnalyzer(enabledName, "enabled_ir", "reports every source function",
					enabledListener)}
		}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			leftOutCalls := &atomic.Int64{}
			enabled := testCase.register(rule.Namespace+"test-enabled", leftOutCalls)
			result := runRules(subtest, Options{}, enabled)
			if calls := leftOutCalls.Load(); calls != 0 {
				subtest.Errorf("the left-out rule's listener was called %d times, where its rule is not in the run",
					calls)
			}
			messages := []string{}
			for _, finding := range result.Findings {
				messages = append(messages, finding.Rule+": "+finding.Message)
			}
			slices.Sort(messages)
			expected := []string{enabled.Name + ": declares Marked", enabled.Name + ": declares Target",
				enabled.Name + ": declares Use"}
			if !slices.Equal(messages, expected) || len(result.Crashes) != 0 {
				subtest.Errorf("the run found %q and crashed %+v, where exactly %q and no crash were expected",
					messages, result.Crashes, expected)
			}
		})
	}
}

// failsOnBase fails on the fixtures' base package and hands every other package a result, standing in for an
// analyzer a listener requires that can fail on a package, as staticcheck's config does on a malformed
// staticcheck.conf.
var failsOnBase = &analysis.Analyzer{
	Name:             "fails_on_base",
	Doc:              "fails on base and hands every other package a result",
	RunDespiteErrors: true,
	ResultType:       reflect.TypeFor[string](),
	Run: func(pass *analysis.Pass) (any, error) {
		if pass.Pkg.Path() == "example.com/fixture/base" {
			return nil, fmt.Errorf("planted failure on %s", pass.Pkg.Path())
		}
		return "the result of fails_on_base", nil
	},
}

// A listener whose requirement fails on a package fails only its own rule there, on either dispatcher, though the
// dispatcher requires that requirement for it: the rule's crash reads exactly as check.Run writes it for the same
// rule as a plain analyzer requiring the failing one, cause and all, and is the only crash, while the rule beside it
// on the same dispatcher reports every function, base's included, and the failing rule still reports on user, where
// its requirement held, reading the result it was handed.
func TestAListenersFailedRequirementFailsOnlyItsOwnRule(test *testing.T) {
	test.Parallel()

	// reportDeclared reports every function as declaring its name, for a listener of either walk.
	reportDeclared := func(context *rule.Context, position token.Pos, name string) {
		context.Report(analysis.Diagnostic{Pos: position, Message: "declares " + name})
	}
	// readsItsRequirement panics, which the dispatcher makes the rule's crash, unless the listener was handed the
	// result it requires.
	readsItsRequirement := func(context *rule.Context) {
		if result, isResult := context.ResultOf(failsOnBase).(string); !isResult || result == "" {
			panic(fmt.Sprintf("the listener was handed %v for %s", result, failsOnBase.Name))
		}
	}
	cases := []struct {
		name string
		// register registers a listener that requires failsOnBase and one that does not, each reporting every
		// function it is called for, on a fresh dispatcher of the walk the case is for, and returns their rules.
		register func(failingName string, besideName string) (rule.Rule, rule.Rule)
	}{
		{"syntax", func(failingName string, besideName string) (rule.Rule, rule.Rule) {
			dispatcher := dispatch.NewSyntax()
			listener := func(requires []*analysis.Analyzer) *rule.Listener {
				return &rule.Listener{
					Requires: requires,
					Nodes:    []ast.Node{(*ast.FuncDecl)(nil)},
					Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
						declaration, isDeclaration := node.(*ast.FuncDecl)
						if !isDeclaration {
							return
						}
						if len(requires) > 0 {
							readsItsRequirement(context)
						}
						reportDeclared(context, declaration.Name.Pos(), declaration.Name.Name)
					},
				}
			}
			failingListener, besideListener := listener([]*analysis.Analyzer{failsOnBase}), listener(nil)
			return rule.Rule{Name: failingName, Origin: rule.OriginHouse, Listener: failingListener,
					Analyzer: dispatcher.RuleAnalyzer(failingName, "failing_syntax", "requires fails_on_base",
						failingListener)},
				rule.Rule{Name: besideName, Origin: rule.OriginHouse, Listener: besideListener,
					Analyzer: dispatcher.RuleAnalyzer(besideName, "beside_syntax", "requires nothing",
						besideListener)}
		}},
		{"intermediate representation", func(failingName string, besideName string) (rule.Rule, rule.Rule) {
			dispatcher := dispatch.NewIr()
			listener := func(requires []*analysis.Analyzer) *rule.Listener {
				return &rule.Listener{
					Requires: requires,
					Function: func(context *rule.Context, function *ir.Function) {
						// The package initializer is synthetic and declared by no one.
						if function.Synthetic != "" {
							return
						}
						if len(requires) > 0 {
							readsItsRequirement(context)
						}
						reportDeclared(context, function.Pos(), function.Name())
					},
				}
			}
			failingListener, besideListener := listener([]*analysis.Analyzer{failsOnBase}), listener(nil)
			return rule.Rule{Name: failingName, Origin: rule.OriginHouse, Listener: failingListener,
					Analyzer: dispatcher.RuleAnalyzer(failingName, "failing_ir", "requires fails_on_base",
						failingListener)},
				rule.Rule{Name: besideName, Origin: rule.OriginHouse, Listener: besideListener,
					Analyzer: dispatcher.RuleAnalyzer(besideName, "beside_ir", "requires nothing", besideListener)}
		}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			failing, beside := testCase.register(rule.Namespace+"test-failing-requirement",
				rule.Namespace+"test-beside-the-failing")
			dispatched := runRules(subtest, Options{}, failing, beside)
			// The same rule walking by itself: an analyzer of its own that requires the failing one.
			alone := runRules(subtest, Options{}, rule.Rule{Name: failing.Name, Origin: rule.OriginHouse,
				Analyzer: &analysis.Analyzer{
					Name:     "failing_alone_" + strings.ReplaceAll(testCase.name, " ", "_"),
					Doc:      "requires fails_on_base and reports nothing",
					Requires: []*analysis.Analyzer{failsOnBase},
					Run:      func(*analysis.Pass) (any, error) { return nil, nil },
				}})
			if len(alone.Crashes) != 1 || !strings.Contains(alone.Crashes[0].Error, "planted failure on") {
				subtest.Fatalf("the rule alone crashed %+v, where one crash naming the planted failure was expected",
					alone.Crashes)
			}
			if !slices.Equal(dispatched.Crashes, alone.Crashes) {
				subtest.Errorf("the dispatched rules crashed\n%+v\nwhere exactly what the rule alone crashed was "+
					"expected,\n%+v", dispatched.Crashes, alone.Crashes)
			}
			if !slices.Equal(dispatched.CrashedRules, []string{failing.Name}) {
				subtest.Errorf("the run lists %q as crashed, where %s alone was expected", dispatched.CrashedRules,
					failing.Name)
			}
			messages := []string{}
			for _, finding := range dispatched.Findings {
				messages = append(messages, finding.Rule+": "+finding.Message)
			}
			slices.Sort(messages)
			expected := []string{beside.Name + ": declares Marked", beside.Name + ": declares Target",
				beside.Name + ": declares Use", failing.Name + ": declares Use"}
			slices.Sort(expected)
			if !slices.Equal(messages, expected) {
				subtest.Errorf("the run found %q, where exactly %q was expected", messages, expected)
			}
		})
	}
}
