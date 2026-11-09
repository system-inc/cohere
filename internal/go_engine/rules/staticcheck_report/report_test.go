package staticcheck_report_test

import (
	"fmt"
	"go/ast"
	"go/version"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/facts/tokenfile"
	"honnef.co/go/tools/analysis/report"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// mismatchesShown is how many mismatches a failing run prints, the rest counted, since a broken range breaks
// thousands of calls alike.
const mismatchesShown = 25

// moduleEnvironment loads a fixture module as analysistest does, in module mode with nothing fetched.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// comparison is what one package's side by side run found.
type comparison struct {
	// calls counts the pairs of calls compared.
	calls int
	// mismatches describes each pair whose two sides differed.
	mismatches []string
	// met counts each label of requiredLabels the run met.
	met map[string]int
}

// outcome is what one call did: the diagnostics it reported, and what it panicked with, if it did.
type outcome struct {
	diagnostics []analysis.Diagnostic
	panicked    string
}

// versionRead is what one read of a version did: the version, and what it panicked with, if it did.
type versionRead struct {
	version  string
	panicked string
}

// optionSet is one way of calling both reporters at a node, the same options spelled once for each.
type optionSet struct {
	name string
	// gate is the name of the version gate the set tries, and "" for a set that tries none.
	gate     string
	upstream []report.Option
	kit      []staticcheck_report.Option
}

// gate is one of the four version gates, spelled for each reporter.
type gate struct {
	name string
	// language is whether the gate reads the language version, and otherwise the standard library's.
	language bool
	upstream func(version string) report.Option
	kit      func(version string) staticcheck_report.Option
}

// gates are the four version gates. staticcheck's maximum language gate is set by hand, since its option sets the
// minimum (see TestStaticcheckMaximumLanguageVersionSetsTheMinimum), and its Report reads the maximum all the same.
var gates = []gate{
	{"MinimumLanguageVersion", true, report.MinimumLanguageVersion, staticcheck_report.MinimumLanguageVersion},
	{"MaximumLanguageVersion", true, func(version string) report.Option {
		return func(options *report.Options) { options.MaximumLanguageVersion = version }
	}, staticcheck_report.MaximumLanguageVersion},
	{"MinimumStdlibVersion", false, report.MinimumStdlibVersion, staticcheck_report.MinimumStandardLibraryVersion},
	{"MaximumStdlibVersion", false, report.MaximumStdlibVersion, staticcheck_report.MaximumStandardLibraryVersion},
}

// gateVersions are the versions every gate is tried at, besides the node's own: older and newer than every version
// the fixtures name, and each of those versions.
var gateVersions = []string{"go1.1", "go1.18", "go1.20", "go1.21", "go1.22", "go1.23", "go1.999"}

// comparingAnalyzer runs both reporters side by side over a package, its result a comparison. It requires what
// staticcheck's Report reads, the tokenfile and generated passes, and the engine's own intermediate representation,
// whose functions and instructions the ported checks report at.
var comparingAnalyzer = &analysis.Analyzer{
	Name: "comparereporters",
	Doc:  "reports at every node through staticcheck's report.Report and the kit's Report, and compares them",
	Requires: []*analysis.Analyzer{
		build_intermediate_representation.Analyzer, generated.Analyzer, tokenfile.Analyzer,
	},
	Run:        compareReporters,
	ResultType: reflect.TypeFor[*comparison](),
}

// The kit's Report and staticcheck's report.Report report side by side at every node of three fixtures, every node of
// the syntax, every function, parameter and instruction of the engine's own intermediate representation and every
// object the package defines, under every option and every version gate, and each pair of calls must report the same
// diagnostics, its position, end, message, related information and suggested fixes, or panic alike. The kit's File,
// LanguageVersion, StandardLibraryVersion, DisplayPosition and HasRange must answer as staticcheck's do at each node
// too, HasRange both true and false. shapes
// holds every shape shortRange cuts, a file tagged with its own version and three generated files, two of them moved
// by line directives; older_module, at go 1.20, and newer_module, at go 1.22, each hold a file tagged older and one
// tagged newer than the module, so StandardLibraryVersion's two readings of a module's go line are both met. The test
// fails unless every shape, every gate below, at and above the node's version, and the generated filter both
// withholding and keeping were met, so a fixture that stops holding one cannot pass by comparing nothing.
func TestReportMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loads := []struct {
		directory   string
		patterns    []string
		environment []string
	}{
		{directory, []string{"shapes"}, []string{"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off"}},
		{filepath.Join(directory, "src", "older_module"), []string{"./..."}, moduleEnvironment},
		{filepath.Join(directory, "src", "newer_module"), []string{"./..."}, moduleEnvironment},
	}
	calls := 0
	mismatches := []string{}
	met := map[string]int{}
	for _, each := range loads {
		loaded, loadError := load.Packages(each.directory, each.patterns, each.environment, nil)
		if loadError != nil {
			test.Fatalf("loading %v from %s: %v", each.patterns, each.directory, loadError)
		}
		for _, loadedPackage := range loaded.Roots {
			if len(loadedPackage.Errors) > 0 {
				test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
			}
		}
		graph, analyzeError := checker.Analyze(
			[]*analysis.Analyzer{comparingAnalyzer}, loaded.Roots, &checker.Options{},
		)
		if analyzeError != nil {
			test.Fatalf("analyzing %v: %v", each.patterns, analyzeError)
		}
		for _, action := range graph.Roots {
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			compared, isComparison := action.Result.(*comparison)
			if !isComparison {
				test.Fatalf("running %s: its result is %T, where a comparison was expected", action, action.Result)
			}
			calls += compared.calls
			mismatches = append(mismatches, compared.mismatches...)
			for label, count := range compared.met {
				met[label] += count
			}
		}
	}
	test.Logf("%d calls compared, %d mismatched", calls, len(mismatches))
	for _, mismatch := range mismatches[:min(len(mismatches), mismatchesShown)] {
		test.Error(mismatch)
	}
	if len(mismatches) > mismatchesShown {
		test.Errorf("and %d more mismatches", len(mismatches)-mismatchesShown)
	}
	for _, label := range requiredLabels() {
		test.Logf("%s: %d", label, met[label])
		if met[label] == 0 {
			test.Errorf("the fixtures never met %s, so the comparison proves nothing of it", label)
		}
	}
}

