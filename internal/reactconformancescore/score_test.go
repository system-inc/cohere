// Package reactconformancescore runs verify's actual React rules against React's own error goldens.
//
// # Why this is a separate package
//
// `internal/reactconformance` owns the corpus, the expectation parser, and the categories. It
// deliberately depends on nothing in verify's rule engine, so that the instrument cannot be bent by
// the thing it measures — and so that its 0.3s suite stays cheap enough never to be gated.
//
// Running a real rule costs a TypeScript program build, which is roughly a second per fixture.
// Putting that in the same package would either make the instrument slow enough to gate, or push
// someone to skip the build and hand rules a nil checker, which is the vacuous-probe failure
// `ruletest.RunTyped` was written to prevent. So the expensive half lives here, alone.
//
// # What this proves that the categorisation cannot
//
// `reactconformance` can decide, from the corpus alone, which fixtures a shipped rule is
// responsible for. It cannot decide whether those rules are RIGHT, and a harness that only ever
// reported "94 addressable, 0 scored" would be a taxonomy wearing a scoreboard's clothes.
//
// This scores one rule end to end against real goldens, so the wiring from fixture to finding to
// verdict is demonstrated on real input rather than on constructed test doubles. `globals` was
// chosen because it is syntax-only: it needs no `@types/react`, so its score measures the rule
// rather than the corpus's missing imports.
package reactconformancescore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/program"
	"github.com/system-inc/verify/internal/reactconformance"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rules/react"
)

// fixtureRoot reaches into the sibling package's vendored corpus.
//
// One corpus with two readers, rather than a second copy that could drift from the first. A vendored
// tree that exists twice is a tree that disagrees with itself eventually, and the disagreement would
// show up as a score change nobody made.
const fixtureRoot = "../reactconformance/testdata/fixtures"

const tsConfig = `{
  "compilerOptions": {
    "target": "esnext",
    "module": "esnext",
    "moduleResolution": "bundler",
    "jsx": "preserve",
    "strict": false,
    "noEmit": true,
    "skipLibCheck": true,
    "allowJs": true
  },
  "include": ["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx"]
}`

// ruleUnderTest pairs a verify rule with the upstream rule name it implements.
type ruleUnderTest struct {
	Upstream string
	Rule     rule.Rule
}

// analyze runs one verify rule over one fixture and returns what it found, in the shape the
// conformance comparison expects.
//
// The message text a rule produces does not match React's prose, and it is not meant to: verify's
// messages were written for verify's users. So this maps a finding to the upstream message the
// fixture expects via the rule's message id, which is the same join upstream's own backend
// comparison makes. A rule whose id is not in the table reports nothing here rather than guessing,
// because a wrong join produces a confident false failure.
func analyze(t *testing.T, subject ruleUnderTest, fixture reactconformance.Fixture) (reactconformance.Result, error) {
	t.Helper()

	// The golden's location line names a `.ts` or `.tsx` file even when the input is `.js`, and the
	// extension decides how the parser reads JSX. Following the golden keeps the parse the same one
	// upstream made.
	name := fixture.Expected.Errors[0].File
	if name == "" {
		name = filepath.Base(fixture.Name)
	}
	name = filepath.Base(name)

	directory := t.TempDir()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(fixture.Source), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	configPath := filepath.Join(directory, "tsconfig.json")
	if err := os.WriteFile(configPath, []byte(tsConfig), 0o644); err != nil {
		t.Fatalf("writing the tsconfig: %v", err)
	}

	graph, err := program.Build(program.Options{
		ConfigFileName:   configPath,
		CurrentDirectory: directory,
		SingleThreaded:   true,
	})
	if err != nil {
		return reactconformance.Result{}, &reactconformance.ErrUnsupported{Reason: "the fixture does not parse: " + err.Error()}
	}

	projectFiles := graph.ProjectFiles()
	if len(projectFiles) == 0 {
		return reactconformance.Result{}, &reactconformance.ErrUnsupported{Reason: "the fixture produced no project files"}
	}

	var sourceFile *ast.SourceFile
	wanted := filepath.ToSlash(path)
	for _, candidate := range projectFiles {
		if filepath.ToSlash(candidate.FileName()) == wanted {
			sourceFile = candidate
		}
	}
	if sourceFile == nil {
		return reactconformance.Result{}, &reactconformance.ErrUnsupported{Reason: "the fixture is not in the built program"}
	}

	fileChecker, release := graph.CheckerForFile(context.Background(), sourceFile)
	defer release()
	if fileChecker == nil {
		// A nil checker would make a type-aware rule take its decline path and report nothing,
		// which would score as a failure it did not earn.
		return reactconformance.Result{}, &reactconformance.ErrUnsupported{Reason: "no type checker for the fixture"}
	}

	var diagnostics []rule.Diagnostic
	context := rule.Context{
		SourceFile:  sourceFile,
		Program:     graph.Program,
		TypeChecker: fileChecker,
		FileCache:   rule.NewFileCache(),
		Report: func(diagnostic rule.Diagnostic) {
			diagnostic.RuleName = subject.Rule.Name
			if diagnostic.SourceFile == nil {
				diagnostic.SourceFile = sourceFile
			}
			diagnostics = append(diagnostics, diagnostic)
		},
	}

	listeners := subject.Rule.Run(context, nil)
	if listeners != nil {
		walk(sourceFile.AsNode(), listeners)
	}

	result := reactconformance.Result{}
	for _, diagnostic := range diagnostics {
		message, found := upstreamMessageForId[diagnostic.Message.Id]
		if !found {
			continue
		}
		line, _ := scanner_GetLineAndCharacterOfPosition(sourceFile, diagnostic.Range.Pos())
		result.Errors = append(result.Errors, reactconformance.ReportedError{
			Heading: message.Heading,
			Message: message.Text,
			Line:    line + 1,
		})
	}
	return result, nil
}

