// Package rules is the list of rules the Go engine runs: one place that names every rule, so the engine,
// `--rules` and the docs all read the same list.
package rules

import (
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"golang.org/x/tools/go/analysis/suite/vet"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
	"honnef.co/go/tools/unused"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/rules/correctness_require_checked_assertion"
	"github.com/system-inc/cohere/internal/goengine/rules/testing_no_machine_path"
	"github.com/system-inc/cohere/internal/goengine/rules/testing_require_parallel"
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
	// ST1001 reads import declarations for a dot import, and of a file only its name, to allow one in a test.
	"ST1001": true,
	// ST1015 reads the order of a switch's clauses for a default in the middle.
	"ST1015": true,
	// ST1018 reads string literals for control and format characters.
	"ST1018": true,
	// ST1019 reads import declarations for a package imported twice.
	"ST1019": true,
}

// stylecheckRuledOn is the ST checks ruled on for cohere, from a census of what each found in cohere's own
// code. ST1016 is among them although staticcheck leaves it off by default.
//
// Ruled off: ST1003, whose initialisms (`ID`, `JSON`) contradict the house's `Id` and `Json` and whose
// underscores are this repository's package names, and ST1005.
//
// Off until ruled: ST1000, ST1020, ST1021 and ST1022, the form of package and exported declarations'
// comments, and ST1023, a declared type the right-hand side would infer. Comment form and declared types
// are style no one has judged yet.
var stylecheckRuledOn = map[string]bool{
	"ST1001": true,
	"ST1006": true,
	"ST1008": true,
	"ST1011": true,
	"ST1012": true,
	"ST1013": true,
	"ST1015": true,
	"ST1016": true,
	"ST1017": true,
	"ST1018": true,
	"ST1019": true,
}

// All is every rule the Go engine knows, in the order `--rules` lists them before sorting.
//
// vet's suite comes first, read from vet.Suite rather than retyped, so `cohere` in a Go repository is
// never weaker than `go vet`, and a newer x/tools that adds a pass adds its rule here with no edit. Then
// two of x/tools' own passes that vet leaves out, which find real bugs: a nil dereference or a comparison
// with nil whose answer is already known, and a write to a struct or array nothing reads again. Then
// staticcheck's SA and S checks under their own codes, the ST checks ruled on, and U1000 through an
// analyzer that reports what it finds. staticcheck's QF checks are quick fixes an editor offers, not
// findings.
//
// An SA or S check staticcheck leaves off by default (its documentation's NonDefault) is left off here
// too, until it is ruled on: at staticcheck v0.8.1 that is SA9003, an empty if or else branch. And a check
// that makes the same judgment as a vet pass already registered is left out (duplicatesOfVet).
// duplicatesOfVet names each staticcheck check that judges exactly what a registered vet pass judges, and
// that pass. Both registered, one mistake is two findings, and turning the vet rule off in the settings, or
// withholding it with //cohere:ignore, would leave its twin reporting the very thing the reader just
// silenced. vet's pass is kept, since vet is what every Go programmer already runs.
var duplicatesOfVet = map[string]string{
	// Printf format verbs that do not match their arguments.
	"SA5009": "printf",
}

func All() []rule.Rule {
	registered := []rule.Rule{}
	for _, analyzer := range vet.Suite {
		registered = append(registered, portedRule(analyzer, rule.OriginVet))
	}
	registered = append(registered, portedRule(nilness.Analyzer, rule.OriginTools))
	registered = append(registered, portedRule(unusedwrite.Analyzer, rule.OriginTools))
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers) {
		if wrapped.Doc != nil && wrapped.Doc.NonDefault || duplicatesOfVet[wrapped.Analyzer.Name] != "" {
			continue
		}
		registered = append(registered, portedRule(wrapped.Analyzer, rule.OriginStaticcheck))
	}
	for _, wrapped := range stylecheck.Analyzers {
		if stylecheckRuledOn[wrapped.Analyzer.Name] {
			registered = append(registered, portedRule(wrapped.Analyzer, rule.OriginStaticcheck))
		}
	}
	registered = append(registered, rule.Rule{
		Name:         rule.Namespace + unused.Analyzer.Analyzer.Name,
		Origin:       rule.OriginStaticcheck,
		UpstreamName: unused.Analyzer.Analyzer.Name,
		TypeAware:    true,
		Analyzer:     unusedReport,
	})
	// The house rules come last, each from its own package, which holds its name, origin and analyzer
	// together so the registry restates none of them.
	registered = append(registered, House()...)
	return registered
}

// House is every house rule, in the order the registry lists them after the ported ones.
func House() []rule.Rule {
	return []rule.Rule{
		correctness_require_checked_assertion.Rule,
		testing_no_machine_path.Rule,
		testing_require_parallel.Rule,
	}
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