// The kit's Ordinal writes every number from -125 to 1250 as staticcheck's report.Ordinal does, so the teens, the
// hundreds whose last two digits are 11 to 13, zero and negative numbers all read alike, and a few it writes are
// held by their text too, so a change both sides could share still shows.
func TestOrdinalMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	compared := 0
	for number := -125; number <= 1250; number++ {
		compared++
		if upstream, kit := report.Ordinal(number), staticcheck_report.Ordinal(number); upstream != kit {
			test.Errorf("Ordinal(%d): staticcheck %q, the kit %q", number, upstream, kit)
		}
	}
	test.Logf("%d numbers compared", compared)
	for number, written := range map[int]string{
		-1: "-1th", 0: "0th", 1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 10: "10th", 11: "11th", 12: "12th", 13: "13th",
		20: "20th", 21: "21st", 22: "22nd", 23: "23rd", 101: "101st", 111: "111st", 112: "112nd",
	} {
		if kit := staticcheck_report.Ordinal(number); kit != written {
			test.Errorf("Ordinal(%d) is %q, where %q was expected", number, kit, written)
		}
	}
}

// staticcheck's MaximumLanguageVersion sets the minimum language version, where the kit's sets the maximum, the one
// place the two differ on purpose (see the package comment). TestReportMatchesStaticcheck compares the kit's maximum
// against staticcheck's Report reading a maximum set by hand, and this holds the slip itself, so a staticcheck that
// fixes it fails here and the package comment's difference can go.
func TestStaticcheckMaximumLanguageVersionSetsTheMinimum(test *testing.T) {
	test.Parallel()
	options := &report.Options{}
	report.MaximumLanguageVersion("go1.21")(options)
	if options.MinimumLanguageVersion != "go1.21" || options.MaximumLanguageVersion != "" {
		test.Errorf("staticcheck's MaximumLanguageVersion set the minimum to %q and the maximum to %q, where the slip "+
			"sets the minimum alone", options.MinimumLanguageVersion, options.MaximumLanguageVersion)
	}
}

