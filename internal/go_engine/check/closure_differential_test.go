package check

import (
	"fmt"
	"go/types"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/diff"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// closureBottom is the import path of the workspace's bottom package, the one whose analysis the differential compares.
const closureBottom = "example.com/closure/bottom"

// differentialFiles are the workspace the differential runs over. Bottom is P: generics, a pure function it discards
// the result of, a pure function only its importer calls, a function returning a typed nil it compares, a printf
// wrapper it misuses, a function that wants its arguments in pairs, called with an odd number, one that never returns
// and one that is deprecated. Importer is Q: it instantiates bottom's generics with its own types and asks of the same
// functions, and of the one bottom never calls. Unrelated is Z, which neither imports nor is imported.
var differentialFiles = map[string]string{
	"go.mod": "module example.com/closure\n\ngo 1.24\n",
	"bottom/bottom.go": `// Package bottom is the package whose analysis must read only its import closure.
package bottom

import "fmt"

// Box holds one value.
type Box[Value any] struct {
	held Value
}

// Wrap is value in a box.
func Wrap[Value any](value Value) Box[Value] {
	return Box[Value]{held: value}
}

// Held is the box's value.
func (box Box[Value]) Held() Value {
	return box.held
}

// Apply is each of values transformed.
func Apply[Value any, Transformed any](values []Value, transform func(Value) Transformed) []Transformed {
	transformed := make([]Transformed, 0, len(values))
	for _, value := range values {
		transformed = append(transformed, transform(value))
	}
	return transformed
}

// Double is value twice.
func Double(value int) int {
	return value * 2
}

// Triple is value three times, which only an importer calls.
func Triple(value int) int {
	return value * 3
}

type failure struct{}

func (*failure) Error() string {
	return "failure"
}

// Fail is an error that is never nil, though what it holds is.
func Fail() error {
	println("not trivial")
	var value *failure
	return value
}

// Logf writes as fmt.Printf does.
func Logf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

// Pairs takes its arguments in pairs.
func Pairs(name string, arguments ...any) {
	if len(arguments)%2 != 0 {
		panic(name)
	}
}

// Stop never returns.
func Stop(reason string) {
	panic(reason)
}

// Old is value as it was.
//
// Deprecated: use Double.
func Old(value int) int {
	return value
}

// Use calls each of them as a rule finds fault with.
func Use() int {
	Double(2)
	_ = Fail() == nil
	Logf("%d\n", "text")
	Pairs("name", 1, 2, 3)
	return Wrap(1).Held() + len(Apply([]int{1}, Double))
}
`,
	"importer/importer.go": `// Package importer asks of bottom's functions and instantiates its generics with its own types.
package importer

import "example.com/closure/bottom"

type local struct {
	name string
}

// Use calls bottom's functions as bottom does, and the one bottom never calls.
func Use() string {
	bottom.Triple(3)
	bottom.Double(3)
	_ = bottom.Fail() == nil
	bottom.Logf("%d\n", "text")
	bottom.Pairs("name", 1)
	if bottom.Old(1) > 1 {
		bottom.Stop("old")
	}
	held := bottom.Wrap(local{name: "held"}).Held()
	return held.name + bottom.Apply([]local{held}, func(item local) string { return item.name })[0]
}
`,
	"unrelated/unrelated.go": `// Package unrelated neither imports bottom nor is imported by it.
package unrelated

// Quadruple is value four times.
func Quadruple(value int) int {
	return value * 4
}

// Use discards a pure call.
func Use() {
	Quadruple(1)
}
`,
}

// closureKinds are the fact kinds the resolver computes on demand, each of which the differential holds to no
// contradiction.
var closureKinds = []*fact_resolver.Kind{function_purity.Kind, odd_slice_argument.Kind, return_nil_ness.Kind}

// closureRun is one analysis of the workspace by every registered rule: the patterns' load, the graph, the resolver
// it asked, and bottom's package in it.
type closureRun struct {
	root     string
	graph    *checker.Graph
	resolver *fact_resolver.Resolver
	bottom   *packages.Package
	ruleOf   map[*analysis.Analyzer]rule.Rule
}

// analyzeClosure loads patterns in root and runs every registered rule over what they match, as Run analyzes them,
// with a resolver that records what it computes.
func analyzeClosure(test *testing.T, root string, patterns ...string) *closureRun {
	test.Helper()

	loaded, loadError := load.Packages(root, patterns, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading %v: %v %+v", patterns, loadError, loaded)
	}
	run := &closureRun{root: root, ruleOf: map[*analysis.Analyzer]rule.Rule{}}
	var analyzers []*analysis.Analyzer
	for _, registered := range rules.All() {
		run.ruleOf[registered.Analyzer] = registered
		analyzers = append(analyzers, registered.Analyzer)
	}
	if wrapError := wrapRuns(analyzers); wrapError != nil {
		test.Fatal(wrapError)
	}
	options := DispatchOptions()
	run.resolver = fact_resolver.New(answerlessStore{})
	options.Resolver = run.resolver
	graph, analyzeError := checker.Analyze(analyzers, loaded.Packages, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	run.graph = graph
	for _, analyzed := range loaded.Packages {
		if analyzed.PkgPath == closureBottom {
			run.bottom = analyzed
		}
	}
	if run.bottom == nil {
		test.Fatalf("loading %v matched no %s", patterns, closureBottom)
	}
	return run
}

// recordedPaths is every objectpath each kind's answers the run recorded in bottom hold.
func (run *closureRun) recordedPaths() map[string][]objectpath.Path {
	paths := map[string][]objectpath.Path{}
	for kind, answers := range run.resolver.Recorded(run.bottom.Types) {
		paths[kind] = slices.Collect(maps.Keys(answers))
	}
	return paths
}

// objectName is object as a line of the rendering names it: its objectpath in its package, or its position for an
// object with none.
func (run *closureRun) objectName(object types.Object) string {
	if path, pathError := objectpath.For(object); pathError == nil {
		return string(path)
	}
	return strings.TrimPrefix(run.bottom.Fset.Position(object.Pos()).String(), run.root)
}

// exportedFactLine is fact, about what, as analyzer exported it: its type and a digest of its value as gob carries it,
// which two runs that export equal facts digest alike.
func exportedFactLine(analyzer *analysis.Analyzer, what string, fact analysis.Fact) string {
	digest, digestError := factValueDigest(fact)
	if digestError != nil {
		return fmt.Sprintf("fact %s %s %s undigested: %v", analyzer.Name, what, factTypeName(reflect.TypeOf(fact)),
			digestError)
	}
	return fmt.Sprintf("fact %s %s %s %s", analyzer.Name, what, factTypeName(reflect.TypeOf(fact)), digest)
}

// render is bottom as the run analyzed it, one sorted line each: every rule's findings, or its crash, every fact
// every action on bottom exported, the answers it took from other packages, each kind's answer about every object of
// paths asked as bottom asks it, and the answers of bottom computed whole. The answers are asked before the whole is
// computed, since computing the whole replaces each verdict the demand walk kept with the push's.
func (run *closureRun) render(paths map[string][]objectpath.Path) string {
	var lines []string
	for action := range run.graph.All() {
		if action.Package != run.bottom {
			continue
		}
		if enabled, isRule := run.ruleOf[action.Analyzer]; isRule && action.IsRoot {
			if action.Err != nil {
				lines = append(lines, fmt.Sprintf("crash %s: %v", enabled.Name, action.Err))
				continue
			}
			for _, finding := range actionFindings(enabled, action) {
				finding.File = strings.TrimPrefix(finding.File, run.root)
				lines = append(lines, fmt.Sprintf("finding %+v", finding))
			}
		}
		objectFacts, packageFacts := action.ExportedFacts()
		for _, objectFact := range objectFacts {
			lines = append(lines, exportedFactLine(action.Analyzer, run.objectName(objectFact.Object), objectFact.Fact))
		}
		for _, packageFact := range packageFacts {
			lines = append(lines, exportedFactLine(action.Analyzer, "package", packageFact.Fact))
		}
	}
	reads, whole := run.resolver.Reads(run.bottom.Types)
	lines = append(lines, fmt.Sprintf("reads whole %t", whole))
	for _, read := range reads {
		lines = append(lines, fmt.Sprintf("read %s %s %s %s", read.Kind, read.Owner.Path(), read.Path, read.Value))
	}
	for kind, kindPaths := range paths {
		for _, path := range kindPaths {
			answer, answered := run.resolver.Probe(kind, run.bottom.Types, path)
			lines = append(lines, fmt.Sprintf("answer %s %s %s answered %t", kind, path, answer, answered))
		}
	}
	if home := run.resolver.Package(run.bottom.Types); home != nil {
		for kind, answers := range run.resolver.Whole(home) {
			for path, answer := range answers {
				lines = append(lines, fmt.Sprintf("whole %s %s %s", kind, path, answer))
			}
		}
	} else {
		lines = append(lines, "the resolver never learned bottom")
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n") + "\n"
}

// Every rule's findings on a package, the facts its actions export and the answers the resolver gives about it are a
// function of the package and its import closure alone (#23b2grt): the walk hands a package no edit reaches the host's
// findings on that premise, though the host's run loaded its importers and packages unrelated to it, and an extra
// build's may not. Every registered rule runs over a workspace whose bottom package is imported by one that
// instantiates its generics with its own types and asks the resolver of the same functions, and of one bottom never
// asks of, beside one unrelated to either; and again over bottom's closure alone, the patterns naming bottom only, so
// neither other package is loaded. Bottom must read the same, line for line, both ways, and no kind may contradict a
// verdict it kept in either run.
func TestABottomPackageReadsTheSameWithItsImportersAndWithout(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, differentialFiles)
	workspace := analyzeClosure(test, root, "./...")
	closure := analyzeClosure(test, root, "./bottom")
	for _, action := range closure.graph.Roots {
		if action.Package.PkgPath != closureBottom {
			test.Fatalf("the closure's run analyzed %s, want bottom alone", action.Package.PkgPath)
		}
	}
	paths := map[string][]objectpath.Path{}
	for _, run := range []*closureRun{workspace, closure} {
		for kind, kindPaths := range run.recordedPaths() {
			for _, path := range kindPaths {
				if !slices.Contains(paths[kind], path) {
					paths[kind] = append(paths[kind], path)
				}
			}
		}
	}
	workspaceRendering, closureRendering := workspace.render(paths), closure.render(paths)
	if workspaceRendering != closureRendering {
		test.Errorf("bottom reads otherwise beside its importer than over its closure alone:\n%s",
			diff.Unified("closure alone", "whole workspace", closureRendering, workspaceRendering))
	}
	for _, run := range []struct {
		name     string
		analyzed *closureRun
	}{{"the whole workspace", workspace}, {"the closure alone", closure}} {
		for _, kind := range closureKinds {
			if contradicted := run.analyzed.resolver.Counts(kind).Contradicted.Load(); contradicted != 0 {
				test.Errorf("over %s, %s's fallbacks contradicted %d verdicts its demand walk kept", run.name,
					kind.Name, contradicted)
			}
		}
		if run.analyzed.resolver.Unsure(run.analyzed.bottom.Types) || len(run.analyzed.resolver.Reloads()) > 0 {
			test.Errorf("over %s, bottom was handed a placeholder, or a question reached a package to reload", run.name)
		}
	}
	if strings.HasPrefix(closureRendering, "crash ") || strings.Contains(closureRendering, "\ncrash ") {
		test.Errorf("a rule crashed on bottom, so its findings were never compared:\n%s", closureRendering)
	}
	// The comparison proves something only when bottom found what it was written to, learned a fact, and the resolver
	// answered each kind about it, the purity of Triple only because its importer asked.
	for _, wanted := range []string{"{Rule:" + rule.Namespace + "SA4017 ", "{Rule:" + rule.Namespace + "SA4023 ",
		"{Rule:" + rule.Namespace + "SA5012 ", "{Rule:" + rule.Namespace + "printf ", "\nfact printf Logf ",
		"\nfact ctrlflow Stop ", "\nfact fact_deprecated Old ",
		"\nanswer function_purity Triple ", "\nanswer odd_slice_argument ", "\nanswer return_nil_ness "} {
		if !strings.Contains(closureRendering, wanted) {
			test.Errorf("bottom's rendering holds no %q, so the comparison proves less than it claims:\n%s", wanted,
				closureRendering)
		}
	}
}
