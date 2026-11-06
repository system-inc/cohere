package goengine

// The Go adapter of the text rules' conformance corpus (#4b337jk), beside the TypeScript one
// (internal/lint/rules/textrules_conformance_test.go) and the Swift one. It lives in this package rather than in
// the rules tree because a case's settings and options must reach its rule the way a project's do, through
// settings.Load and configureRules, which this package keeps unexported.
//
// Each case runs as the engine runs a project: a module of its own on disk, loaded and type-checked by
// load.Packages, and linted by check.Run. So the adapter sees what the engine reports, the engine's drop of a
// house rule's findings in a file go/ast.IsGenerated accepts included (check.go, generatedFiles). Whether that
// drop is right is a separate engine question, for @system_cohere_go (textrules-decisions.md, "Whole-file drop
// in Go"); this adapter measures it rather than ruling on it.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/conformance/textrules"
	"github.com/system-inc/cohere/internal/goengine/check"
	"github.com/system-inc/cohere/internal/goengine/load"
	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/rules"
	"github.com/system-inc/cohere/internal/goengine/rules/concurrency_no_buffered_wait_delay"
	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_long_file"
	"github.com/system-inc/cohere/internal/goengine/settings"
)

// textRulesOwnAnalyzers builds an analyzer of its own for each rule whose package exports a constructor, the rules
// with options. configureRules sets options on an analyzer's flags, and every case runs in this one process in
// parallel, so a case setting them on the registry's analyzer would set them for whichever case ran that rule next.
var textRulesOwnAnalyzers = map[string]func() *analysis.Analyzer{
	consistency_no_long_file.Name:           consistency_no_long_file.NewAnalyzer,
	concurrency_no_buffered_wait_delay.Name: concurrency_no_buffered_wait_delay.NewAnalyzer,
}

// textRulesModuleName is the module each case's source is written into.
const textRulesModuleName = "example.com/textrules"

// textRulesSettingsFileName is the file settings.Load reads beside go.mod when no path is named.
const textRulesSettingsFileName = "CohereSettings.json"

// textRulesGoVersion is the go line of the repository's own go.mod, which each case's module declares, so a case
// is type-checked at the language version the engine's own code is.
func textRulesGoVersion(test *testing.T) string {
	test.Helper()
	// The test runs in its package's directory, internal/goengine, two below the module root.
	goModPath := filepath.Join("..", "..", "go.mod")
	contents, readError := os.ReadFile(goModPath)
	if readError != nil {
		test.Fatalf("reading the repository's go.mod: %v", readError)
	}
	parsed, parseError := modfile.ParseLax(goModPath, contents, nil)
	if parseError != nil {
		test.Fatalf("parsing the repository's go.mod: %v", parseError)
	}
	if parsed.Go == nil {
		test.Fatal("the repository's go.mod has no go line")
	}
	return parsed.Go.Version
}

// textRulesRule is the registered rule a family names for Go, found by its name in the registry, with an analyzer
// of its own where its package can build one.
func textRulesRule(test *testing.T, corpus *textrules.Corpus, testCase textrules.Case) rule.Rule {
	test.Helper()
	entry, found := corpus.RuleFor(testCase.Family, textrules.EngineGo)
	if !found {
		test.Fatalf("the corpus has no family %s", testCase.Family)
	}
	if !entry.Runs() {
		test.Fatalf("family %s has no Go rule, and has a Go case", testCase.Family)
	}
	for _, registered := range rules.All() {
		if registered.Name != entry.Rule {
			continue
		}
		if newAnalyzer, hasConstructor := textRulesOwnAnalyzers[registered.Name]; hasConstructor {
			return withOwnAnalyzer(registered, newAnalyzer())
		}
		// A rule with no constructor keeps the registry's analyzer, which is safe while it has no flags to set.
		// One that grew flags without a constructor would have a case's options set where every case reads them.
		hasFlags := false
		registered.Analyzer.Flags.VisitAll(func(*flag.Flag) { hasFlags = true })
		if hasFlags && len(testCase.Options) > 0 {
			test.Fatalf("%s has options and no NewAnalyzer, so %s's options would be set on the analyzer every "+
				"case shares; give its package a constructor and add it to textRulesOwnAnalyzers",
				registered.Name, testCase.Id())
		}
		return registered
	}
	test.Fatalf("families.json names %s for Go in %s, and no rule is registered by that name", entry.Rule,
		testCase.Family)
	return rule.Rule{}
}

// writeTextRulesFile writes contents to a file under directory, making the folders above it.
func writeTextRulesFile(test *testing.T, directory string, name string, contents []byte) string {
	test.Helper()
	path := filepath.Join(directory, filepath.FromSlash(name))
	if directoryError := os.MkdirAll(filepath.Dir(path), 0o755); directoryError != nil {
		test.Fatalf("making the folder for %s: %v", name, directoryError)
	}
	if writeError := os.WriteFile(path, contents, 0o600); writeError != nil {
		test.Fatalf("writing %s: %v", name, writeError)
	}
	return path
}