// PassContext refuses an analyzer that does not require staticcheck's generated pass, so a context never judges every
// file hand written for want of the judgment.
func TestPassContextRefusesAPassWithoutTheGeneratedJudgment(test *testing.T) {
	test.Parallel()
	pass := &analysis.Pass{Analyzer: &analysis.Analyzer{Name: "planted"}, ResultOf: map[*analysis.Analyzer]any{}}
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError == nil || context != nil {
		test.Errorf("PassContext returned %v and %v, where an error was expected", context, contextError)
	}
}

// compareReporters calls both reporters at every node of the pass's package under every option set, and the kit's
// version helpers beside staticcheck's, recording each pair that differs and the labels it met.
func compareReporters(pass *analysis.Pass) (any, error) {
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	upstreamReported := []analysis.Diagnostic{}
	upstreamPass := *pass
	upstreamPass.Report = func(diagnostic analysis.Diagnostic) {
		upstreamReported = append(upstreamReported, diagnostic)
	}
	kitReported := []analysis.Diagnostic{}
	kitPass := *pass
	kitPass.Report = func(diagnostic analysis.Diagnostic) {
		kitReported = append(kitReported, diagnostic)
	}
	context, contextError := staticcheck_report.PassContext(&kitPass)
	if contextError != nil {
		return nil, contextError
	}
	generatedFiles, isGeneratedFiles := pass.ResultOf[generated.Analyzer].(map[string]generated.Generator)
	if !isGeneratedFiles {
		return nil, fmt.Errorf("the generated pass's result is %T", pass.ResultOf[generated.Analyzer])
	}

	compared := &comparison{met: map[string]int{}}
	mismatch := func(name string, node staticcheck_report.Positioner, upstream string, kit string) {
		compared.mismatches = append(compared.mismatches, fmt.Sprintf("%s at %T %s: staticcheck %s, the kit %s",
			name, node, pass.Fset.Position(node.Pos()), upstream, kit))
	}
	compareAt := func(node staticcheck_report.Positioner, label string) {
		if label != "" {
			compared.met[label]++
		}
		// The helpers the gates read answer alike.
		upstreamFile := code.File(pass, node)
		if kitFile := staticcheck_report.File(context, node); upstreamFile != kitFile {
			mismatch("File", node, fileDescribed(pass, upstreamFile), fileDescribed(pass, kitFile))
		}
		upstreamLanguage := code.LanguageVersion(pass, node)
		if kitLanguage := staticcheck_report.LanguageVersion(context, node); upstreamLanguage != kitLanguage {
			mismatch("LanguageVersion", node, upstreamLanguage, kitLanguage)
		}
		upstreamStandardLibrary := versionOutcome(func() string { return code.StdlibVersion(pass, node) })
		kitStandardLibrary := versionOutcome(func() string {
			return staticcheck_report.StandardLibraryVersion(context, node)
		})
		if upstreamStandardLibrary != kitStandardLibrary {
			mismatch("StandardLibraryVersion", node, fmt.Sprintf("%+v", upstreamStandardLibrary),
				fmt.Sprintf("%+v", kitStandardLibrary))
		}
		upstreamHasRange := report.HasRange(node)
		if kitHasRange := staticcheck_report.HasRange(node); upstreamHasRange != kitHasRange {
			mismatch("HasRange", node, fmt.Sprint(upstreamHasRange), fmt.Sprint(kitHasRange))
		}
		compared.met[map[bool]string{true: "HasRange true", false: "HasRange false"}[upstreamHasRange]]++
		upstreamDisplay := report.DisplayPosition(pass.Fset, node.Pos())
		if kitDisplay := staticcheck_report.DisplayPosition(pass.Fset, node.Pos()); upstreamDisplay != kitDisplay {
			mismatch("DisplayPosition", node, upstreamDisplay.String(), kitDisplay.String())
		}
		_, inGeneratedFile := generatedFiles[pass.Fset.PositionFor(node.Pos(), false).Filename]

		for _, set := range optionSets(pass, node) {
			message := fmt.Sprintf("%s at %T", set.name, node)
			upstream := outcomeOf(func() []analysis.Diagnostic {
				upstreamReported = []analysis.Diagnostic{}
				report.Report(&upstreamPass, node, message, set.upstream...)
				return upstreamReported
			})
			kit := outcomeOf(func() []analysis.Diagnostic {
				kitReported = []analysis.Diagnostic{}
				staticcheck_report.Report(context, node, message, set.kit...)
				return kitReported
			})
			compared.calls++
			if !reflect.DeepEqual(upstream, kit) {
				mismatch(set.name, node, fmt.Sprintf("%+v", upstream), fmt.Sprintf("%+v", kit))
			}
			if upstream.panicked != "" {
				continue
			}
			kept := map[bool]string{true: "kept", false: "withheld"}[len(upstream.diagnostics) > 0]
			if set.name == "FilterGenerated" && inGeneratedFile {
				compared.met["FilterGenerated in a generated file, "+kept]++
			}
			if set.name == "ShortRange" {
				compared.met["ShortRange on "+shapeOf(node)]++
			}
			if set.gate != "" {
				compared.met[set.name]++
				compared.met[set.gate+" "+kept]++
			}
		}
	}

	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if node != nil {
				compareAt(node, "")
			}
			return true
		})
	}
	for function := range built.SourceFunctions() {
		compareAt(function, "")
		for _, parameter := range function.Parameters {
			compareAt(parameter, "")
		}
		for _, block := range function.Blocks {
			for _, instruction := range block.Instructions {
				compareAt(instruction, map[bool]string{
					true:  "an instruction with a source",
					false: "an instruction with no source",
				}[instruction.Source() != nil])
			}
		}
	}
	for _, object := range pass.TypesInfo.Defs {
		if object != nil {
			compareAt(object, "a types.Object, which has no end")
		}
	}
	return compared, nil
}

