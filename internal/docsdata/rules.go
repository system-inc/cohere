package docsdata

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// Rules is rules.json: one row per registered rule, and a note per field saying where it comes from.
type Rules struct {
	SourceNotes map[string]string `json:"sourceNotes"`
	Count       int               `json:"count"`
	Rules       []RuleRow         `json:"rules"`
}

// RuleRow is one registered rule.
type RuleRow struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	// Namespace is the part of the name before its last slash, empty for an ESLint core rule or a house
	// rule registered bare.
	Namespace string `json:"namespace,omitempty"`
	Origin    string `json:"origin"`
	// UpstreamName is the rule's name in the plugin it was ported from, for a port. A rule registers
	// under its real upstream spelling, so for a port it is the registered name.
	UpstreamName string       `json:"upstreamName,omitempty"`
	TypeAware    bool         `json:"typeAware"`
	Options      *RuleOptions `json:"options,omitempty"`
	// MessageIds and FixKind are what the rule's own tests assert, read from its examples. A rule whose
	// tests assert no fix has no FixKind rather than "none": a fix no test shows is not a fix absent.
	MessageIds []string          `json:"messageIds,omitempty"`
	FixKind    string            `json:"fixKind,omitempty"`
	Sets       []RuleSetSeverity `json:"sets,omitempty"`
	Swift      *SwiftVerdict     `json:"swift,omitempty"`
}

// RuleOptions is what a rule's registration declares about its options.
type RuleOptions struct {
	// Elements is "one" for a rule decoding a single option element, "many" for one decoding upstream's
	// whole option list.
	Elements string `json:"elements"`
	// AnchorsPaths is a rule whose options name places on disk, resolved against the settings file.
	AnchorsPaths bool `json:"anchorsPaths,omitempty"`
	// Required is a rule that checks nothing without its options, so cohere refuses it bare.
	Required bool `json:"required,omitempty"`
}

// RuleSetSeverity is one carried set that turns the rule on, at the severity it resolves to.
type RuleSetSeverity struct {
	Set      string `json:"set"`
	Severity string `json:"severity"`
}

// SwiftVerdict is the Swift catalog's ruling on a TypeScript rule.
type SwiftVerdict struct {
	Verdict   string `json:"verdict"`
	SwiftRule string `json:"swiftRule,omitempty"`
}

var ruleSourceNotes = map[string]string{
	"name":         "the rule registry (internal/lint/registry), as `cohere --rules` lists it",
	"language":     "the registry the row comes from; only the TypeScript registry is exported today",
	"namespace":    "the registered name, before its last slash",
	"origin":       "the registered namespace, and for a bare name the Go package that registers it",
	"upstreamName": "the registered name, for a rule ported from a plugin or ESLint core",
	"typeAware":    "the rule's NeedsTypeChecker declaration",
	"options":      "the rule's registration: which decoder it carries and whether it requires options",
	"messageIds":   "the message ids the rule's own tests assert (examples/<rule>.json)",
	"fixKind":      "whether the rule's own tests show a fix, a suggestion, or both (examples/<rule>.json)",
	"sets":         "each carried rule set as the loader resolves it, at warn or error, top-level rules only",
	"swift":        "swift/HouseRuleVerdicts.json",
}

// namespaceOrigins names where each registered namespace's rules come from. A namespace missing here
// fails the build, so a new plugin's rules cannot publish with no origin.
var namespaceOrigins = map[string]string{
	"@typescript-eslint":                "typescript-eslint",
	"react":                             "react",
	"react-hooks":                       "react-hooks",
	"@next/next":                        "next",
	"better-tailwindcss":                "tailwind",
	"boundaries":                        "boundaries",
	"@eslint-community/eslint-comments": "eslint-comments",
	"base":                              "house",
	"structure":                         "house",
	"nexus":                             "house",
}

// houseOrigin is the origin of a rule this organization wrote rather than ported.
const houseOrigin = "house"

// corePackage is the Go package ESLint core's ports register from. A bare name registered anywhere else
// is a house rule.
const corePackage = "github.com/system-inc/cohere/internal/lint/rules/core"

