package dispatch

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/rule"
)

// heardNodes is what the standing tests' recording listeners heard over a package, each under its own name, from
// every run of it at once: each file entered and left and each node, how many times its PackageExit was called, and
// the contexts it was called with, one per run.
type heardNodes struct {
	lock     sync.Mutex
	heard    map[string][]string
	packages map[string]int
	contexts map[string]map[*rule.Context]bool
}

// newHeardNodes is a heardNodes that has heard nothing.
func newHeardNodes() *heardNodes {
	return &heardNodes{heard: map[string][]string{}, packages: map[string]int{},
		contexts: map[string]map[*rule.Context]bool{}}
}

// placedAt is position as the standing tests read one: its file's base name, its line and its column.
func placedAt(fileSet *token.FileSet, position token.Pos) string {
	placed := fileSet.PositionFor(position, false)
	return fmt.Sprintf("%s:%d:%d", filepath.Base(placed.Filename), placed.Line, placed.Column)
}

// isHeardKind is whether node is of a kind the recording listeners list, the kinds a function's header, its doc
// comment and its body are made of.
func isHeardKind(node ast.Node) bool {
	switch node.(type) {
	case *ast.FuncDecl, *ast.FuncLit, *ast.IfStmt, *ast.Ident, *ast.BlockStmt, *ast.FieldList, *ast.CommentGroup:
		return true
	}
	return false
}

// listen is a file-local listener that records, under name, each file it enters and leaves, each node of a heard kind
// with its place, and each call of its PackageExit.
func (heard *heardNodes) listen(name string) *rule.Listener {
	record := func(context *rule.Context, what string) {
		heard.lock.Lock()
		defer heard.lock.Unlock()
		heard.heard[name] = append(heard.heard[name], what)
		if heard.contexts[name] == nil {
			heard.contexts[name] = map[*rule.Context]bool{}
		}
		heard.contexts[name][context] = true
	}
	return &rule.Listener{
		FileLocal: true,
		FileEnter: func(context *rule.Context, file *ast.File) {
			record(context, "enter "+placedAt(context.FileSet, file.Pos()))
		},
		Nodes: []ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil), (*ast.IfStmt)(nil), (*ast.Ident)(nil),
			(*ast.BlockStmt)(nil), (*ast.FieldList)(nil), (*ast.CommentGroup)(nil)},
		Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
			record(context, fmt.Sprintf("%T %s", node, placedAt(context.FileSet, node.Pos())))
		},
		FileExit: func(context *rule.Context, file *ast.File) {
			record(context, "exit "+placedAt(context.FileSet, file.Pos()))
		},
		PackageExit: func(*rule.Context) {
			heard.lock.Lock()
			defer heard.lock.Unlock()
			heard.packages[name]++
		},
	}
}

// expectedHeard is what a recording listener hears over files, sorted: everything, and what one hears that is deaf
// inside the bodies of the functions named in standing, from each one's opening brace to its end. It reads the very
// files the pass holds with ast.Inspect, and finds each standing function by its name.
func expectedHeard(files *probedFiles, standing []string) (everything []string, outsideBodies []string) {
	for _, file := range files.files {
		bodies := [][2]token.Pos{}
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction &&
				slices.Contains(standing, function.Name.Name) {
				bodies = append(bodies, [2]token.Pos{function.Body.Lbrace, function.End()})
			}
		}
		for _, edge := range []string{"enter ", "exit "} {
			everything = append(everything, edge+placedAt(files.fileSet, file.Pos()))
			outsideBodies = append(outsideBodies, edge+placedAt(files.fileSet, file.Pos()))
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil || !isHeardKind(node) {
				return true
			}
			line := fmt.Sprintf("%T %s", node, placedAt(files.fileSet, node.Pos()))
			everything = append(everything, line)
			if !slices.ContainsFunc(bodies, func(body [2]token.Pos) bool {
				return body[0] <= node.Pos() && node.Pos() <= body[1]
			}) {
				outsideBodies = append(outsideBodies, line)
			}
			return true
		})
	}
	slices.Sort(everything)
	slices.Sort(outsideBodies)
	return everything, outsideBodies
}

