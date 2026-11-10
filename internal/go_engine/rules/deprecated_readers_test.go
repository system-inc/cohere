package rules

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/deprecated_use"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// deprecatedReaders are the registered rules whose requirements reach staticcheck's deprecated facts, by name, each
// with the path its requirements take there, following a dispatcher only through the listener's own rule, as
// pathToBuildir does.
func deprecatedReaders(registered []rule.Rule) map[string][]string {
	readers := map[string][]string{}
	for _, checked := range registered {
		if path := pathToAnalyzer(checked.Analyzer, deprecated.Analyzer); path != nil {
			readers[checked.Name] = path
		}
	}
	return readers
}

// pathToAnalyzer is the names of the analyzers from start to target along its requirements, both ends included, or
// nil when none reaches it, skipping a dispatcher's requirements for its listeners (see pathToBuildir).
func pathToAnalyzer(start *analysis.Analyzer, target *analysis.Analyzer) []string {
	visited := map[*analysis.Analyzer]bool{}
	var walk func(analyzer *analysis.Analyzer) []string
	walk = func(analyzer *analysis.Analyzer) []string {
		if visited[analyzer] {
			return nil
		}
		visited[analyzer] = true
		if analyzer == target {
			return []string{analyzer.Name}
		}
		for _, required := range analyzer.Requires {
			if dispatch.ListenerRequirement(analyzer, required) {
				continue
			}
			if rest := walk(required); rest != nil {
				return append([]string{analyzer.Name}, rest...)
			}
		}
		return nil
	}
	return walk(start)
}

// TestOnlyTheDeprecationRuleReadsTheDeprecatedFacts holds the deprecated facts' one reader to SA1019 (#b2t9xmf).
// Their analyzer gathers every fact of the import closure through AllObjectFacts, which no fact read records; SA1019
// records each question it asks of that result instead, so another reader would read past the record. The check also
// runs over a planted rule, to prove it can fail and that it blames that rule alone.
func TestOnlyTheDeprecationRuleReadsTheDeprecatedFacts(test *testing.T) {
	test.Parallel()

	for _, problem := range deprecatedReaderProblems(registry) {
		test.Error(problem)
	}
	planted := &analysis.Analyzer{Name: "planted_deprecated_reader", Doc: "planted reads the deprecated facts",
		Requires: []*analysis.Analyzer{deprecated.Analyzer}, Run: func(*analysis.Pass) (any, error) { return nil, nil }}
	problems := deprecatedReaderProblems(append(slices.Clone(registry),
		rule.Rule{Name: "cohere-go/planted-deprecated-reader", Analyzer: planted}))
	expected := "cohere-go/planted-deprecated-reader requires the deprecated facts through planted_deprecated_reader " +
		"-> " + deprecated.Analyzer.Name
	if len(problems) != 1 || !strings.HasPrefix(problems[0], expected) {
		test.Errorf("want the planted rule alone blamed, %q, got %q", expected, problems)
	}
}

// deprecatedReaderProblems is, for each of registered but SA1019 whose requirements reach the deprecated facts, the
// path they take there, and a problem when SA1019 itself no longer reaches them, so the guard never passes by reading
// nothing.
func deprecatedReaderProblems(registered []rule.Rule) []string {
	readers := deprecatedReaders(registered)
	problems := []string{}
	for _, name := range slices.Sorted(maps.Keys(readers)) {
		if readers[name][0] != deprecated_use.Analyzer.Name {
			problems = append(problems, fmt.Sprintf("%s requires the deprecated facts through %s, and SA1019 is "+
				"their only reader, since only it records what it asks of them", name,
				strings.Join(readers[name], " -> ")))
		}
	}
	if !slices.ContainsFunc(slices.Collect(maps.Values(readers)), func(path []string) bool {
		return path[0] == deprecated_use.Analyzer.Name
	}) {
		problems = append(problems, "SA1019 no longer reaches the deprecated facts, so the guard reads nothing")
	}
	return problems
}