// optionSets are the ways both reporters are called at node: plainly, short, filtering generated files, with fixes
// and related information before and after ShortRange, and under every gate at every version, each named for how the
// version stands to the node's, below, at or above it, so the test can tell all three were met.
func optionSets(pass *analysis.Pass, node staticcheck_report.Positioner) []optionSet {
	fix := analysis.SuggestedFix{
		Message:   "planted",
		TextEdits: []analysis.TextEdit{{Pos: node.Pos(), End: node.Pos(), NewText: []byte("planted")}},
	}
	sets := []optionSet{
		{"plain", "", nil, nil},
		{
			"ShortRange",
			"",
			[]report.Option{report.ShortRange()},
			[]staticcheck_report.Option{staticcheck_report.ShortRange()},
		},
		{
			"FilterGenerated",
			"",
			[]report.Option{report.FilterGenerated()},
			[]staticcheck_report.Option{staticcheck_report.FilterGenerated()},
		},
		{
			"ShortRange and FilterGenerated",
			"",
			[]report.Option{report.ShortRange(), report.FilterGenerated()},
			[]staticcheck_report.Option{staticcheck_report.ShortRange(), staticcheck_report.FilterGenerated()},
		},
		{
			"Fixes and Related around ShortRange",
			"",
			[]report.Option{
				report.Fixes(fix), report.Related(node, "full"), report.ShortRange(), report.Related(node, "short"),
				report.Fixes(fix),
			},
			[]staticcheck_report.Option{
				staticcheck_report.Fixes(fix),
				staticcheck_report.Related(node, "full"),
				staticcheck_report.ShortRange(),
				staticcheck_report.Related(node, "short"),
				staticcheck_report.Fixes(fix),
			},
		},
	}
	// A node outside the package's files has no standard library version, and both reporters panic at it under any
	// options, which the plain set already compares.
	standardLibraryVersion := versionOutcome(func() string { return code.StdlibVersion(pass, node) })
	if standardLibraryVersion.panicked != "" {
		return sets
	}
	versions := map[bool]string{true: code.LanguageVersion(pass, node), false: standardLibraryVersion.version}
	for _, each := range gates {
		nodeVersion := versions[each.language]
		for _, gateVersion := range append(slices.Clone(gateVersions), nodeVersion) {
			if gateVersion == "" {
				continue
			}
			relation := map[int]string{-1: "below", 0: "at", 1: "above"}[version.Compare(gateVersion, nodeVersion)]
			sets = append(sets, optionSet{
				fmt.Sprintf("%s %s", each.name, relation),
				each.name,
				[]report.Option{each.upstream(gateVersion)},
				[]staticcheck_report.Option{each.kit(gateVersion)},
			})
		}
	}
	return sets
}

