package rules

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Inputs is what a rule's findings may be a function of, declared for every rule so that cohere-go judges a rule per
// declaration across builds (#ewgh523) only where its findings really are per declaration, and a test holds each rule
// to what it declares (#h350vqr). A unit is one top-level declaration: a function with its body, a type, or a var or
// const spec. Nothing serializes the values, so they are spelled as the house spells string literal types.
//
// A rule's inputs live here, in a side table by its name, and not on rule.Rule (#qr19hvc): every plain run copies a
// rule.Rule through the registry, the settings and the checker, and two more fields there leaned a long pair toward one
// more garbage collection, though no plain run reads them. Only the registry test, the unit-isolation tier,
// cohere-dev's land gate and block 4's judgment read the table, each through DeclaredInputs, which parses it on first
// use, so a plain run never does.
type Inputs string

const (
	// InputsPerUnit is a rule whose finding in a unit is a function of that unit's syntax, its types, and the facts and
	// resolver answers it reads, never of another unit's body or initializer, so a unit every build judges
	// independent has the host's findings in each. A constant's value is part of what a unit names, and a same-package
	// callee's no-return is a fact the unit reads, though ctrlflow computes it in place (#3rcmc3h). The unit-isolation
	// test holds it to that by stubbing other units' bodies and comparing what the rule finds in the units it kept.
	InputsPerUnit Inputs = "PerUnit"
	// InputsFileSyntax is a rule that reads only its file's syntax, bytes and name, as comments, imports and build
	// constraints, and no types, so a file both builds compile gives every build the same findings and only a file one
	// build alone compiles is judged in that build.
	InputsFileSyntax Inputs = "FileSyntax"
	// InputsComposes is a package-wide rule, judging every unit of its package together: one whose judgment is a union
	// or a fixed point over per-unit pieces, as the inventory read each, or one that judges each unit behind a gate the
	// whole package decides, as an import or a name in any of its files, which a build's own file can flip for a unit
	// its difference never reaches (#zvwfe26). Until it is rewritten into its per-unit contribution and package-level
	// judgment, a package with a unit some build judges dependent is checked whole for it in that build, so it is never
	// judged per declaration on a promise it does not keep.
	InputsComposes Inputs = "Composes"
)

// ruleInputsTable is rule_inputs.txt, the one home of every rule's declaration. It is the table rather than each
// registration, since the table is what the inventory was read into, what the registry test holds every registered
// rule to having a line in, and what cohere-dev's land gate reads at two commits to find the rules a landing widens:
// with the declarations in the registrations too, each would have two homes that could drift.
//
//go:embed rule_inputs.txt
var ruleInputsTable string

// declaredInputs is the table parsed, once, on the first DeclaredInputs.
var declaredInputs = sync.OnceValues(func() (map[string]Declared, error) {
	return ParseInputs(ruleInputsTable)
})

// DeclaredInputs is every rule's declaration by its name, from rule_inputs.txt, or the error naming the first line that
// is malformed, which TestTheInputsTableParses keeps from ever landing.
func DeclaredInputs() (map[string]Declared, error) {
	return declaredInputs()
}

// ParseInputs is table, in rule_inputs.txt's form, by rule name: each line not blank or a comment is a rule's name, its
// inputs and, optionally, FullKind and then its scope (Declared.Scope, parseScope). A line that is not, names inputs
// none of the three are, or names a rule a second time is an error naming it, so a typo in the table fails rather
// than reading as a rule with no line.
func ParseInputs(table string) (map[string]Declared, error) {
	declared := map[string]Declared{}
	for index, text := range strings.Split(table, "\n") {
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) < 2 || len(fields) > 4 {
			return nil, fmt.Errorf("rule_inputs.txt:%d: %q is not a rule's name, its inputs and optionally FullKind "+
				"and its scope", index+1, text)
		}
		read := Declared{Inputs: Inputs(fields[1])}
		if !slices.Contains([]Inputs{InputsPerUnit, InputsFileSyntax, InputsComposes}, read.Inputs) {
			return nil, fmt.Errorf("rule_inputs.txt:%d: %q is none of the three inputs", index+1, fields[1])
		}
		rest := fields[2:]
		if len(rest) > 0 && rest[0] == "FullKind" {
			read.FullKind, rest = true, rest[1:]
		}
		if len(rest) > 1 {
			return nil, fmt.Errorf("rule_inputs.txt:%d: %q is not a rule's name, its inputs and optionally FullKind "+
				"and its scope", index+1, text)
		}
		if len(rest) == 1 {
			scope, scopeError := parseScope(rest[0], read.Inputs)
			if scopeError != nil {
				return nil, fmt.Errorf("rule_inputs.txt:%d: %q is not a rule's name, its inputs and optionally "+
					"FullKind and its scope: %w", index+1, text, scopeError)
			}
			read.Scope = scope
		}
		if _, isListed := declared[fields[0]]; isListed {
			return nil, fmt.Errorf("rule_inputs.txt:%d: %s has a line already", index+1, fields[0])
		}
		declared[fields[0]] = read
	}
	return declared, nil
}
