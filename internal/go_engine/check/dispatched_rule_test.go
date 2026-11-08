package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/deprecated"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/load"
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

// plantedRuns is an analyzer that learns facts, so the checker runs it on every package below one it runs on, and
// counts its runs by package path, standing in for staticcheck's purity, which only the listeners of SA4017 and its
// kin require.
type plantedRuns struct {
	mutex    sync.Mutex
	runs     map[string]int
	analyzer *analysis.Analyzer
}

// newPlantedRuns is a counting analyzer named name, whose result names the package it ran on.
func newPlantedRuns(name string) *plantedRuns {
	planted := &plantedRuns{runs: map[string]int{}}
	planted.analyzer = &analysis.Analyzer{
		Name:       name,
		Doc:        name + " counts its runs on each package",
		FactTypes:  []analysis.Fact{new(plantedFact)},
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			planted.mutex.Lock()
			defer planted.mutex.Unlock()
			planted.runs[pass.Pkg.Path()]++
			return name + " on " + pass.Pkg.Path(), nil
		},
	}
	return planted
}

// An analyzer that only the listener of a rule the run leaves out requires never runs, on either dispatcher, on any
// package, though that listener registered on the very dispatcher the run's enabled rule requires and the dispatcher
// requires what each of its listeners does: the run's rule beside it still reports every function. The same analyzer,
// required also by an enabled rule's listener, runs once on every package, and that listener reads its result there,
// as a dispatcher handed only some of what its listeners require would not let it.
func TestAnAnalyzerOnlyALeftOutListenerRequiresNeverRuns(test *testing.T) {
	test.Parallel()

	// reportDeclared reports every function as declaring its name, for a listener of either walk.
	reportDeclared := func(context *rule.Context, position token.Pos, name string) {
		context.Report(analysis.Diagnostic{Pos: position, Message: "declares " + name})
	}
	// reportRead reports the result of planted the listener was handed at the package clause of the package's first
	// file, which the run keeps once per package however often it is called there, and panics, which the dispatcher
	// makes the rule's crash, when it was handed none.
	reportRead := func(context *rule.Context, planted *analysis.Analyzer) {
		result, isResult := context.ResultOf(planted).(string)
		if !isResult || result == "" {
			panic(fmt.Sprintf("the listener was handed %v for %s", context.ResultOf(planted), planted.Name))
		}
		context.Report(analysis.Diagnostic{Pos: context.Files[0].Package, Message: "read " + result})
	}
	walks := []struct {
		name string
		// register registers on a fresh dispatcher of the walk the case is for a listener that requires planted
		// whose rule the run leaves out, a listener that requires nothing and reports every function, and one that
		// requires planted and reports reading its result, and returns the last two's rules.
		register func(planted *analysis.Analyzer) (declaring rule.Rule, reading rule.Rule)
	}{
		{"syntax", func(planted *analysis.Analyzer) (rule.Rule, rule.Rule) {
			dispatcher := dispatch.NewSyntax()
			dispatcher.RuleAnalyzer(rule.Namespace+"test-planted-left-out", "planted_left_out_syntax",
				"requires the planted analyzer", &rule.Listener{
					Requires: []*analysis.Analyzer{planted},
					Nodes:    []ast.Node{(*ast.FuncDecl)(nil)},
					Node:     func(context *rule.Context, _ ast.Node, _ []ast.Node) { reportRead(context, planted) },
				})
			declaring := &rule.Listener{
				Nodes: []ast.Node{(*ast.FuncDecl)(nil)},
				Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
					if declaration, isDeclaration := node.(*ast.FuncDecl); isDeclaration {
						reportDeclared(context, declaration.Name.Pos(), declaration.Name.Name)
					}
				},
			}
			reading := &rule.Listener{
				Requires:  []*analysis.Analyzer{planted},
				FileEnter: func(context *rule.Context, _ *ast.File) { reportRead(context, planted) },
			}
			declaringName, readingName := rule.Namespace+"test-planted-declaring", rule.Namespace+"test-planted-reading"
			return rule.Rule{Name: declaringName, Origin: rule.OriginHouse, Listener: declaring,
					Analyzer: dispatcher.RuleAnalyzer(declaringName, "planted_declaring_syntax",
						"reports every declaration", declaring)},
				rule.Rule{Name: readingName, Origin: rule.OriginHouse, Listener: reading,
					Analyzer: dispatcher.RuleAnalyzer(readingName, "planted_reading_syntax",
						"reports reading the planted analyzer", reading)}
		}},
		{"intermediate representation", func(planted *analysis.Analyzer) (rule.Rule, rule.Rule) {
			dispatcher := dispatch.NewIr()
			dispatcher.RuleAnalyzer(rule.Namespace+"test-planted-left-out", "planted_left_out_ir",
				"requires the planted analyzer", &rule.Listener{
					Requires: []*analysis.Analyzer{planted},
					Function: func(context *rule.Context, _ *ir.Function) { reportRead(context, planted) },
				})
			declaring := &rule.Listener{
				Function: func(context *rule.Context, function *ir.Function) {
					// The package initializer is synthetic and declared by no one.
					if function.Synthetic == "" {
						reportDeclared(context, function.Pos(), function.Name())
					}
				},
			}
			reading := &rule.Listener{
				Requires: []*analysis.Analyzer{planted},
				Function: func(context *rule.Context, _ *ir.Function) { reportRead(context, planted) },
			}
			declaringName, readingName := rule.Namespace+"test-planted-declaring", rule.Namespace+"test-planted-reading"
			return rule.Rule{Name: declaringName, Origin: rule.OriginHouse, Listener: declaring,
					Analyzer: dispatcher.RuleAnalyzer(declaringName, "planted_declaring_ir",
						"reports every source function", declaring)},
				rule.Rule{Name: readingName, Origin: rule.OriginHouse, Listener: reading,
					Analyzer: dispatcher.RuleAnalyzer(readingName, "planted_reading_ir",
						"reports reading the planted analyzer", reading)}
		}},
	}
	const base = "example.com/fixture/base"
	const user = "example.com/fixture/user"
	for _, walk := range walks {
		for _, readerEnabled := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s, the reader enabled %t", walk.name, readerEnabled), func(subtest *testing.T) {
				subtest.Parallel()
				planted := newPlantedRuns("planted_" + strings.ReplaceAll(walk.name, " ", "_"))
				declaring, reading := walk.register(planted.analyzer)
				enabled := []rule.Rule{declaring}
				expectedRuns := map[string]int{}
				expected := []string{declaring.Name + ": declares Marked", declaring.Name + ": declares Target",
					declaring.Name + ": declares Use"}
				if readerEnabled {
					enabled = append(enabled, reading)
					expectedRuns = map[string]int{base: 1, user: 1}
					expected = append(expected, reading.Name+": read "+planted.analyzer.Name+" on "+base,
						reading.Name+": read "+planted.analyzer.Name+" on "+user)
				}
				result := runRules(subtest, Options{}, enabled...)
				planted.mutex.Lock()
				defer planted.mutex.Unlock()
				if !maps.Equal(planted.runs, expectedRuns) {
					subtest.Errorf("the planted analyzer ran %v, where %v was expected", planted.runs, expectedRuns)
				}
				messages := []string{}
				for _, finding := range result.Findings {
					messages = append(messages, finding.Rule+": "+finding.Message)
				}
				slices.Sort(messages)
				slices.Sort(expected)
				if !slices.Equal(messages, expected) || len(result.Crashes) != 0 {
					subtest.Errorf("the run found %q and crashed %+v, where exactly %q and no crash were expected",
						messages, result.Crashes, expected)
				}
			})
		}
	}
}