// upstreamMessage is the golden-side text a verify message id corresponds to.
type upstreamMessage struct {
	Heading string
	Text    string
}

// upstreamMessageForId joins verify's message ids to React's message text.
//
// Only ids whose correspondence was checked against a real golden are here. An id that is absent
// reports nothing rather than being mapped by resemblance, because a wrong join is worse than a
// missing one: it produces a specific, confident, wrong failure.
var upstreamMessageForId = map[string]upstreamMessage{
	"globalReassignment": {
		Heading: "Error",
		Text:    "Cannot reassign variables declared outside of the component/hook",
	},
}

// walk visits every node, dispatching to the listeners registered for its kind.
func walk(node *ast.Node, listeners rule.Listeners) {
	if node == nil {
		return
	}
	if listener, found := listeners[node.Kind]; found {
		listener(node)
	}
	node.ForEachChild(func(child *ast.Node) bool {
		walk(child, listeners)
		return false
	})
}

// scanner_GetLineAndCharacterOfPosition converts an offset into a zero-based line and column.
//
// Written here rather than taken from the shim because the shim's spelling has moved before, and a
// scoring harness that stops compiling for a rename is worse than twelve lines of arithmetic.
func scanner_GetLineAndCharacterOfPosition(sourceFile *ast.SourceFile, position int) (line int, character int) {
	text := sourceFile.Text()
	if position > len(text) {
		position = len(text)
	}
	line = 0
	lastNewline := -1
	for index := 0; index < position; index++ {
		if text[index] == '\n' {
			line++
			lastNewline = index
		}
	}
	return line, position - lastNewline - 1
}

// TestGlobalsScoresAgainstReactsOwnGoldens is the end-to-end proof.
//
// It runs verify's real `react/globals` over every fixture in the corpus whose diagnostics all
// belong to upstream's `globals` rule, and requires the harness to produce a non-zero pass count.
//
// The assertion is a floor rather than an exact number, and that choice is deliberate in one
// direction only. An exact number would be the better assertion — it is what
// `TestAttributionIsAMeasurementNotAGuess` does — but it would pin the SPAN and LINE conventions of
// a rule that was never written against this corpus, turning any future span refinement into a
// failure here rather than where it belongs. A floor still fails for the thing this test exists to
// catch: a harness that cannot turn a real rule's real finding into a pass.
//
// What it proves, precisely: the join from corpus to fixture to program to rule to finding to
// comparison to verdict is closed, on real input, with a real type graph. Everything else in this
// package is categorisation, and categorisation cannot tell you it is wired to anything.
func TestGlobalsScoresAgainstReactsOwnGoldens(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a TypeScript program per fixture")
	}

	fixtures, err := reactconformance.Load(fixtureRoot)
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	subject := ruleUnderTest{Upstream: "globals", Rule: react.Globals}

	var scored []reactconformance.Fixture
	for _, fixture := range fixtures {
		if fixture.RequiresFlow() {
			continue
		}
		rules, complete := fixture.Rules()
		if !complete || len(rules) != 1 || rules[0] != subject.Upstream {
			continue
		}
		scored = append(scored, fixture)
	}

	// 11 flow-free fixtures whose every diagnostic is a `globals` one. Pinned so that a change in
	// the attribution shows up here as well as in its own test.
	//
	// Not 23. The wider figure counts every fixture whose diagnostics are all Globals INCLUDING the
	// twelve `error.todo-*` differential-testing fixtures, whose expectations are Todo diagnostics
	// from the Rust backend comparison rather than Globals findings. Selecting on the attributed
	// rule rather than on the filename is what keeps them out, and the gap between 11 and 23 is a
	// concrete measure of how wrong a filename join would have been for this one rule.
	if len(scored) != 11 {
		t.Errorf("selected %d pure globals fixtures, want 11", len(scored))
	}
	if len(scored) == 0 {
		t.Fatal("no fixtures selected, so this test would prove nothing")
	}

	counts := map[reactconformance.Verdict]int{}
	var detail []string
	for _, fixture := range scored {
		result, analyzeErr := analyze(t, subject, fixture)
		verdict := reactconformance.Classify(fixture, result, analyzeErr)
		counts[verdict.Verdict]++
		detail = append(detail, "  "+fixture.Name+": "+string(verdict.Verdict))
	}

	t.Logf("react/globals against React's own goldens: %d passed, %d failed, %d excluded as stated divergences, of %d",
		counts[reactconformance.VerdictPassed], counts[reactconformance.VerdictFailed],
		counts[reactconformance.VerdictStatedDivergence], len(scored))
	for _, line := range detail {
		t.Log(line)
	}

	// Zero failed is the assertion that matters, and it is a stronger claim than a pass count.
	//
	// Every one of these eleven either passes or lands in a category with a named, machine-checked
	// reason. A `failed` here would mean the rule disagreed with a golden on a fixture that is
	// inside its stated scope, which is the only outcome in this harness that is a defect in verify
	// rather than a fact about the corpus.
	if counts[reactconformance.VerdictFailed] != 0 {
		t.Errorf("react/globals failed %d fixtures inside its own stated scope", counts[reactconformance.VerdictFailed])
	}

	// And the honest half: today the rule passes none of them. Asserted so the zero is a recorded
	// measurement rather than something a reader has to infer from its absence — and so that the
	// day a scope decision closes one of these divergences, this test fails and someone updates the
	// number deliberately instead of the improvement passing unnoticed.
	if got := counts[reactconformance.VerdictPassed]; got != 0 {
		t.Errorf("react/globals now passes %d of React's own goldens, was 0; update this number and the statedDivergences entries it came from", got)
	}
	if got := counts[reactconformance.VerdictStatedDivergence]; got != 11 {
		t.Errorf("stated divergences among the pure globals fixtures = %d, want 11", got)
	}
}