// A listener whose rule is confined, on a package where three functions stand, hears each standing function's header,
// its doc comment, name, parameters and results, and nothing from its body's opening brace on, a function literal
// inside the body included, while it hears every other node, the function literal in a variable's initializer among
// them, and every file's FileEnter and FileExit and its PackageExit. A listener beside it whose rule is not confined
// hears everything. Each holds whether the package is walked whole or split into shards.
func TestAConfinedListenerHearsAStandingFunctionsHeaderAndNothingInItsBody(test *testing.T) {
	test.Parallel()
	for _, shardBytes := range []int{defaultShardBytes, 1} {
		test.Run(fmt.Sprintf("shards of %d bytes", shardBytes), func(test *testing.T) {
			test.Parallel()
			if shardBytes == 1 {
				requireSplitting(test)
			}
			dispatcher := NewSyntax()
			dispatcher.shardBytes = shardBytes
			heard := newHeardNodes()
			confinedName := rule.Namespace + "confined"
			confined := dispatcher.RuleAnalyzer(confinedName, "confined", "records every call",
				heard.listen("confined"))
			free := dispatcher.RuleAnalyzer(rule.Namespace+"free", "free", "records every call", heard.listen("free"))
			probe, files := filesProbe()
			standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), confinedName)
			for _, action := range standing.analyze(test, confined, free, probe).Roots {
				if action.Err != nil {
					test.Fatalf("running %s: %v", action, action.Err)
				}
			}
			if shardBytes == 1 && len(heard.contexts["confined"]) < 2 {
				test.Fatalf("the confined listener ran in %d context, so the package did not split",
					len(heard.contexts["confined"]))
			}

			everything, outsideBodies := expectedHeard(files, []string{"stood", "stoodToo", "alsoStood"})
			heardConfined := slices.Sorted(slices.Values(heard.heard["confined"]))
			heardFree := slices.Sorted(slices.Values(heard.heard["free"]))
			if !slices.Equal(heardConfined, outsideBodies) {
				test.Errorf("the confined listener heard\n%s\nwant everything outside the standing bodies\n%s",
					strings.Join(heardConfined, "\n"), strings.Join(outsideBodies, "\n"))
			}
			if !slices.Equal(heardFree, everything) {
				test.Errorf("the listener that is not confined heard\n%s\nwant everything\n%s",
					strings.Join(heardFree, "\n"), strings.Join(everything, "\n"))
			}
			// The expectation above finds a standing body as the walk does, so each claim is also held to the fixture's
			// own lines: stood's doc comment, declaration, name, parameters and results, the function literal in kept's
			// initializer with its if, and the if in reached.
			for _, outside := range []string{
				"*ast.CommentGroup first.go:13:1", "*ast.FuncDecl first.go:14:1", "*ast.Ident first.go:14:6",
				"*ast.FieldList first.go:14:11", "*ast.FieldList first.go:14:23", "*ast.FuncLit first.go:6:12",
				"*ast.IfStmt first.go:7:2", "*ast.IfStmt first.go:28:2", "*ast.FuncDecl second.go:3:1",
			} {
				if !slices.Contains(heardConfined, outside) {
					test.Errorf("the confined listener did not hear %s, which is outside every standing body", outside)
				}
			}
			// stood's body, the if in it, the function literal in it and that literal's if, and the if in alsoStood.
			for _, inside := range []string{
				"*ast.BlockStmt first.go:14:27", "*ast.IfStmt first.go:15:2", "*ast.FuncLit first.go:18:11",
				"*ast.IfStmt first.go:19:3", "*ast.IfStmt first.go:35:2", "*ast.IfStmt second.go:4:2",
			} {
				if slices.Contains(heardConfined, inside) || !slices.Contains(heardFree, inside) {
					test.Errorf("%s is inside a standing body, which the confined listener must not hear (heard: %t) "+
						"and the other must (heard: %t)", inside, slices.Contains(heardConfined, inside),
						slices.Contains(heardFree, inside))
				}
			}
			if heard.packages["confined"] == 0 || heard.packages["free"] == 0 {
				test.Errorf("PackageExit was called %d times on the confined listener and %d on the other, want both "+
					"called", heard.packages["confined"], heard.packages["free"])
			}
		})
	}
}