// The cache asks an entry for the facts of exactly the fact analyzers the run's graph runs: not one only a left-out
// rule's listener requires, which no run of these rules ever runs, so an entry that holds none of its facts covers
// what the run asks, and the same analyzer once an enabled rule's listener requires it too.
func TestTheCacheAsksNoFactsOfAnAnalyzerOnlyALeftOutListenerRequires(test *testing.T) {
	test.Parallel()

	planted := newPlantedRuns("planted_cache")
	dispatcher := dispatch.NewSyntax()
	// heard is every listener's FileEnter: newCacheAnalyzers reads only the analyzers, and no rule here ever runs.
	heard := func(*rule.Context, *ast.File) {
		panic("a listener of the cache's test ran, where only its analyzers are read")
	}
	listener := func() *rule.Listener {
		return &rule.Listener{Requires: []*analysis.Analyzer{planted.analyzer}, FileEnter: heard}
	}
	dispatcher.RuleAnalyzer(rule.Namespace+"test-cache-left-out", "cache_left_out", "requires the planted analyzer",
		listener())
	plain := &rule.Listener{FileEnter: heard}
	plainAnalyzer := dispatcher.RuleAnalyzer(rule.Namespace+"test-cache-plain", "cache_plain", "requires nothing",
		plain)
	readingAnalyzer := dispatcher.RuleAnalyzer(rule.Namespace+"test-cache-reading", "cache_reading",
		"requires the planted analyzer", listener())
	for _, configuration := range []struct {
		name      string
		analyzers []*analysis.Analyzer
		asked     bool
	}{
		{"the reader left out", []*analysis.Analyzer{plainAnalyzer}, false},
		{"the reader enabled", []*analysis.Analyzer{plainAnalyzer, readingAnalyzer}, true},
	} {
		ruleOf := map[*analysis.Analyzer]rule.Rule{}
		for _, analyzer := range configuration.analyzers {
			ruleOf[analyzer] = rule.Rule{Name: rule.Namespace + "test-" + analyzer.Name, Analyzer: analyzer}
		}
		known, knownError := newCacheAnalyzers(ruleOf, configuration.analyzers)
		if knownError != nil {
			test.Fatalf("%s: reading the analyzers: %v", configuration.name, knownError)
		}
		if asked := slices.Contains(known.facts, planted.analyzer); asked != configuration.asked {
			test.Errorf("%s: the cache asks for the planted analyzer's facts %t, where %t was expected",
				configuration.name, asked, configuration.asked)
		}
	}
}

