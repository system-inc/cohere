package odd_slice_argument_test

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/rules/odd_slice_argument"
)

// askedRun is what one run's questions left in a fresh resolver: each answer asked, by the function and parameter it
// is about, and, for every package of the load that holds any, the marks recorded for the store, by objectpath, and
// the answers the package's analysis read of other packages, each as its owner's path, the object's path and the
// answer.
type askedRun struct {
	answers map[string]bool
	marks   map[string]map[objectpath.Path]string
	reads   map[string][]string
}

// runAsking loads the package at pattern from the rule's testdata and, on a fresh resolver under a store that holds
// nothing, so the run records every answer and every read, asks each of the package's parameters named by asked, as
// "function parameter", of the package itself, in that order, with nothing asked before. It returns what the run
// left.
func runAsking(test *testing.T, pattern string, asked []string) askedRun {
	test.Helper()
	roots := loadFixtures(test, analysistest.TestData(), []string{pattern})
	resolver := fact_resolver.New(emptyStore{})
	options := check.DispatchOptions()
	options.Resolver = resolver
	ran := askedRun{
		answers: map[string]bool{},
		marks:   map[string]map[objectpath.Path]string{},
		reads:   map[string][]string{},
	}
	asking := &analysis.Analyzer{
		Name:     "entering_questions",
		Doc:      "asks the package's own marks in a given order, entering its walks there",
		Requires: []*analysis.Analyzer{odd_slice_argument.MarksAnalyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			home := resolver.Package(pass.Pkg)
			if home == nil {
				return nil, fmt.Errorf("the resolver never learned %s", pass.Pkg.Path())
			}
			for _, name := range asked {
				parameter, parameterError := parameterNamed(pass.Pkg, name)
				if parameterError != nil {
					return nil, parameterError
				}
				ran.answers[name] = odd_slice_argument.Ask(resolver, home, parameter)
			}
			return nil, nil
		},
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{asking}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", pattern, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("asking in %s: %v", action.Package.ID, action.Err)
		}
	}
	packages.Visit(roots, nil, func(visited *packages.Package) {
		for path, answer := range resolver.Recorded(visited.Types)[odd_slice_argument.Kind.Name] {
			if ran.marks[visited.PkgPath] == nil {
				ran.marks[visited.PkgPath] = map[objectpath.Path]string{}
			}
			ran.marks[visited.PkgPath][path] = string(answer)
		}
		reads, whole := resolver.Reads(visited.Types)
		if !whole {
			test.Fatalf("%s read an answer no entry can name", visited.PkgPath)
		}
		for _, read := range reads {
			if read.Owner == visited.Types {
				test.Errorf("%s holds a read of its own answer at %s, so its entry would depend on itself",
					visited.PkgPath, read.Path)
			}
			ran.reads[visited.PkgPath] = append(ran.reads[visited.PkgPath], fmt.Sprintf("%s %s %s=%s", read.Kind,
				read.Owner.Path(), read.Path, read.Value))
		}
	})
	return ran
}

// parameterNamed is the parameter of packageTypes named by name, "function parameter", a package-level function's.
func parameterNamed(packageTypes *types.Package, name string) (*types.Var, error) {
	functionName, parameterName, isNamed := strings.Cut(name, " ")
	function, isFunction := packageTypes.Scope().Lookup(functionName).(*types.Func)
	if !isNamed || !isFunction {
		return nil, fmt.Errorf("%s declares no function for %q", packageTypes.Path(), name)
	}
	for parameter := range function.Signature().Params().Variables() {
		if parameter.Name() == parameterName {
			return parameter, nil
		}
	}
	return nil, fmt.Errorf("%s.%s takes no parameter %s", packageTypes.Path(), functionName, parameterName)
}

// A walk asks the same marks of other packages wherever a question enters a cycle (#8d9r2rh). In entry/home, h passes
// its slice to f and to pairs, and f passes its slice back to h and across to other.G. Entered at f or at h, each on a
// fresh resolver, the walk must ask other.G's parameter: home's reads, and other's record of its answers, are the same
// either way. A walk that asks a pass across a package only while its caller's slice is still unmarked asks it
// entered at h, where f is read before h is marked, and never entered at f.
func TestAWalkAsksAcrossAPackageWhereverItEntersACycle(test *testing.T) {
	test.Parallel()
	atF := runAsking(test, "entry/home", []string{"f values"})
	atH := runAsking(test, "entry/home", []string{"h values"})
	if !reflect.DeepEqual(atF.reads, atH.reads) || !reflect.DeepEqual(atF.marks["entry/other"],
		atH.marks["entry/other"]) {
		test.Errorf("entered at f, home reads %v and other records %v; entered at h, %v and %v", atF.reads,
			atF.marks["entry/other"], atH.reads, atH.marks["entry/other"])
	}
	want := []string{"odd_slice_argument entry/other G.PA0=false"}
	if !reflect.DeepEqual(atF.reads["entry/home"], want) {
		test.Errorf("entered at f, home reads %v, want %v", atF.reads["entry/home"], want)
	}
}

// A cycle of calls entered at any of its members leaves the same marks, reads and record (#8d9r2rh). For each cycle,
// each on a fresh resolver with nothing asked before: the first question is of one member's slice, which enters the
// walk there, and then every member's slice is asked, so each run asks the same marks and only its entry differs.
// The cycles are order/forward's of two, around's of one, swapped calling itself, and of two, first and second, which
// a mark goes around twice, and entry/home's of two and of three, each passing a slice across a package to other.G.
// Every member is marked, by the check its cycle reaches.
func TestACycleEnteredAtAnyMemberLeavesTheSameRun(test *testing.T) {
	test.Parallel()
	for _, cycle := range []struct {
		pattern string
		members []string
	}{
		{pattern: "order/forward", members: []string{"back values", "forward values"}},
		{pattern: "around", members: []string{"swapped left", "swapped right"}},
		{pattern: "around", members: []string{"first left", "first right", "second left", "second right"}},
		{pattern: "entry/home", members: []string{"h values", "f values"}},
		{pattern: "entry/home", members: []string{"a values", "b values", "c values"}},
	} {
		test.Run(cycle.pattern+" "+strings.Join(cycle.members, ", "), func(test *testing.T) {
			test.Parallel()
			var first askedRun
			for index, entry := range cycle.members {
				ran := runAsking(test, cycle.pattern, append([]string{entry}, cycle.members...))
				for _, member := range cycle.members {
					if !ran.answers[member] {
						test.Errorf("entered at %s, %s is unmarked", entry, member)
					}
				}
				if index == 0 {
					first = ran
					continue
				}
				if !reflect.DeepEqual(ran, first) {
					test.Errorf("entered at %s the run left %+v, entered at %s %+v", entry, ran, cycle.members[0],
						first)
				}
			}
		})
	}
}
