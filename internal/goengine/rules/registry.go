// Package rules is the list of rules the Go engine runs: one place that names every rule, so the engine,
// `--rules` and the docs all read the same list.
package rules

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"golang.org/x/tools/go/analysis/suite/vet"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/unused"

	"github.com/system-inc/cohere/internal/goengine/rule"
)

// syntaxOnly names the passes that read no type information, decided by reading each pass's source at
// x/tools v0.49.0 and staticcheck v0.8.1. Every other pass in vet's suite reads pass.TypesInfo, nilness and
// unusedwrite read the SSA form built from it, and staticcheck's checks read it through the helpers they
// share (analysis/code, pattern, buildir). A pass missing from this table counts as type aware, so a pass a
// newer release adds is never described as running without types before someone has read it.
var syntaxOnly = map[string]bool{
	// buildtag reads build constraint lines from the files' comments and text.
	"buildtag": true,
	// directive reads `//go:debug` lines from the files' comments and text.
	"directive": true,
	// framepointer reads assembly files, and of the package only its import path, which a load without
	// types also has.
	"framepointer": true,
	// unreachable walks the syntax tree for statements after a terminating one.
	"unreachable": true,
	// SA9009 reads comments for a directive indented out of effect.
	"SA9009": true,
}

// All is every rule the Go engine knows, in the order `--rules` lists them before sorting.
//
// vet's suite comes first, read from vet.Suite rather than retyped, so `cohere` in a Go repository is
// never weaker than `go vet`, and a newer x/tools that adds a pass adds its rule here with no edit. Then
// two of x/tools' own passes that vet leaves out, which find real bugs: a nil dereference or a comparison
// with nil whose answer is already known, and a write to a struct or array nothing reads again. Then
// staticcheck's SA and S checks under their own codes, and U1000 through an analyzer that reports what it
// finds. staticcheck's ST checks wait on a census of what they would find in cohere's own code, and its QF
// checks are quick fixes an editor offers, not findings.
func All() []rule.Rule {
	registered := []rule.Rule{}
	for _, analyzer := range vet.Suite {
		registered = append(registered, portedRule(analyzer, rule.OriginVet))
	}
	registered = append(registered, portedRule(nilness.Analyzer, rule.OriginTools))
	registered = append(registered, portedRule(unusedwrite.Analyzer, rule.OriginTools))
	for _, wrapped := range staticcheck.Analyzers {
		registered = append(registered, portedRule(wrapped.Analyzer, rule.OriginStaticcheck))
	}
	for _, wrapped := range simple.Analyzers {
		registered = append(registered, portedRule(wrapped.Analyzer, rule.OriginStaticcheck))
	}
	registered = append(registered, rule.Rule{
		Name:         rule.Namespace + unused.Analyzer.Analyzer.Name,
		Origin:       rule.OriginStaticcheck,
		UpstreamName: unused.Analyzer.Analyzer.Name,
		TypeAware:    true,
		Analyzer:     unusedReport,
	})
	return registered
}

// portedRule is the rule that runs an upstream analyzer under the name it has upstream.
func portedRule(analyzer *analysis.Analyzer, origin rule.Origin) rule.Rule {
	return rule.Rule{
		Name:         rule.Namespace + analyzer.Name,
		Origin:       origin,
		UpstreamName: analyzer.Name,
		TypeAware:    !syntaxOnly[analyzer.Name],
		Analyzer:     analyzer,
	}
}
