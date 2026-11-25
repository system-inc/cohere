package dispatch

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/rule"
)

// plantedStanding is a rule.StandingBodies a test builds by hand over the standing fixture: which of its functions
// stand, which rules are confined, and the host's findings on each file, the same for every rule but one they are
// withheld from.
type plantedStanding struct {
	// stands is the offset of the `func` of each standing function, by its file's base name.
	stands map[string][]int
	// confines is each confined rule, by name.
	confines map[string]bool
	// planted is the host's findings on each file, by its base name. A file with no entry is one the host holds no
	// findings for.
	planted map[string][]rule.InheritedFinding
	// withheld is each rule the host holds no findings of on a file though it holds other rules' there, by the rule's
	// name, a space and the file's base name.
	withheld map[string]bool
	// took records what each rule says it took.
	took *tookStanding
}

// tookStanding is what each rule told a plantedStanding it took, by the rule's name, a space and the file's base name:
// how many declarations stood, and how many of the host's findings it reported in them.
type tookStanding struct {
	lock   sync.Mutex
	byFile map[string][2]int
}

// standingFunctions is the offset of the `func` of each function named in functions, by the base name of the standing
// fixture's file declaring it, read from the fixture's own source.
func standingFunctions(test *testing.T, functions map[string][]string) map[string][]int {
	test.Helper()
	offsets := map[string][]int{}
	for base, names := range functions {
		source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "src", "standing", base))
		if readError != nil {
			test.Fatalf("reading the fixture: %v", readError)
		}
		for _, name := range names {
			offset := bytes.Index(source, []byte("func "+name+"("))
			if offset < 0 {
				test.Fatalf("%s declares no function named %s", base, name)
			}
			offsets[base] = append(offsets[base], offset)
		}
	}
	return offsets
}

// everyStandingFunction is the functions that stand in most of the standing tests: two of first.go's three, and one of
// second.go's two.
var everyStandingFunction = map[string][]string{"first.go": {"stood", "stoodToo"}, "second.go": {"alsoStood"}}

// hostsFindingsOnStanding is the host's findings on the standing fixture, as most of the standing tests plant them. On
// first.go: one over the if in stood's body, with a fix, one at stood's doc comment, one at stoodToo's name, and two
// outside every standing function, at the if in reached and at kept. On second.go: one at the if in alsoStood's body,
// and one outside, at the if in alsoReached.
func hostsFindingsOnStanding() map[string][]rule.InheritedFinding {
	return map[string][]rule.InheritedFinding{
		"first.go": {
			{
				Line: 15, Column: 2, EndLine: 17, EndColumn: 3, MessageId: "hostIf", Message: "the host's if",
				Suggestions: []rule.InheritedSuggestion{{Message: "replace it", Edits: []rule.InheritedEdit{
					{Start: 30, End: 34, Text: "planted"},
				}}},
			},
			{Line: 13, Column: 1, MessageId: "hostDoc", Message: "the host's doc comment"},
			{Line: 28, Column: 2, MessageId: "hostReached", Message: "the host's if in reached"},
			{Line: 6, Column: 1, MessageId: "hostKept", Message: "the host's kept"},
			{Line: 34, Column: 6, MessageId: "hostName", Message: "the host's name"},
		},
		"second.go": {
			{Line: 4, Column: 2, MessageId: "hostIf", Message: "the host's if"},
			{Line: 11, Column: 2, MessageId: "hostReached", Message: "the host's if in reached"},
		},
	}
}

// hostsFindingsInStanding is what a rule confined on both of the fixture's files reports of hostsFindingsOnStanding
// with everyStandingFunction standing, as diagnosticPlaced reads each: those inside a standing function, doc comment
// included, in the order of the files and then as planted.
var hostsFindingsInStanding = []string{
	"first.go:15:2-17:3 hostIf the host's if, fix replace it: 30-34 planted",
	"first.go:13:1 hostDoc the host's doc comment",
	"first.go:34:6 hostName the host's name",
	"second.go:4:2 hostIf the host's if",
}

// newPlantedStanding is a plantedStanding in which the functions named in functions stand, by their file's base name,
// the rules named in confined are confined, and the host's findings are planted.
func newPlantedStanding(
	test *testing.T,
	functions map[string][]string,
	planted map[string][]rule.InheritedFinding,
	confined ...string,
) *plantedStanding {
	test.Helper()
	standing := &plantedStanding{stands: standingFunctions(test, functions), confines: map[string]bool{},
		planted: planted, withheld: map[string]bool{}, took: &tookStanding{byFile: map[string][2]int{}}}
	for _, ruleName := range confined {
		standing.confines[ruleName] = true
	}
	return standing
}

// options is the checker's options that hand every pass the planted standing bodies, whatever its package, with a
// dispatcher told from any other analyzer as the engine tells one, and each dispatcher a follower of
// checker.Options.StandingAfter where a test sets it.
func (standing *plantedStanding) options() *checker.Options {
	return &checker.Options{
		Failed:          rule.Failed,
		Tolerant:        IsDispatcher,
		FollowsStanding: IsDispatcher,
		Standing:        rule.Standing,
		StandingBodies:  func(string, any) rule.StandingBodies { return standing },
	}
}

// analyze runs analyzers over the standing fixture with the planted standing bodies handed to every pass.
func (standing *plantedStanding) analyze(test *testing.T, analyzers ...*analysis.Analyzer) *checker.Graph {
	test.Helper()
	graph, analyzeError := checker.Analyze(analyzers, loadFixture(test, "standing"), standing.options())
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	return graph
}

// Stands is whether a function planted as standing is declared at offset in the file with filename's base name.
func (standing *plantedStanding) Stands(filename string, offset int) bool {
	for _, planted := range standing.stands[filepath.Base(filename)] {
		if planted == offset {
			return true
		}
	}
	return false
}

// Confines is whether ruleName is planted as confined.
func (standing *plantedStanding) Confines(ruleName string) bool {
	return standing.confines[ruleName]
}

// Findings is the findings planted on filename's base name, and whether any entry is, none being for a rule they are
// withheld from there. Each edit names filename itself, or, when it names a file, that file beside filename.
func (standing *plantedStanding) Findings(ruleName string, filename string) ([]rule.InheritedFinding, bool) {
	planted, held := standing.planted[filepath.Base(filename)]
	if !held || standing.withheld[ruleName+" "+filepath.Base(filename)] {
		return nil, false
	}
	findings := []rule.InheritedFinding{}
	for _, finding := range planted {
		named := finding
		named.Suggestions = nil
		for _, suggestion := range finding.Suggestions {
			edits := []rule.InheritedEdit{}
			for _, edit := range suggestion.Edits {
				if edit.File == "" {
					edit.File = filename
				} else {
					edit.File = filepath.Join(filepath.Dir(filename), edit.File)
				}
				edits = append(edits, edit)
			}
			named.Suggestions = append(named.Suggestions, rule.InheritedSuggestion{Message: suggestion.Message,
				Edits: edits})
		}
		findings = append(findings, named)
	}
	return findings, true
}

// Took records declarations and findings as what the rule named ruleName took on filename's base name.
func (standing *plantedStanding) Took(ruleName string, filename string, declarations int, findings int) {
	standing.took.lock.Lock()
	defer standing.took.lock.Unlock()
	standing.took.byFile[ruleName+" "+filepath.Base(filename)] = [2]int{declarations, findings}
}

// told is what the rules told the planted standing bodies they took, a copy to read once the run is over.
func (standing *plantedStanding) told() map[string][2]int {
	standing.took.lock.Lock()
	defer standing.took.lock.Unlock()
	return maps.Clone(standing.took.byFile)
}