// namingListener is a file-local listener that reports each function declaration at its name, which it reads off the
// declaration's header, and each if it hears.
func namingListener() *rule.Listener {
	return &rule.Listener{
		FileLocal: true,
		Nodes:     []ast.Node{(*ast.FuncDecl)(nil), (*ast.IfStmt)(nil)},
		Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
			if function, isFunction := node.(*ast.FuncDecl); isFunction {
				context.Reportf(function.Name.Pos(), "its own name %s", function.Name.Name)
				return
			}
			context.Reportf(node.Pos(), "its own if")
		},
	}
}

// closingListener is a file-local listener that hears no node and reports, as it leaves a file, each function
// declaration's closing brace, a place inside the function's body.
func closingListener() *rule.Listener {
	return &rule.Listener{
		FileLocal: true,
		FileExit: func(context *rule.Context, file *ast.File) {
			for _, declaration := range file.Decls {
				if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Body != nil {
					context.Reportf(function.Body.Rbrace, "its own closing of %s", function.Name.Name)
				}
			}
		},
	}
}

// reportedBy is each finding action reported, in order, as diagnosticPlaced reads one.
func reportedBy(action *checker.Action) []string {
	reported := []string{}
	for _, diagnostic := range action.Diagnostics {
		reported = append(reported, diagnosticPlaced(action, diagnostic))
	}
	return reported
}

// A confined rule's own finding inside a standing function is dropped at its action, whether its listener made it
// from the function's header, at its name, or from FileExit, at a place inside the body, and the host's findings
// inside the function, from its doc comment to its end, are reported as the rule's own, after its own, at the lines,
// columns, ends and fix offsets the host's name. The host's findings outside every standing function are not
// reported, since the rule judged those places itself, and its own findings there are kept. The rule tells the build,
// for each file, how many functions stood and how many of the host's findings it reported in them. Each holds whether
// the package is walked whole or split into shards.
func TestAConfinedRuleReportsTheHostsFindingsInAStandingFunctionAndItsOwnOutside(test *testing.T) {
	test.Parallel()
	for _, shardBytes := range []int{defaultShardBytes, 1} {
		test.Run(fmt.Sprintf("shards of %d bytes", shardBytes), func(test *testing.T) {
			test.Parallel()
			if shardBytes == 1 {
				requireSplitting(test)
			}
			dispatcher := NewSyntax()
			dispatcher.shardBytes = shardBytes
			namingName, closingName := rule.Namespace+"naming", rule.Namespace+"closing"
			naming := dispatcher.RuleAnalyzer(namingName, "naming", "reports each name and if", namingListener())
			closing := dispatcher.RuleAnalyzer(closingName, "closing", "reports each closing brace", closingListener())
			standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), namingName,
				closingName)
			ownOutside := map[*analysis.Analyzer][]string{
				naming: {
					"first.go:7:2 its own if",
					"first.go:27:6 its own name reached",
					"first.go:28:2 its own if",
					"second.go:10:6 its own name alsoReached",
					"second.go:11:2 its own if",
				},
				closing: {
					"first.go:32:1 its own closing of reached",
					"second.go:15:1 its own closing of alsoReached",
				},
			}
			for _, action := range standing.analyze(test, naming, closing).Roots {
				if action.Err != nil {
					test.Fatalf("running %s: %v", action, action.Err)
				}
				reported := reportedBy(action)
				expected := slices.Concat(ownOutside[action.Analyzer], hostsFindingsInStanding)
				if !slices.Equal(reported, expected) {
					test.Errorf("%s reported\n%s\nwant its own outside the standing functions, then the host's "+
						"inside them\n%s", action.Analyzer.Name, strings.Join(reported, "\n"),
						strings.Join(expected, "\n"))
				}
			}
			expectedTold := map[string][2]int{
				namingName + " first.go": {2, 3}, namingName + " second.go": {1, 1},
				closingName + " first.go": {2, 3}, closingName + " second.go": {1, 1},
			}
			if told := standing.told(); !maps.Equal(told, expectedTold) {
				test.Errorf("the rules told the build they took %v, want %v, each file's standing functions and the "+
					"host's findings reported in them", told, expectedTold)
			}
		})
	}
}

// doubtedListener is a file-local listener that reports all that namingListener and closingListener report between
// them, so its own findings show what it heard and what was kept of it.
func doubtedListener() *rule.Listener {
	listener := namingListener()
	listener.FileExit = closingListener().FileExit
	return listener
}

