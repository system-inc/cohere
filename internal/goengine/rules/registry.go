// Package rules is the list of rules the Go engine runs: one place that names every rule, so the engine,
// `--rules` and the docs all read the same list.
package rules

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/suite/vet"

	"github.com/system-inc/cohere/internal/goengine/rule"
)

// syntaxOnly names the vet passes that read no type information, decided by reading each pass's source at
// x/tools v0.49.0. Every other pass in vet's suite reads pass.TypesInfo. A pass missing from this table
// counts as type aware, so a pass a newer x/tools adds is never described as running without types before
// someone has read it.
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
}

// All is every rule the Go engine knows, in the order `--rules` lists them before sorting.
//
// vet's suite comes first, read from vet.Suite rather than retyped, so `cohere` in a Go repository is
// never weaker than `go vet`, and a newer x/tools that adds a pass adds its rule here with no edit.
func All() []rule.Rule {
	registered := []rule.Rule{}
	for _, analyzer := range vet.Suite {
		registered = append(registered, vetRule(analyzer))
	}
	return registered
}

// vetRule is the rule that runs one of x/tools' passes under its own name.
func vetRule(analyzer *analysis.Analyzer) rule.Rule {
	return rule.Rule{
		Name:         rule.Namespace + analyzer.Name,
		Origin:       rule.OriginVet,
		UpstreamName: analyzer.Name,
		TypeAware:    !syntaxOnly[analyzer.Name],
		Analyzer:     analyzer,
	}
}