// textRulesSettings is a CohereSettings.json whose go block runs the rule at error, with the case's options when
// it has any.
func textRulesSettings(test *testing.T, ruleName string, testCase textrules.Case) []byte {
	test.Helper()
	// A rule's entry is its severity alone, or `[severity, options]`, as settings.Load reads it.
	setting, severityError := json.Marshal(settings.SeverityError)
	if severityError != nil {
		test.Fatal(severityError)
	}
	if len(testCase.Options) > 0 {
		options, optionsError := json.Marshal(testCase.Options)
		if optionsError != nil {
			test.Fatal(optionsError)
		}
		var pairError error
		setting, pairError = json.Marshal([]json.RawMessage{setting, options})
		if pairError != nil {
			test.Fatal(pairError)
		}
	}
	file := textRulesSettingsFile{}
	file.Go.Rules = map[string]json.RawMessage{ruleName: setting}
	contents, encodeError := json.Marshal(file)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	return contents
}

// textRulesSettingsFile is the part of CohereSettings.json a case writes: the go block's rules, each a rule's
// entry as written.
type textRulesSettingsFile struct {
	Go struct {
		Rules map[string]json.RawMessage `json:"rules"`
	} `json:"go"`
}

// runTextRulesCase runs a case's rule over its Go source as the engine runs a project, and returns each finding of
// that rule in the case's file as a byte range of its source. A refusal is the engine refusing the case's options
// before anything ran, which is the case's failure rather than the adapter's. Anything that keeps the case from
// being measured at all, a load or type error or a crash, fails the test loudly, since it measures nothing.
func runTextRulesCase(
	test *testing.T,
	corpus *textrules.Corpus,
	goVersion string,
	testCase textrules.Case,
) (found []textrules.Range, refusal error) {
	test.Helper()
	subject := textRulesRule(test, corpus, testCase)
	rulesForCase := []rule.Rule{subject}

	// Resolved, so the file names the load reports are the ones written here.
	directory, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	writeTextRulesFile(test, directory, "go.mod", fmt.Appendf(nil, "module %s\n\ngo %s\n", textRulesModuleName,
		goVersion))
	writeTextRulesFile(test, directory, textRulesSettingsFileName, textRulesSettings(test, subject.Name, testCase))
	source := testCase.Sources[textrules.EngineGo]
	casePath := writeTextRulesFile(test, directory, testCase.FileName(textrules.EngineGo), []byte(source))

	ruleSettings, settingsError := settings.Load(directory, "", knownRuleNames(rulesForCase))
	if settingsError != nil {
		test.Fatalf("the adapter's own settings for %s are refused: %v", testCase.Id(), settingsError)
	}
	if optionsError := configureRules(ruleSettings, rulesForCase); optionsError != nil {
		return nil, optionsError
	}

	loaded, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", testCase.Id(), loadError)
	}
	for _, diagnostic := range loaded.Diagnostics {
		test.Errorf("%s does not compile, so it measures nothing: %s:%d:%d: %s", testCase.Id(), diagnostic.File,
			diagnostic.Line, diagnostic.Column, diagnostic.Message)
	}
	if len(loaded.Diagnostics) > 0 {
		test.FailNow()
	}
	// A file the build left out is a file no rule read, which would compare as clean.
	typeChecked := false
	for _, loadedPackage := range loaded.Packages {
		typeChecked = typeChecked || slices.Contains(loadedPackage.CompiledGoFiles, casePath)
	}
	if !typeChecked {
		test.Fatalf("the load never type-checked %s's file %s", testCase.Id(), casePath)
	}

	checked, runError := check.Run(loaded.Packages, rulesForCase, check.Options{})
	if runError != nil {
		test.Fatalf("running %s on %s: %v", subject.Name, testCase.Id(), runError)
	}
	for _, crash := range checked.Crashes {
		test.Errorf("%s crashed on %s: %s", subject.Name, testCase.Id(), crash.Error)
	}
	if len(checked.Crashes) > 0 {
		test.FailNow()
	}
	if withheld := checked.WithheldAsGenerated[subject.Name]; withheld > 0 {
		test.Logf("the engine withheld %d finding(s) of %s on %s as generated", withheld, subject.Name,
			testCase.Id())
	}

	lineStarts := textrules.LineStarts(source, textrules.EngineGo)
	offsetOf := func(line int, column int) int {
		if line < 1 || line > len(lineStarts) || column < 1 {
			test.Fatalf("%s reported %d:%d on %s, which is not in its %d lines", subject.Name, line, column,
				testCase.Id(), len(lineStarts))
		}
		return lineStarts[line-1] + column - 1
	}
	for _, finding := range checked.Findings {
		if finding.Rule != subject.Name || finding.File != casePath {
			continue
		}
		start := offsetOf(finding.Line, finding.Column)
		if finding.EndLine == 0 {
			found = append(found, textrules.Range{Start: start, NoEnd: true})
			continue
		}
		found = append(found, textrules.Range{Start: start, End: offsetOf(finding.EndLine, finding.EndColumn)})
	}
	return found, nil
}