// ownInStanding is what doubtedListener reports inside the functions of everyStandingFunction, by file: each one's
// name, the ifs in its body and its closing brace.
var ownInStanding = map[string][]string{
	"first.go": {
		"first.go:14:6 its own name stood", "first.go:15:2 its own if", "first.go:19:3 its own if",
		"first.go:25:1 its own closing of stood", "first.go:34:6 its own name stoodToo", "first.go:35:2 its own if",
		"first.go:39:1 its own closing of stoodToo",
	},
	"second.go": {
		"second.go:3:6 its own name alsoStood", "second.go:4:2 its own if",
		"second.go:8:1 its own closing of alsoStood",
	},
}

// requireLeftToTheRule fails test unless, of each file in fallen, reported holds every finding the rule makes inside
// its standing functions and none of the host's, and told holds nothing for the rule there, and, of each other file of
// the fixture, reported holds none of the rule's own inside its standing functions and told holds the rule's take.
func requireLeftToTheRule(
	test *testing.T,
	ruleName string,
	reported []string,
	told map[string][2]int,
	fallen ...string,
) {
	test.Helper()
	for file, owned := range ownInStanding {
		hosts := slices.ContainsFunc(reported, func(finding string) bool {
			return strings.HasPrefix(finding, file+":") && strings.Contains(finding, "the host's")
		})
		kept := 0
		for _, own := range owned {
			if slices.Contains(reported, own) {
				kept++
			}
		}
		_, took := told[ruleName+" "+file]
		if slices.Contains(fallen, file) {
			if kept != len(owned) || hosts || took {
				test.Errorf("on %s, which the rule could not take from the host, it kept %d of its own %d findings "+
					"inside the standing functions, reported the host's: %t, and told the build it took: %t, want "+
					"every one kept, none of the host's and nothing told; it reported\n%s", file, kept, len(owned),
					hosts, took, strings.Join(reported, "\n"))
			}
			continue
		}
		if kept != 0 || !hosts || !took {
			test.Errorf("on %s, which the rule takes from the host, it kept %d of its own findings inside the "+
				"standing functions, reported the host's: %t, and told the build it took: %t, want none kept, the "+
				"host's reported and the build told; it reported\n%s", file, kept, hosts, took,
				strings.Join(reported, "\n"))
		}
	}
}

// Every doubt lints. A rule that is not confined, a file the host holds no findings of the rule for, a file one of
// whose findings will not place, and a file one of whose findings edits another file, each leave the file to the rule:
// its listener hears the standing bodies there, its own findings inside them are kept, none of the host's is reported
// and the build is told nothing for the file. A doubt about one file leaves the other taken from the host.
func TestEveryDoubtAboutAFilesStandingFunctionsLeavesTheFileToTheRule(test *testing.T) {
	test.Parallel()
	for _, doubt := range []struct {
		name   string
		plant  func(standing *plantedStanding, ruleName string)
		fallen []string
	}{
		{
			name:   "the rule is not confined",
			plant:  func(standing *plantedStanding, ruleName string) { delete(standing.confines, ruleName) },
			fallen: []string{"first.go", "second.go"},
		},
		{
			name:   "the host holds no findings for the file",
			plant:  func(standing *plantedStanding, _ string) { delete(standing.planted, "second.go") },
			fallen: []string{"second.go"},
		},
		{
			name: "a finding of the host's is past the file's end",
			plant: func(standing *plantedStanding, _ string) {
				standing.planted["second.go"] = append(standing.planted["second.go"],
					rule.InheritedFinding{Line: 99, Column: 1, MessageId: "hostPast", Message: "the host's past it"})
			},
			fallen: []string{"second.go"},
		},
		{
			name: "a finding of the host's edits another file",
			plant: func(standing *plantedStanding, _ string) {
				standing.planted["second.go"] = append(standing.planted["second.go"], rule.InheritedFinding{
					Line: 5, Column: 3, MessageId: "hostElsewhere", Message: "the host's edit elsewhere",
					Suggestions: []rule.InheritedSuggestion{{Message: "edit first.go", Edits: []rule.InheritedEdit{
						{File: "first.go", Start: 0, End: 2, Text: "planted"},
					}}},
				})
			},
			fallen: []string{"second.go"},
		},
	} {
		test.Run(doubt.name, func(test *testing.T) {
			test.Parallel()
			dispatcher := NewSyntax()
			ruleName := rule.Namespace + "doubted"
			doubted := dispatcher.RuleAnalyzer(ruleName, "doubted", "reports names, ifs and closing braces",
				doubtedListener())
			standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), ruleName)
			doubt.plant(standing, ruleName)
			action := standing.analyze(test, doubted).Roots[0]
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			requireLeftToTheRule(test, ruleName, reportedBy(action), standing.told(), doubt.fallen...)
		})
	}
}

