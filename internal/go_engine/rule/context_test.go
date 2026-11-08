package rule

import (
	"flag"
	"fmt"
	"go/token"
	"go/types"
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