// A syntax listener's context hands it the cursor of exactly the node it is called on, through check.Run: its Node is
// that node, its parent and every ancestor through it are the stack the listener is handed, and the field of its
// parent that holds it and its siblings on either side are what the syntax says, read off the files the run walked.
// Each function and the identifier naming it reports what its cursor answers, and a cursor that is another node's
// reports that instead, as does a cursor of the node visited before, which for the first node is no cursor at all and
// the rule's crash.
func TestASyntaxListenersCursorIsTheNodeItIsHanded(test *testing.T) {
	test.Parallel()

	dispatcher := dispatch.NewSyntax()
	listener := &rule.Listener{
		Nodes: []ast.Node{(*ast.FuncDecl)(nil), (*ast.Ident)(nil)},
		Node: func(context *rule.Context, node ast.Node, stack []ast.Node) {
			cursor := context.Cursor()
			enclosing := []ast.Node{}
			if cursor.Node() == node {
				for ancestor := range cursor.Parent().Enclosing() {
					enclosing = append(enclosing, ancestor.Node())
				}
				slices.Reverse(enclosing)
			}
			if cursor.Node() != node || !slices.Equal(enclosing, stack) {
				context.Report(analysis.Diagnostic{Pos: node.Pos(), Message: fmt.Sprintf("%s is handed the cursor "+
					"of %s, with %d ancestors where its stack holds %d", nodeAt(context.Fset, node),
					nodeAt(context.Fset, cursor.Node()), len(enclosing), len(stack))})
				return
			}
			var name string
			switch typed := node.(type) {
			case *ast.FuncDecl:
				name = typed.Name.Name
			case *ast.Ident:
				declaration, isDeclaration := stack[len(stack)-1].(*ast.FuncDecl)
				if !isDeclaration || declaration.Name != typed {
					return
				}
				name = typed.Name
			}
			kind, index := cursor.ParentEdge()
			before, after := "nothing", "nothing"
			if sibling, found := cursor.PrevSibling(); found {
				before = nodeAt(context.Fset, sibling.Node())
			}
			if sibling, found := cursor.NextSibling(); found {
				after = nodeAt(context.Fset, sibling.Node())
			}
			context.Report(analysis.Diagnostic{Pos: node.Pos(), Message: fmt.Sprintf("%T %s is %s[%d], after %s "+
				"and before %s", node, name, kind, index, before, after)})
		},
	}
	reading := rule.Rule{Name: rule.Namespace + "test-reads-its-cursor", Origin: rule.OriginHouse, Listener: listener,
		Analyzer: dispatcher.RuleAnalyzer(rule.Namespace+"test-reads-its-cursor", "reads_its_cursor",
			"reports what each function's cursor answers", listener)}
	result := runRules(test, Options{}, reading)
	if len(result.Crashes) != 0 {
		test.Fatalf("the run crashed %+v, where the listener reads its cursor in every Node call", result.Crashes)
	}

	expected := []string{}
	for _, analyzedPackage := range fixtures.loaded.Packages {
		fileSet := analyzedPackage.Fset
		for _, file := range analyzedPackage.Syntax {
			for index, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction {
					continue
				}
				// The file's children are its doc comment, its package name and its declarations, in that order.
				var before ast.Node = file.Name
				if index > 0 {
					before = file.Decls[index-1]
				}
				after := "nothing"
				if index+1 < len(file.Decls) {
					after = nodeAt(fileSet, file.Decls[index+1])
				}
				expected = append(expected, fmt.Sprintf("*ast.FuncDecl %s is File.Decls[%d], after %s and before %s",
					function.Name.Name, index, nodeAt(fileSet, before), after))
				// A function's children are its doc comment, its receiver, its name, its type and its body.
				nameBefore := "nothing"
				if function.Doc != nil {
					nameBefore = nodeAt(fileSet, function.Doc)
				}
				expected = append(expected, fmt.Sprintf("*ast.Ident %s is FuncDecl.Name[-1], after %s and before %s",
					function.Name.Name, nameBefore, nodeAt(fileSet, function.Type)))
			}
		}
	}
	messages := []string{}
	for _, finding := range result.Findings {
		messages = append(messages, finding.Message)
	}
	slices.Sort(messages)
	slices.Sort(expected)
	if len(expected) != 6 || !slices.Equal(messages, expected) {
		test.Errorf("the cursors answered\n%s\nwhere the syntax says\n%s", strings.Join(messages, "\n"),
			strings.Join(expected, "\n"))
	}
}

