package rule

import (
	"flag"
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// plantedPass is a pass over an empty package whose results hold one for plantedAnalyzer and one for
// optionedAnalyzer.
func plantedPass() *analysis.Pass {
	return &analysis.Pass{
		Pkg:       types.NewPackage("example.com/planted", "planted"),
		TypesInfo: &types.Info{},
		Fset:      token.NewFileSet(),
		ResultOf: map[*analysis.Analyzer]any{
			plantedAnalyzer:  "planted's result",
			optionedAnalyzer: "optioned's result",
		},
	}
}

// generatedNone judges no file generated, for contexts whose tests read no file.
func generatedNone(string) bool {
	return false
}

// Report hands the sink the name the check was registered under, so a check never names its rule.
func TestContextReportStampsTheRegisteredRuleName(test *testing.T) {
	test.Parallel()
	stamped := []string{}
	context := NewContext(Namespace+"planted", plantedPass(), nil, &flag.FlagSet{}, generatedNone,
		func(ruleName string, _ analysis.Diagnostic) {
			stamped = append(stamped, ruleName)
		})
	context.Report(analysis.Diagnostic{Message: "planted"})
	if len(stamped) != 1 || stamped[0] != Namespace+"planted" {
		test.Errorf("Report stamped %q, where %q was registered", stamped, Namespace+"planted")
	}
}

// plantedRange is a range whose End is past its Pos, so a report that drops either shows.
type plantedRange struct{}

func (plantedRange) Pos() token.Pos { return 7 }

func (plantedRange) End() token.Pos { return 19 }

// Reportf and ReportRangef report, called as a check ported from a pass calls them, every field of the diagnostic
// analysis.Pass's method of the same name reports for the same call, so moving a check off its pass changes none of
// its findings.
func TestContextReportsFormattedFindingsAsAPassDoes(test *testing.T) {
	test.Parallel()
	calls := []struct {
		name    string
		context func(context *Context)
		pass    func(pass *analysis.Pass)
	}{
		{
			name:    "Reportf with arguments",
			context: func(context *Context) { context.Reportf(7, "%s reads %d of %q", "planted", 3, "files") },
			pass:    func(pass *analysis.Pass) { pass.Reportf(7, "%s reads %d of %q", "planted", 3, "files") },
		},
		{
			name:    "Reportf without arguments",
			context: func(context *Context) { context.Reportf(7, "planted reads nothing") },
			pass:    func(pass *analysis.Pass) { pass.Reportf(7, "planted reads nothing") },
		},
		{
			name: "ReportRangef with arguments",
			context: func(context *Context) {
				context.ReportRangef(plantedRange{}, "%s reads %d of %q", "planted", 3, "files")
			},
			pass: func(pass *analysis.Pass) {
				pass.ReportRangef(plantedRange{}, "%s reads %d of %q", "planted", 3, "files")
			},
		},
	}
	for _, call := range calls {
		test.Run(call.name, func(test *testing.T) {
			test.Parallel()
			passReported := []analysis.Diagnostic{}
			pass := plantedPass()
			pass.Report = func(diagnostic analysis.Diagnostic) {
				passReported = append(passReported, diagnostic)
			}
			call.pass(pass)
			contextReported := []analysis.Diagnostic{}
			context := NewContext(Namespace+"planted", plantedPass(), nil, &flag.FlagSet{}, generatedNone,
				func(_ string, diagnostic analysis.Diagnostic) {
					contextReported = append(contextReported, diagnostic)
				})
			call.context(context)
			if len(passReported) != 1 || !reflect.DeepEqual(contextReported, passReported) {
				test.Errorf("the context reported %#v, where the pass reported %#v", contextReported, passReported)
			}
		})
	}
}

// ResultOf hands a check the result of an analyzer its listener requires, and panics naming one it does not, even
// when the pass holds that one's result because another listener required it.
func TestContextResultOfReadsOnlyWhatTheListenerRequires(test *testing.T) {
	test.Parallel()
	context := NewContext(Namespace+"planted", plantedPass(), []*analysis.Analyzer{plantedAnalyzer},
		&flag.FlagSet{}, generatedNone, func(string, analysis.Diagnostic) {
			test.Error("a context that reports nothing handed the sink a finding")
		})
	if result := context.ResultOf(plantedAnalyzer); result != "planted's result" {
		test.Errorf("ResultOf read %v for a required analyzer, where planted's result was expected", result)
	}
	recovered := panicOf(func() { context.ResultOf(optionedAnalyzer) })
	if !strings.Contains(recovered, "reads the result of analyzer optioned, which its listener does not require") {
		test.Errorf("ResultOf of an analyzer the listener does not require panicked with %q", recovered)
	}
}

// Option reads the rule's own analyzer's flag as the settings set it, and panics naming an option it has no flag
// for.
func TestContextOptionReadsTheRulesOwnFlag(test *testing.T) {
	test.Parallel()
	options := &flag.FlagSet{}
	options.String("readsFile", "", "the file a rule under test would read")
	if setError := options.Set("readsFile", "reads.txt"); setError != nil {
		test.Fatal(setError)
	}
	context := NewContext(Namespace+"planted", plantedPass(), nil, options, generatedNone,
		func(string, analysis.Diagnostic) {
			test.Error("a context that reports nothing handed the sink a finding")
		})
	if value := context.Option("readsFile").String(); value != "reads.txt" {
		test.Errorf("Option read %q, where the flag was set to reads.txt", value)
	}
	recovered := panicOf(func() { context.Option("readsFiles") })
	if !strings.Contains(recovered, "reads option readsFiles, which its analyzer has no flag for") {
		test.Errorf("Option of a name with no flag panicked with %q", recovered)
	}
}

// State is made on first use and the same on every use after, each context has its own, and reading it as a second
// type panics naming both.
func TestStateIsTheContextsOwn(test *testing.T) {
	test.Parallel()
	newContext := func() *Context {
		return NewContext(Namespace+"planted", plantedPass(), nil, &flag.FlagSet{}, generatedNone,
			func(string, analysis.Diagnostic) {
				test.Error("a context that reports nothing handed the sink a finding")
			})
	}
	first, second := newContext(), newContext()
	*State[[]string](first) = append(*State[[]string](first), "gathered")
	if gathered := *State[[]string](first); len(gathered) != 1 {
		test.Errorf("the state read back as %q, where what was gathered was expected", gathered)
	}
	if gathered := *State[[]string](second); len(gathered) != 0 {
		test.Errorf("a second context's state holds %q, gathered on the first", gathered)
	}
	recovered := panicOf(func() { State[int](first) })
	if !strings.Contains(recovered, "reads its state as *int, where it was made as *[]string") {
		test.Errorf("reading the state as a second type panicked with %q", recovered)
	}
}

// panicOf is what call panicked with, spelled, or empty when it returned.
func panicOf(call func()) (recovered string) {
	defer func() {
		if value := recover(); value != nil {
			recovered = fmt.Sprint(value)
		}
	}()
	call()
	return ""
}
