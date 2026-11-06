// Package rule is what the Go engine knows about one of its rules beyond the analyzer that runs it: the
// name a finding carries, where the rule came from, and the upstream name a ported rule keeps.
package rule

import (
	"fmt"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Namespace opens every Go rule's name, as `cohere-swift/` opens every Swift rule's, so a finding line
// says which engine's rule it is before a reader gets to the rule.
const Namespace = "cohere-go/"

// Origin is where a rule came from. Its values are serialized, so they are spelled as swift/Rules.json
// spells its origins, lowercase, rather than in the house's PascalCase for string literal types.
type Origin string

const (
	// OriginHouse is a rule cohere wrote, named from policy/RuleNaming.json's closed lists.
	OriginHouse Origin = "house"
	// OriginVet is one of the analyzers cmd/vet runs, ported under its own name.
	OriginVet Origin = "vet"
	// OriginStaticcheck is one of staticcheck's checks, ported under its own name.
	OriginStaticcheck Origin = "staticcheck"
	// OriginTools is one of golang.org/x/tools' own analysis passes that cmd/vet leaves out, ported under its
	// own name.
	OriginTools Origin = "tools"
)

// Rule is one rule of the Go engine: an analyzer, plus the naming and provenance cohere adds to it.
//
// The analyzer is a plain *analysis.Analyzer so that vet's suite, staticcheck, analysistest and the facts
// analyzers share across packages all work unchanged. Everything cohere needs on top of that lives here,
// so no analyzer has to know it runs inside cohere.
type Rule struct {
	// Name is what a finding prints, `cohere-go/performance-require-parallel-test` or `cohere-go/printf`.
	Name string
	// Origin is where the rule came from, which is also what decides whether UpstreamName is required.
	Origin Origin
	// UpstreamName is the name a ported rule had where it came from, `printf` or `SA4006`, so a reader can
	// find its upstream documentation. A house rule has none, since it has no upstream.
	UpstreamName string
	// TypeAware is whether the rule reads type information, for the docs, as swift/Rules.json's typeAware.
	TypeAware bool
	// Analyzer is what runs. Its options are flags on Analyzer.Flags, which the engine sets from the
	// options object of the rule's entry in the settings.
	Analyzer *analysis.Analyzer
	// ConfiguredBy names the options without which the rule reports nothing, each a flag on Analyzer.Flags.
	// While any of them is empty the engine lists the rule in the lint record's rulesNotConfigured, so a rule
	// that cannot report reads as not configured rather than as quiet.
	ConfiguredBy []string
}

// Validate refuses a rule the registry should never hold, so a malformed rule fails a test at the
// registry rather than reaching a finding line as a name nobody can look up.
func (rule Rule) Validate() error {
	if rule.Name == "" {
		return fmt.Errorf("a rule has no name")
	}
	if !strings.HasPrefix(rule.Name, Namespace) {
		return fmt.Errorf(
			"rule %q does not start with %q, the namespace every Go rule's name opens with", rule.Name, Namespace,
		)
	}
	if rule.Name == Namespace {
		return fmt.Errorf("rule %q is the namespace alone, with no name after it", rule.Name)
	}
	if rule.Analyzer == nil {
		return fmt.Errorf("rule %q has no analyzer, so nothing would run", rule.Name)
	}
	switch rule.Origin {
	case OriginHouse:
		if rule.UpstreamName != "" {
			return fmt.Errorf(
				"rule %q is a house rule and names upstream %q; a house rule has no upstream",
				rule.Name, rule.UpstreamName,
			)
		}
	case OriginVet, OriginStaticcheck, OriginTools:
		if rule.UpstreamName == "" {
			return fmt.Errorf(
				"rule %q is ported from %s and names no upstream rule, so nobody can find what it ports",
				rule.Name, rule.Origin,
			)
		}
	default:
		return fmt.Errorf(
			"rule %q has origin %q, which is none of %q, %q, %q and %q",
			rule.Name, rule.Origin, OriginHouse, OriginVet, OriginStaticcheck, OriginTools,
		)
	}
	// A name with no flag behind it could never be set, so the rule would read as not configured forever.
	for _, option := range rule.ConfiguredBy {
		if rule.Analyzer.Flags.Lookup(option) == nil {
			return fmt.Errorf(
				"rule %q is configured by option %q, which its analyzer has no flag for", rule.Name, option,
			)
		}
	}
	return nil
}
