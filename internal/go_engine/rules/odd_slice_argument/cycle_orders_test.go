package odd_slice_argument_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"honnef.co/go/tools/staticcheck/sa5012"

	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
)

// cycleFixture is a fixture's functions as separate declarations, each by its name, and its finding calls, each a
// line of odd's body by the name a test gives it, for writing the package in every order its declarations can take.
type cycleFixture struct {
	declarations map[string]string
	calls        []cycleCall
}

// cycleCall is one call of odd's body and the name a test gives the finding it makes.
type cycleCall struct {
	name string
	text string
}

// aroundCycles is package around's functions, as testdata/src/around declares them, without their comments.
var aroundCycles = cycleFixture{
	declarations: map[string]string{
		"pairs":   "func pairs(values []string) {\n\tif len(values)%2 != 0 {\n\t\tpanic(\"values come in pairs\")\n\t}\n}\n",
		"swapped": "func swapped(left, right []string) {\n\tswapped(right, left)\n\tpairs(left)\n}\n",
		"first":   "func first(left, right []string) {\n\tsecond(right, left)\n\tpairs(left)\n}\n",
		"second":  "func second(left, right []string) {\n\tfirst(left, right)\n}\n",
	},
	calls: []cycleCall{
		{name: "first's left", text: `first([]string{"one"}, nil)`},
		{name: "swapped's right", text: `swapped(nil, []string{"one"})`},
		{name: "first's right", text: `first(nil, []string{"one"})`},
		{name: "second's right", text: `second(nil, []string{"one"})`},
	},
}

// forwardCycle is package order/forward's functions, as testdata/src/order/forward declares them, without their
// comments.
var forwardCycle = cycleFixture{
	declarations: map[string]string{
		"enter":   "func enter() {\n\tforward(nil)\n}\n",
		"back":    "func back(values []string) {\n\tforward(values)\n}\n",
		"forward": "func forward(values []string) {\n\tback(nil)\n\tpairs(values)\n}\n",
		"pairs":   "func pairs(values []string) {\n\tif len(values)%2 != 0 {\n\t\tpanic(\"values come in pairs\")\n\t}\n}\n",
	},
	calls: []cycleCall{{name: "back's values", text: `back([]string{"one"})`}},
}

// orders is every order of names.
func orders(names []string) [][]string {
	if len(names) <= 1 {
		return [][]string{append([]string{}, names...)}
	}
	var all [][]string
	for index, name := range names {
		rest := append(append([]string{}, names[:index]...), names[index+1:]...)
		for _, order := range orders(rest) {
			all = append(all, append([]string{name}, order...))
		}
	}
	return all
}

// reportedInOrders writes fixture under directory as one package for every order of its declarations and odd, runs
// staticcheck's SA5012 and the port over all of them, and returns, for each finding call by its name, in how many of
// the orders each reports it, and how many orders there were.
func reportedInOrders(test *testing.T, directory string, base string, fixture cycleFixture) (map[string]int,
	map[string]int, int) {
	test.Helper()
	names := []string{"odd"}
	for name := range fixture.declarations {
		names = append(names, name)
	}
	odd := "func odd() {\n"
	for _, call := range fixture.calls {
		odd += "\t" + call.text + "\n"
	}
	odd += "}\n"
	// callAt names each finding call by the file and line it sits on.
	callAt := map[string]string{}
	var patterns []string
	all := orders(names)
	for index, order := range all {
		path := fmt.Sprintf("%s/order%03d", base, index)
		file := filepath.Join(directory, "src", filepath.FromSlash(path), "cycle.go")
		source := fmt.Sprintf("package order%03d\n", index)
		for _, name := range order {
			declaration := fixture.declarations[name]
			if name == "odd" {
				declaration = odd
				line := strings.Count(source, "\n") + 3
				for _, call := range fixture.calls {
					callAt[fmt.Sprintf("%s:%d", file, line)] = call.name
					line++
				}
			}
			source += "\n" + declaration
		}
		if makeError := os.MkdirAll(filepath.Dir(file), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(file, []byte(source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		patterns = append(patterns, path)
	}
	outcomes, analyzeError := parity.Analyze([]parity.Pair{{Name: "SA5012", Original: sa5012.Analyzer,
		Port: odd_slice_argument.Analyzer}}, loadFixtures(test, directory, patterns))
	if analyzeError != nil {
		test.Fatal(analyzeError)
	}
	count := func(side parity.Side) map[string]int {
		if len(side.Failures) != 0 {
			test.Fatalf("a run failed: %+v", side.Failures)
		}
		reported := map[string]int{}
		for _, finding := range side.Findings {
			name, isCall := callAt[fmt.Sprintf("%s:%d", finding.File, finding.Line)]
			if !isCall {
				test.Fatalf("a finding at no finding call: %+v", finding)
			}
			reported[name]++
		}
		return reported
	}
	return count(outcomes[0].Original), count(outcomes[0].Port), len(all)
}

// Where staticcheck reports the findings of a cycle of calls, over every order a package can declare the cycle's
// functions and odd in (#8d9r2rh). In around, staticcheck reports the slice passed as first's left in every order,
// and the three passed as a right, to swapped, first and second, in none: its one visit of each function never
// carries a mark around twice. In order/forward it reports the slice passed to back in some orders and not in
// others, order/back's among the first and order/forward's among the second, as
// TestACycleReportsTheSameInEitherDeclarationOrder holds of the two. The port reports all of them in every order.
func TestStaticcheckReportsACyclesFindingsInTheseOrders(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	original, port, count := reportedInOrders(test, directory, "around", aroundCycles)
	if original["first's left"] != count || original["swapped's right"] != 0 || original["first's right"] != 0 ||
		original["second's right"] != 0 {
		test.Errorf("over around's %d orders staticcheck reports %v, want first's left in every one and no right",
			count, original)
	}
	for _, call := range aroundCycles.calls {
		if port[call.name] != count {
			test.Errorf("over around's %d orders the port reports %s in %d", count, call.name, port[call.name])
		}
	}
	original, port, count = reportedInOrders(test, directory, "forward", forwardCycle)
	if original["back's values"] == 0 || original["back's values"] == count {
		test.Errorf("over order/forward's %d orders staticcheck reports back's values in %d, want some and not all",
			count, original["back's values"])
	}
	if port["back's values"] != count {
		test.Errorf("over order/forward's %d orders the port reports back's values in %d", count, port["back's values"])
	}
	test.Logf("over order/forward's %d orders staticcheck reports back's values in %d", count,
		original["back's values"])
}
