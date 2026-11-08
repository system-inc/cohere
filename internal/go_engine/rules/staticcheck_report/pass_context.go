package staticcheck_report

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// PassContext adapts a plain analyzer's pass to rule.Context, for a check run outside the engine's dispatcher, as
// the kits' own tests run theirs: the context the dispatcher would build for the check, built over the pass instead,
// named rule.Namespace followed by the analyzer's name, reading the results and flags of the analyzer itself, its
// findings handed to pass.Report. A rule in the registry never comes through here; the dispatcher builds its context.
// The analyzer must require generated.Analyzer, the staticcheck pass IsGenerated reads, the very one staticcheck's
// checks require, so the two share one run per package; one that does not is an error naming it, never a context
// that judges no file generated.
func PassContext(pass *analysis.Pass) (*rule.Context, error) {
	generatedFiles, isGeneratedFiles := pass.ResultOf[generated.Analyzer].(map[string]generated.Generator)
	if !isGeneratedFiles {
		return nil, fmt.Errorf("%s reads no result of %s, which it must require to judge generated files",
			pass.Analyzer.Name, generated.Analyzer.Name)
	}
	return rule.NewContext(rule.Namespace+pass.Analyzer.Name, pass, pass.Analyzer.Requires, &pass.Analyzer.Flags,
		func(filename string) bool {
			// staticcheck's pass keys a file by the name it was parsed from, and Report asks by the name a finding
			// is displayed in, the two differing only under a line directive naming another Go file, where
			// staticcheck's lookup misses too.
			_, isGenerated := generatedFiles[filename]
			return isGenerated
		},
		func(_ string, diagnostic analysis.Diagnostic) {
			pass.Report(diagnostic)
		},
	), nil
}