// A rule confined on one file hears the next whole, beside a rule confined on both: with the host's findings on
// second.go withheld from one of two rules walked by the same runs, first.go and then second.go, that rule hears
// second.go's standing body and keeps its findings there, while the other is deaf in it and takes the host's. Confined
// is a run's on one file, never carried into the next.
func TestARuleConfinedOnOneFileHearsTheNextWholeBesideARuleConfinedOnBoth(test *testing.T) {
	test.Parallel()
	dispatcher := NewSyntax()
	partlyName, whollyName := rule.Namespace+"partly", rule.Namespace+"wholly"
	partly := dispatcher.RuleAnalyzer(partlyName, "partly", "reports names, ifs and closing braces", doubtedListener())
	wholly := dispatcher.RuleAnalyzer(whollyName, "wholly", "reports names, ifs and closing braces", doubtedListener())
	standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), partlyName, whollyName)
	standing.withheld[partlyName+" second.go"] = true
	graph := standing.analyze(test, partly, wholly)
	told := standing.told()
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		if action.Analyzer == partly {
			requireLeftToTheRule(test, partlyName, reportedBy(action), told, "second.go")
			continue
		}
		requireLeftToTheRule(test, whollyName, reportedBy(action), told)
	}
}

// Where the checker has every dispatcher follow an analyzer (checker.Options.StandingAfter), the standing bodies are
// answered from that analyzer's result on the package: with it finished, a confined rule takes the host's findings in
// both files, and with it failed nothing stands, so the dispatcher runs as on the host, the rule hears every body and
// keeps its own findings, none of the host's is reported and the build is told nothing.
func TestTheStandingBodiesFollowAnAnalyzerAndNothingStandsWhereItFailed(test *testing.T) {
	test.Parallel()
	for _, failure := range []error{nil, errors.New("planted failure")} {
		test.Run(fmt.Sprintf("the followed analyzer failed: %t", failure != nil), func(test *testing.T) {
			test.Parallel()
			dispatcher := NewSyntax()
			ruleName := rule.Namespace + "doubted"
			doubted := dispatcher.RuleAnalyzer(ruleName, "doubted", "reports names, ifs and closing braces",
				doubtedListener())
			standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), ruleName)
			options := standing.options()
			options.StandingAfter = &analysis.Analyzer{
				Name:       "followed",
				Doc:        "followed is what the dispatcher follows, and says so or fails",
				ResultType: reflect.TypeFor[string](),
				Run: func(*analysis.Pass) (any, error) {
					if failure != nil {
						return nil, failure
					}
					return "followed", nil
				},
			}
			options.StandingBodies = func(_ string, after any) rule.StandingBodies {
				if after != "followed" {
					return nil
				}
				return standing
			}
			graph, analyzeError := checker.Analyze([]*analysis.Analyzer{doubted}, loadFixture(test, "standing"),
				options)
			if analyzeError != nil {
				test.Fatalf("analyzing the fixture: %v", analyzeError)
			}
			action := graph.Roots[0]
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			fallen := []string{}
			if failure != nil {
				fallen = []string{"first.go", "second.go"}
			}
			requireLeftToTheRule(test, ruleName, reportedBy(action), standing.told(), fallen...)
		})
	}
}