// requiredLabels are what the fixtures must meet for the comparison to prove each case: every shape shortRange
// cuts and the ones it leaves whole, instructions with and without a source, HasRange answering each way, an object,
// each gate below, at and above the node's version and both withholding and keeping, and the generated filter
// withholding in a generated file and keeping in one whose line directives move its findings into another Go file.
func requiredLabels() []string {
	labels := []string{
		"an instruction with a source",
		"an instruction with no source",
		"a types.Object, which has no end",
		"FilterGenerated in a generated file, withheld",
		"FilterGenerated in a generated file, kept",
		"HasRange true",
		"HasRange false",
	}
	for _, shape := range []string{
		"File", "CaseClause", "CommClause", "DeferStmt", "ExprStmt", "ForStmt with a post statement",
		"ForStmt with a condition", "ForStmt with an initializer alone", "ForStmt bare", "FuncDecl", "FuncLit",
		"GoStmt of a function literal", "GoStmt of another call", "IfStmt", "RangeStmt", "SelectStmt",
		"SwitchStmt with a tag", "SwitchStmt with an initializer alone", "SwitchStmt bare", "TypeSwitchStmt",
		"CallExpr", "SelectorExpr", "an instruction", "a Function",
	} {
		labels = append(labels, "ShortRange on "+shape)
	}
	for _, each := range gates {
		labels = append(labels, each.name+" below", each.name+" at", each.name+" above", each.name+" kept",
			each.name+" withheld")
	}
	return labels
}

// shapeOf names node's shape as requiredLabels does, for the shapes shortRange tells apart.
func shapeOf(node staticcheck_report.Positioner) string {
	switch node := node.(type) {
	case *ast.ForStmt:
		switch {
		case node.Post != nil:
			return "ForStmt with a post statement"
		case node.Cond != nil:
			return "ForStmt with a condition"
		case node.Init != nil:
			return "ForStmt with an initializer alone"
		default:
			return "ForStmt bare"
		}
	case *ast.GoStmt:
		if _, isLiteral := ast.Unparen(node.Call.Fun).(*ast.FuncLit); isLiteral {
			return "GoStmt of a function literal"
		}
		return "GoStmt of another call"
	case *ast.SwitchStmt:
		switch {
		case node.Tag != nil:
			return "SwitchStmt with a tag"
		case node.Init != nil:
			return "SwitchStmt with an initializer alone"
		default:
			return "SwitchStmt bare"
		}
	case *intermediate_representation.Function:
		return "a Function"
	case intermediate_representation.Instruction:
		return "an instruction"
	case ast.Node:
		return reflect.TypeOf(node).Elem().Name()
	default:
		return fmt.Sprintf("%T", node)
	}
}

// outcomeOf runs call, which reports and returns what it reported, and is what it reported and what it panicked
// with. A call that panics has reported nothing, since both reporters panic before they report.
func outcomeOf(call func() []analysis.Diagnostic) (result outcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = outcome{diagnostics: []analysis.Diagnostic{}, panicked: fmt.Sprint(recovered)}
		}
	}()
	return outcome{diagnostics: call()}
}

// fileDescribed names file by where it starts, or says there is none.
func fileDescribed(pass *analysis.Pass, file *ast.File) string {
	if file == nil {
		return "no file"
	}
	return pass.Fset.Position(file.Pos()).String()
}

// versionOutcome runs call, which reads a version, and is the version it read or what it panicked with.
func versionOutcome(call func() string) (result versionRead) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result.panicked = fmt.Sprint(recovered)
		}
	}()
	return versionRead{version: call()}
}