// nodeAt is node's kind and where it begins, line and column, or nothing for no node.
func nodeAt(fileSet *token.FileSet, node ast.Node) string {
	if node == nil {
		return "no node"
	}
	position := fileSet.Position(node.Pos())
	return fmt.Sprintf("%T at %d:%d", node, position.Line, position.Column)
}

// Reading the cursor anywhere but a Node call is the rule's crash on each package, naming the misuse: in FileEnter,
// before any node, and in PackageExit, after every Node call has returned and taken its cursor back. The rule beside
// it on the same dispatcher, reading its cursor only in Node, reports every function.
func TestTheCursorOutsideANodeCallIsTheRulesCrash(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name string
		// listener is a listener whose Node reads its cursor, and which reads it once more outside Node.
		listener func() *rule.Listener
	}{
		{"FileEnter", func() *rule.Listener {
			return &rule.Listener{
				FileEnter: func(context *rule.Context, _ *ast.File) { context.Cursor() },
				Nodes:     []ast.Node{(*ast.FuncDecl)(nil)},
				Node:      func(context *rule.Context, _ ast.Node, _ []ast.Node) { context.Cursor() },
			}
		}},
		{"PackageExit", func() *rule.Listener {
			return &rule.Listener{
				Nodes:       []ast.Node{(*ast.FuncDecl)(nil)},
				Node:        func(context *rule.Context, _ ast.Node, _ []ast.Node) { context.Cursor() },
				PackageExit: func(context *rule.Context) { context.Cursor() },
			}
		}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			dispatcher := dispatch.NewSyntax()
			misreadingName := rule.Namespace + "test-misreads-its-cursor"
			misreadingListener := testCase.listener()
			misreading := rule.Rule{Name: misreadingName, Origin: rule.OriginHouse, Listener: misreadingListener,
				Analyzer: dispatcher.RuleAnalyzer(misreadingName, "misreads_its_cursor", "reads it outside Node",
					misreadingListener)}
			besideListener := &rule.Listener{
				Nodes: []ast.Node{(*ast.FuncDecl)(nil)},
				Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
					declaration, isDeclaration := context.Cursor().Node().(*ast.FuncDecl)
					if !isDeclaration || declaration != node {
						return
					}
					context.Report(analysis.Diagnostic{Pos: declaration.Name.Pos(), Message: "declares " +
						declaration.Name.Name})
				},
			}
			besideName := rule.Namespace + "test-beside-the-misreading"
			beside := rule.Rule{Name: besideName, Origin: rule.OriginHouse, Listener: besideListener,
				Analyzer: dispatcher.RuleAnalyzer(besideName, "beside_the_misreading", "reports every function",
					besideListener)}
			result := runRules(subtest, Options{}, misreading, beside)
			if len(result.Crashes) != 2 || !slices.Equal(result.CrashedRules, []string{misreadingName}) {
				subtest.Fatalf("the run crashed %q with %+v, where %s alone was expected to crash, once on each "+
					"package", result.CrashedRules, result.Crashes, misreadingName)
			}
			for _, crash := range result.Crashes {
				if !strings.Contains(crash.Error, "running analyzer misreads_its_cursor on ") ||
					!strings.Contains(crash.Error, "rule "+misreadingName+" reads the cursor outside a Node call") {
					subtest.Errorf("the crash reads %q, where one naming the rule's cursor read outside Node was "+
						"expected", crash.Error)
				}
			}
			messages := []string{}
			for _, finding := range result.Findings {
				messages = append(messages, finding.Rule+": "+finding.Message)
			}
			slices.Sort(messages)
			expected := []string{besideName + ": declares Marked", besideName + ": declares Target",
				besideName + ": declares Use"}
			if !slices.Equal(messages, expected) {
				subtest.Errorf("the run found %q, where exactly %q was expected", messages, expected)
			}
		})
	}
}