// A file with no standing function is no file a rule is confined on, though the rule is confined, the host holds its
// findings there and other files of the package have standing functions: with only first.go's functions standing, the
// rule keeps second.go whole and the build is told of first.go alone.
func TestARuleTakesNothingOnAFileWhereNothingStands(test *testing.T) {
	test.Parallel()
	dispatcher := NewSyntax()
	ruleName := rule.Namespace + "doubted"
	doubted := dispatcher.RuleAnalyzer(ruleName, "doubted", "reports names, ifs and closing braces", doubtedListener())
	standing := newPlantedStanding(test, map[string][]string{"first.go": {"stood", "stoodToo"}},
		hostsFindingsOnStanding(), ruleName)
	action := standing.analyze(test, doubted).Roots[0]
	if action.Err != nil {
		test.Fatalf("running %s: %v", action, action.Err)
	}
	told := standing.told()
	requireLeftToTheRule(test, ruleName, reportedBy(action), told, "second.go")
	if len(told) != 1 || told[ruleName+" first.go"] != [2]int{2, 3} {
		test.Errorf("the rule told the build it took %v, want first.go's two standing functions and the three "+
			"findings of the host's inside them, and nothing else", told)
	}
}

// A confined rule that crashes reports nothing of the host's, as it reports nothing of its own: its action ends with
// the crash, the build is told nothing for it, and its outcome carries no standing take beside the crash, while a
// confined rule beside it on the same dispatcher reports the host's findings and tells the build.
func TestACrashedRuleReportsNothingOfTheHostsInAStandingFunction(test *testing.T) {
	test.Parallel()
	dispatcher := NewSyntax()
	crashingName, steadyName := rule.Namespace+"crashing", rule.Namespace+"steady"
	crashing := dispatcher.RuleAnalyzer(crashingName, "crashing", "panics as it leaves a file", &rule.Listener{
		FileLocal: true,
		FileExit:  func(*rule.Context, *ast.File) { panic("planted panic") },
	})
	steady := dispatcher.RuleAnalyzer(steadyName, "steady", "reports nothing of its own", &rule.Listener{
		FileLocal: true,
		FileExit:  func(*rule.Context, *ast.File) { /* Reports nothing, so all its action reports is the host's. */ },
	})
	var outcome Outcome
	outcomeProbe := &analysis.Analyzer{
		Name:     "outcome_probe",
		Doc:      "reads the crashing rule's outcome off the dispatcher's result",
		Requires: []*analysis.Analyzer{dispatcher.Analyzer()},
		Run: func(pass *analysis.Pass) (any, error) {
			result, isResult := pass.ResultOf[dispatcher.Analyzer()].(Result)
			if !isResult {
				return nil, fmt.Errorf("the dispatcher's result is %T", pass.ResultOf[dispatcher.Analyzer()])
			}
			outcome = result[crashingName]
			return nil, nil
		},
	}
	standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), crashingName, steadyName)
	for _, action := range standing.analyze(test, crashing, steady, outcomeProbe).Roots {
		switch action.Analyzer {
		case crashing:
			if action.Err == nil || !strings.Contains(action.Err.Error(), "planted panic") ||
				len(action.Diagnostics) != 0 {
				test.Errorf("the crashing rule ended with %v and reported %d findings, want its crash and nothing "+
					"reported", action.Err, len(action.Diagnostics))
			}
		case steady:
			if reported := reportedBy(action); action.Err != nil || !slices.Equal(reported, hostsFindingsInStanding) {
				test.Errorf("the rule beside the crash ended with %v and reported\n%s\nwant the host's findings in "+
					"the standing functions\n%s", action.Err, strings.Join(reported, "\n"),
					strings.Join(hostsFindingsInStanding, "\n"))
			}
		default:
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
		}
	}
	if outcome.Crash == nil || outcome.StandingFiles != nil || outcome.Standing != nil {
		test.Errorf("the crashing rule's outcome is its crash %v beside %d standing files and %d of the host's "+
			"findings, want the crash and nothing of the host's", outcome.Crash, len(outcome.StandingFiles),
			len(outcome.Standing))
	}
	expectedTold := map[string][2]int{steadyName + " first.go": {2, 3}, steadyName + " second.go": {1, 1}}
	if told := standing.told(); !maps.Equal(told, expectedTold) {
		test.Errorf("the rules told the build they took %v, want only the rule that finished, %v", told, expectedTold)
	}
}

