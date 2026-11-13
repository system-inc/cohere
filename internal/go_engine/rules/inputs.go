package rules

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Inputs is what a rule's findings may be a function of, declared for every rule so that the engine judges a rule per
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
	// independent has the host's findings in each. The unit-isolation test holds it to that by stubbing other units'
	// bodies and comparing what the rule finds in the units it kept.
	InputsPerUnit Inputs = "PerUnit"
	// InputsFileSyntax is a rule that reads only its file's syntax, bytes and name, as comments, imports and build
	// constraints, and no types, so a file both builds compile gives every build the same findings and only a file one
	// build alone compiles is judged in that build.
	InputsFileSyntax Inputs = "FileSyntax"
	// InputsComposes is a package-wide rule, judging every unit of its package together, whose judgment is a union or a
	// fixed point over per-unit pieces, as the inventory read each. Until it is rewritten into its per-unit
	// contribution and package-level judgment, a package with a unit some build judges dependent is checked whole for
	// it in that build, so it is never judged per declaration on a promise it does not keep.
	InputsComposes Inputs = "Composes"
)

// Declared is one rule's declaration: its inputs, and whether it reads more of a named type than its name, directly or
// through a helper. FullKind is a rule that renders a whole type (TypeString, a type in a message), walks a struct's
// fields or an interface's methods, or asks identity, assignability, implementation or sizes. A unit that holds a
// struct only by name or by pointer stays independent of a build that changes only an unexported field the unit never
// selects, and only a rule without FullKind is sure to judge that unit alike in both builds; a rule with it is judged
// with every named type it sees read in full.
type Declared struct {
	Inputs   Inputs
	FullKind bool
}

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
// inputs and, optionally, FullKind. A line that is not, names inputs none of the three are, or names a rule a second
// time is an error naming it, so a typo in the table fails rather than reading as a rule with no line.
func ParseInputs(table string) (map[string]Declared, error) {
	declared := map[string]Declared{}
	for index, text := range strings.Split(table, "\n") {
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) < 2 || len(fields) > 3 || len(fields) == 3 && fields[2] != "FullKind" {
			return nil, fmt.Errorf("rule_inputs.txt:%d: %q is not a rule's name, its inputs and optionally FullKind",
				index+1, text)
		}
		read := Declared{Inputs: Inputs(fields[1]), FullKind: len(fields) == 3}
		if !slices.Contains([]Inputs{InputsPerUnit, InputsFileSyntax, InputsComposes}, read.Inputs) {
			return nil, fmt.Errorf("rule_inputs.txt:%d: %q is none of the three inputs", index+1, fields[1])
		}
		if _, isListed := declared[fields[0]]; isListed {
			return nil, fmt.Errorf("rule_inputs.txt:%d: %s has a line already", index+1, fields[0])
		}
		declared[fields[0]] = read
	}
	return declared, nil
}