// judgeTextRulesCase is a case's comparison: its findings against its expectation, or, where the engine refused
// the case's options, a verdict failure, since the rule never gave the verdict the case asks for. A control so
// refused fails its count too, as a rule that never ran reported nothing.
func judgeTextRulesCase(
	test *testing.T,
	corpus *textrules.Corpus,
	goVersion string,
	testCase textrules.Case,
) textrules.Result {
	test.Helper()
	found, refusal := runTextRulesCase(test, corpus, goVersion, testCase)
	if refusal != nil {
		return textrules.Result{
			Failure: textrules.KindVerdict,
			Detail: fmt.Sprintf("the engine refuses the case's options, so the rule never ran: %v",
				refusal),
		}
	}
	return textrules.Compare(testCase, textrules.EngineGo, found)
}

// Every Go case in the corpus, through the rule its family names, held to the case's expectation and the ledger: a
// failure needs a row of its kind, a pass must have none, and a control's count and start lines must hold whatever
// the ledger says.
func TestTextRulesConformanceGo(test *testing.T) {
	test.Parallel()

	corpus, loadError := textrules.Load()
	if loadError != nil {
		test.Fatal(loadError)
	}
	goVersion := textRulesGoVersion(test)
	cases := corpus.CasesFor(textrules.EngineGo)
	if len(cases) == 0 {
		test.Fatal("the corpus has no Go cases, so this adapter would be green over nothing")
	}

	// Counted after every case's comparison, so a case that returned early, or never ran, is missed by count. Read
	// in a cleanup, which runs once every parallel subtest has finished.
	var compared atomic.Int64
	test.Cleanup(func() {
		if got := compared.Load(); got != int64(len(cases)) {
			test.Errorf("compared %d Go cases, and the corpus has %d", got, len(cases))
		}
	})
	for _, testCase := range cases {
		test.Run(testCase.Id(), func(test *testing.T) {
			test.Parallel()
			result := judgeTextRulesCase(test, corpus, goVersion, testCase)
			compared.Add(1)
			if judgeError := corpus.Judge(textrules.EngineGo, testCase, result); judgeError != nil {
				test.Error(judgeError)
			}
		})
	}
}

// textRulesComparatorFamily is the family whose control the comparator's own test runs on.
const textRulesComparatorFamily = "warning-comments"

// The comparison can fail: the control's own findings, held to deliberately wrong expectations, fail with the kind
// each is wrong in, and held to exactly what the rule reported, pass. So a green run above is the comparator
// agreeing, not a comparator unable to disagree.
func TestTextRulesConformanceGoComparatorCanFail(test *testing.T) {
	test.Parallel()

	corpus, loadError := textrules.Load()
	if loadError != nil {
		test.Fatal(loadError)
	}
	// The warning-comments control, whose comment holds the word `fix`, a misplacement every engine's finding
	// there differs from.
	var control textrules.Case
	for _, testCase := range corpus.CasesFor(textrules.EngineGo) {
		if testCase.Control && testCase.Family == textRulesComparatorFamily {
			control = testCase
		}
	}
	if control.Name == "" {
		test.Fatalf("the corpus has no Go control in %s", textRulesComparatorFamily)
	}
	found, refusal := runTextRulesCase(test, corpus, textRulesGoVersion(test), control)
	if refusal != nil {
		test.Fatalf("the engine refuses the control %s's options: %v", control.Id(), refusal)
	}
	if len(found) == 0 {
		test.Fatalf("the rule reported nothing on the control %s", control.Id())
	}
	source := control.Sources[textrules.EngineGo]

	asReported := control
	asReported.Expect = nil
	for _, finding := range found {
		// A finding with no end covers no text, so no expectation could name exactly what it reported.
		if finding.NoEnd || finding.End <= finding.Start {
			test.Fatalf("the control's finding at %d covers no text, so it can't be held to itself", finding.Start)
		}
		text := source[finding.Start:finding.End]
		asReported.Expect = append(asReported.Expect, textrules.Span{Covers: &text, Occurrence: 1})
	}
	if result := textrules.Compare(asReported, textrules.EngineGo, found); !result.Passed() {
		test.Errorf("held to exactly what it reported, the control failed: %s", result.Detail)
	}

	clean := control
	clean.Expect = []textrules.Span{}
	if result := textrules.Compare(clean, textrules.EngineGo, found); result.Failure != textrules.KindVerdict {
		test.Errorf("expecting the control clean was not a verdict failure (%q): %s", result.Failure, result.Detail)
	}

	wrongWord := "fix"
	misplaced := control
	misplaced.Expect = []textrules.Span{{Covers: &wrongWord, Occurrence: 1}}
	if result := textrules.Compare(misplaced, textrules.EngineGo, found); result.Failure != textrules.KindSpan {
		test.Errorf("expecting the control over %q was not a span failure (%q): %s", wrongWord, result.Failure,
			result.Detail)
	}
}