// A file a syntax-only rule inherits whole stands in no part: the rule hears none of it, reports the host's findings
// on it once, as the inherited file's, and takes the standing functions of the file it does hear. Taken both ways,
// the host's finding inside second.go's standing function would be reported twice.
func TestAFileARuleInheritsWholeStandsInNoPart(test *testing.T) {
	test.Parallel()
	dispatcher := NewSyntax()
	ruleName := rule.Namespace + "inheriting"
	listener := namingListener()
	listener.SyntaxOnly = true
	inheriting := dispatcher.RuleAnalyzer(ruleName, "inheriting", "reports each name and if", listener)
	standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), ruleName)
	options := standing.options()
	wholeFile := &tookFindings{byFile: map[string]int{}}
	options.Inherited = rule.Inherited
	options.Inheritance = plantedInheritance{ruleName: ruleName, took: wholeFile,
		planted: map[string][]rule.InheritedFinding{"second.go": {
			{Line: 4, Column: 2, MessageId: "inheritedIf", Message: "the host's whole file"},
		}}}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{inheriting}, loadFixture(test, "standing"), options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	action := graph.Roots[0]
	if action.Err != nil {
		test.Fatalf("running %s: %v", action, action.Err)
	}
	reported := reportedBy(action)
	expected := []string{
		"first.go:7:2 its own if",
		"first.go:27:6 its own name reached",
		"first.go:28:2 its own if",
		"second.go:4:2 inheritedIf the host's whole file",
		"first.go:15:2-17:3 hostIf the host's if, fix replace it: 30-34 planted",
		"first.go:13:1 hostDoc the host's doc comment",
		"first.go:34:6 hostName the host's name",
	}
	if !slices.Equal(reported, expected) {
		test.Errorf("the rule reported\n%s\nwant its own on first.go outside the standing functions, second.go's "+
			"inherited finding, then the host's inside first.go's standing functions\n%s",
			strings.Join(reported, "\n"), strings.Join(expected, "\n"))
	}
	told := standing.told()
	if len(told) != 1 || told[ruleName+" first.go"] != [2]int{2, 3} || wholeFile.byFile["second.go"] != 1 {
		test.Errorf("the rule told the build it took %v of the standing functions and %v whole, want first.go's "+
			"standing functions alone and second.go whole", told, wholeFile.byFile)
	}
}

// A rule heard over cohere's own intermediate representation swaps its findings at its action as a syntax rule
// does, since both dispatchers start their listeners one way: of a rule reporting every source function where it is
// declared, the findings on the standing functions, and on the function literal inside one, are dropped for the
// host's, and its findings on the others, the function literal in a variable's initializer among them, are kept.
func TestARuleOverTheRepresentationReportsTheHostsFindingsInAStandingFunction(test *testing.T) {
	test.Parallel()
	dispatcher := NewIntermediateRepresentation()
	ruleName := rule.Namespace + "functions"
	functions := dispatcher.RuleAnalyzer(ruleName, "functions", "reports every source function", &rule.Listener{
		IntermediateRepresentation: &rule.IntermediateRepresentationListener{
			Function: func(context *rule.Context, function *intermediate_representation.Function) {
				if function.Synthetic == "" {
					context.Reportf(function.Pos(), "its own function")
				}
			},
		},
	})
	standing := newPlantedStanding(test, everyStandingFunction, hostsFindingsOnStanding(), ruleName)
	action := standing.analyze(test, functions).Roots[0]
	if action.Err != nil {
		test.Fatalf("running %s: %v", action, action.Err)
	}
	reported := reportedBy(action)
	slices.Sort(reported)
	expected := slices.Sorted(slices.Values(slices.Concat(hostsFindingsInStanding, []string{
		"first.go:6:12 its own function",
		"first.go:27:6 its own function",
		"second.go:10:6 its own function",
	})))
	if !slices.Equal(reported, expected) {
		test.Errorf("the rule reported\n%s\nwant its own outside the standing functions and the host's inside them\n%s",
			strings.Join(reported, "\n"), strings.Join(expected, "\n"))
	}
	expectedTold := map[string][2]int{ruleName + " first.go": {2, 3}, ruleName + " second.go": {1, 1}}
	if told := standing.told(); !maps.Equal(told, expectedTold) {
		test.Errorf("the rule told the build it took %v, want %v", told, expectedTold)
	}
}