// TestScoringHarnessCanFail is the other half of the calibration.
//
// A harness that scored everything as a pass would satisfy the floor above. This gives the same
// machinery a rule that reports nothing and requires the fixtures to fail, so pass and fail are
// both shown to be reachable through the real path rather than through constructed doubles.
func TestScoringHarnessCanFail(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a TypeScript program per fixture")
	}

	fixtures, err := reactconformance.Load(fixtureRoot)
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	silent := ruleUnderTest{Upstream: "globals", Rule: rule.Rule{
		Name: "probe-reports-nothing",
		Run:  func(rule.Context, any) rule.Listeners { return nil },
	}}

	checked := 0
	for _, fixture := range fixtures {
		if fixture.RequiresFlow() {
			continue
		}
		rules, complete := fixture.Rules()
		if !complete || len(rules) != 1 || rules[0] != "globals" {
			continue
		}
		result, analyzeErr := analyze(t, silent, fixture)
		verdict := reactconformance.Classify(fixture, result, analyzeErr)
		if verdict.Verdict == reactconformance.VerdictPassed {
			t.Errorf("%s: a rule that reports nothing scored a pass", fixture.Name)
		}
		checked++
		if checked >= 3 {
			break
		}
	}
	if checked == 0 {
		t.Fatal("checked nothing, so this proves nothing")
	}
}

// TestUpstreamMessageJoinIsNotByResemblance guards the id-to-message table.
//
// Every entry claims a verify message id means the same thing as an upstream message. That claim is
// checkable: the upstream text must be one the corpus actually contains, and it must attribute back
// to the rule the id belongs to. An entry that fails either half is a join made by resemblance, and
// it would manufacture failures on fixtures the rule never had a chance at.
func TestUpstreamMessageJoinIsNotByResemblance(t *testing.T) {
	fixtures, err := reactconformance.Load(fixtureRoot)
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	present := map[string]bool{}
	for _, fixture := range fixtures {
		for _, expectedError := range fixture.Expected.Errors {
			present[expectedError.Message] = true
		}
	}

	for id, message := range upstreamMessageForId {
		if !present[message.Text] {
			t.Errorf("%s maps to %q, which no golden in the corpus contains", id, message.Text)
		}
		category, found := reactconformance.CategoryForMessage(message.Text)
		if !found {
			t.Errorf("%s maps to a message with no category", id)
			continue
		}
		heading, _ := reactconformance.HeadingForCategory(category)
		if heading != message.Heading {
			t.Errorf("%s claims heading %q but category %s prints %q", id, message.Heading, category, heading)
		}
		if !strings.Contains(strings.ToLower(id), "global") {
			continue
		}
		if ruleName, _ := reactconformance.RuleForCategory(category); ruleName != "globals" {
			t.Errorf("%s attributes to rule %q rather than globals", id, ruleName)
		}
	}
}