// skippedForErrorsFiles are a module of its own, so the shared fixtures' counts stay as they are: broken holds a type
// error, so an analyzer that does not run despite errors is skipped there, and fine beside it is well typed.
var skippedForErrorsFiles = map[string]string{
	"go.mod": "module example.com/skipped\n\ngo 1.24\n",
	"broken/broken.go": "package broken\n\n// Broken returns a string as an int.\n" +
		"func Broken() int { return \"no\" }\n",
	"fine/fine.go": "package fine\n\n// Fine is well typed.\nfunc Fine() int { return 1 }\n",
}

// A listener requiring an analyzer that does not run despite errors, staticcheck's deprecated facts as SA1019's
// does, fails only its own rule on a package with errors: the checker skips the requirement there, the syntax
// dispatcher, which runs despite errors and runs past a requirement that did not finish (rule.Failed), starts every
// other listener, and each rule's crash reads exactly as check.Run writes it for the same rule as a plain analyzer:
// the requiring one's with "failed prerequisites" naming the deprecated facts, its twin requiring them itself as the
// original SA1019 does, and the one beside it skipped for the errors, as any rule that does not run despite errors
// is. On the well-typed package beside it both report.
func TestAListenersRequirementSkippedForErrorsFailsOnlyItsOwnRule(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, skippedForErrorsFiles)
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	illTyped := 0
	for _, loadedPackage := range loaded.Packages {
		if loadedPackage.IllTyped {
			illTyped++
		}
	}
	if illTyped != 1 {
		test.Fatalf("want broken alone ill-typed, got %d ill-typed packages of %d", illTyped, len(loaded.Packages))
	}

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
					if _, isResult := context.ResultOf(deprecated.Analyzer).(deprecated.Result); !isResult {
						panic(fmt.Sprintf("the listener was handed no result for %s", deprecated.Analyzer.Name))
					}
				}
				context.Report(analysis.Diagnostic{Pos: declaration.Name.Pos(), Message: "declares " +
					declaration.Name.Name})
			},
		}
	}
	requiringName := rule.Namespace + "test-requires-deprecated"
	besideName := rule.Namespace + "test-beside-the-skipped"
	requiringListener, besideListener := listener([]*analysis.Analyzer{deprecated.Analyzer}), listener(nil)
	dispatched, runError := Run(loaded.Packages, []rule.Rule{
		{Name: requiringName, Origin: rule.OriginHouse, Listener: requiringListener,
			Analyzer: dispatcher.RuleAnalyzer(requiringName, "requires_deprecated", "requires fact_deprecated",
				requiringListener)},
		{Name: besideName, Origin: rule.OriginHouse, Listener: besideListener,
			Analyzer: dispatcher.RuleAnalyzer(besideName, "beside_the_skipped", "requires nothing", besideListener)},
	}, Options{})
	if runError != nil {
		test.Fatalf("running the dispatched rules: %v", runError)
	}
	// The same two rules walking by themselves: one requiring the deprecated facts, one requiring nothing.
	alone, aloneError := Run(loaded.Packages, []rule.Rule{
		{Name: requiringName, Origin: rule.OriginHouse, Analyzer: &analysis.Analyzer{
			Name:     "requires_deprecated_alone",
			Doc:      "requires fact_deprecated and reports nothing",
			Requires: []*analysis.Analyzer{deprecated.Analyzer},
			Run:      func(*analysis.Pass) (any, error) { return nil, nil },
		}},
		{Name: besideName, Origin: rule.OriginHouse, Analyzer: &analysis.Analyzer{
			Name: "beside_the_skipped_alone",
			Doc:  "requires nothing and reports nothing",
			Run:  func(*analysis.Pass) (any, error) { return nil, nil },
		}},
	}, Options{})
	if aloneError != nil {
		test.Fatalf("running the rules alone: %v", aloneError)
	}
	if len(alone.Crashes) != 2 || !strings.Contains(alone.Crashes[0].Error+alone.Crashes[1].Error,
		"failed prerequisites: "+deprecated.Analyzer.Name+"@example.com/skipped/broken") {
		test.Fatalf("the rules alone crashed %+v, where two crashes on broken, one naming %s as a failed "+
			"prerequisite, were expected", alone.Crashes, deprecated.Analyzer.Name)
	}
	if !slices.Equal(dispatched.Crashes, alone.Crashes) {
		test.Errorf("the dispatched rules crashed\n%+v\nwhere exactly what the rules alone crashed was expected,\n%+v",
			dispatched.Crashes, alone.Crashes)
	}
	messages := []string{}
	for _, finding := range dispatched.Findings {
		messages = append(messages, finding.Rule+": "+finding.Message)
	}
	slices.Sort(messages)
	expected := []string{besideName + ": declares Fine", requiringName + ": declares Fine"}
	slices.Sort(expected)
	if !slices.Equal(messages, expected) {
		test.Errorf("the run found %q, where exactly %q was expected", messages, expected)
	}
}