// buildRules renders one row per registration, in name order.
func buildRules(inputs Inputs, sets []RuleSet) (Rules, error) {
	verdicts, err := swiftVerdicts(inputs.SwiftVerdicts)
	if err != nil {
		return Rules{}, err
	}
	enabledBy := map[string][]RuleSetSeverity{}
	for _, set := range sets {
		for _, setRule := range set.Rules {
			if setRule.Severity == configuration.SeverityOff.String() {
				continue
			}
			enabledBy[setRule.Name] = append(enabledBy[setRule.Name], RuleSetSeverity{Set: set.Name, Severity: setRule.Severity})
		}
	}

	rows := make([]RuleRow, 0, len(inputs.Registrations))
	for _, registration := range inputs.Registrations {
		name := registration.Rule.Name
		row := RuleRow{
			Name:      name,
			Language:  "TypeScript",
			TypeAware: registration.Rule.NeedsTypeChecker,
			Sets:      enabledBy[name],
		}
		if slash := strings.LastIndex(name, "/"); slash >= 0 {
			row.Namespace = name[:slash]
		}
		origin, err := originOf(row.Namespace, registeringPackage(registration.Rule))
		if err != nil {
			return Rules{}, fmt.Errorf("docsdata: %s: %w", name, err)
		}
		row.Origin = origin
		if origin != houseOrigin {
			row.UpstreamName = name
		}
		row.Options = optionsOf(registration)
		if examples, present := inputs.Examples[name]; present {
			row.MessageIds = examples.MessageIds()
			row.FixKind = examples.FixKind()
		}
		if verdict, present := verdicts[name]; present {
			row.Swift = &verdict
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(left, right int) bool { return rows[left].Name < rows[right].Name })
	return Rules{SourceNotes: ruleSourceNotes, Count: len(rows), Rules: rows}, nil
}

// originOf is where a rule comes from, by its namespace, or for a bare name by the package registering it.
func originOf(namespace string, packagePath string) (string, error) {
	if namespace == "" {
		if packagePath == corePackage {
			return "core", nil
		}
		return houseOrigin, nil
	}
	origin, known := namespaceOrigins[namespace]
	if !known {
		return "", fmt.Errorf("the namespace %q has no origin; add it to namespaceOrigins", namespace)
	}
	return origin, nil
}

// registeringPackage is the import path of the package whose code a rule's Run is, read off the function
// itself, so a bare house rule is told from an ESLint core port by where it lives rather than by a list.
func registeringPackage(subject rule.Rule) string {
	function := runtime.FuncForPC(reflect.ValueOf(subject.Run).Pointer())
	if function == nil {
		return ""
	}
	// A function's name is its import path, then a dot, then the symbol and any closure suffixes. The
	// path's last segment may hold dots of its own only in a domain, which comes before the last slash.
	name := function.Name()
	slash := strings.LastIndex(name, "/")
	dot := strings.Index(name[slash+1:], ".")
	if dot < 0 {
		return name
	}
	return name[:slash+1+dot]
}

// optionsOf reads what a registration declares about its options, or nil for a rule that takes none.
func optionsOf(registration rule.Registration) *RuleOptions {
	switch {
	case registration.DecodeOptionList != nil:
		return &RuleOptions{Elements: "many", Required: registration.RequiresOptions}
	case registration.DecodeAt != nil:
		return &RuleOptions{Elements: "one", AnchorsPaths: true, Required: registration.RequiresOptions}
	case registration.Decode != nil:
		return &RuleOptions{Elements: "one", Required: registration.RequiresOptions}
	}
	return nil
}

// swiftVerdicts reads the Swift catalog, keyed by TypeScript rule name.
func swiftVerdicts(catalog []byte) (map[string]SwiftVerdict, error) {
	var entries []struct {
		TypeScriptRule string  `json:"typeScriptRule"`
		Verdict        string  `json:"verdict"`
		SwiftRule      *string `json:"swiftRule"`
	}
	if err := json.Unmarshal(catalog, &entries); err != nil {
		return nil, fmt.Errorf("docsdata: reading the Swift verdict catalog: %w", err)
	}
	verdicts := make(map[string]SwiftVerdict, len(entries))
	for _, entry := range entries {
		verdict := SwiftVerdict{Verdict: entry.Verdict}
		if entry.SwiftRule != nil {
			verdict.SwiftRule = *entry.SwiftRule
		}
		verdicts[entry.TypeScriptRule] = verdict
	}
	return verdicts, nil
}
