package differential

import (
	"context"
	"sort"
	"testing"

	"github.com/system-inc/cohere/internal/needs"
)

// A live check against a real cohere binary, skipped unless one is named.
//
// The parse has to be proven against actual output rather than a fixture, because a fixture is a
// copy of what I believed the format was, and the failure this guards against is precisely the
// format not matching that belief. Run with:
//
//	COHERE_BINARY=/tmp/cohere-head COHERE_TREE=~/Projects/ahra \
//	  go test -run TestCompiledRulesAgainstARealBinary ./internal/differential/
func TestCompiledRulesAgainstARealBinary(test *testing.T) {
	test.Parallel()
	binary := needs.OptIn(test, "COHERE_BINARY", "it checks the rule list parse against a real cohere build")
	tree := needs.OptIn(test, "COHERE_TREE", "the real cohere build the rule list parse is checked against runs there")
	explainFile := needs.OptIn(test, "COHERE_EXPLAIN_FILE", "the real cohere build writes its rule list there")

	names, compiledRulesOfError := CompiledRulesOf(context.Background(), GateCommand{
		Name: "cohere", Program: binary, Directory: tree,
	}, explainFile)
	if compiledRulesOfError != nil {
		test.Fatalf("could not read the rule list: %v", compiledRulesOfError)
	}

	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	test.Logf("%d rules: %v", len(sorted), sorted)

	// The count guard inside CompiledRulesOf already compared this against the binary's own
	// coverage line, so reaching here means they agreed. This asserts the population is plausible,
	// which catches the case where the binary reports a tiny number and the parse dutifully agrees.
	if len(names) < 10 {
		test.Fatalf("only %d rules parsed, which is too few to be a real build", len(names))
	}
}
