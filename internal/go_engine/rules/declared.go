package rules

import (
	"fmt"
	"go/token"
	"maps"
	"slices"
	"strings"
)

// Declared is one rule's declaration: its inputs, and whether it reads more of a named type than its name, directly or
// through a helper. FullKind is a rule that renders a whole type (TypeString, a type in a message), walks a struct's
// fields or an interface's methods, or asks identity, assignability, implementation or sizes. A unit that holds a
// struct only by name or by pointer stays independent of a build that changes only an unexported field the unit never
// selects, and only a rule without FullKind is sure to judge that unit alike in both builds; a rule with it is judged
// with every named type it sees read in full.
//
// Scope is the names a rule asks its package's scope about before it writes code or judges a body by them (#b255cfy),
// as the table writes them, comma-separated (scopeEntries): a name, `copy` or `fmt`; a prefix followed by
// scopeDigits, which a name of the prefix and one or more decimal digits matches, as stringintconv's fresh import name
// is `fmt0`, `fmt1` and on where `fmt` is taken; scopeUses, the names the rule's body names itself, which the body
// reach's Shadowed reason reaches on the build's side and an object's bits on the host's; or scopePackage, a Composes
// rule's whole package, every name of it asked, which stage 5 checks whole in a build whose package holds a file only
// one side compiles. Empty for a rule that asks none. A universe name a rule asks is the body reach's whatever the
// table says, so declaring one is the rule's own record of it. A string, so a Declared still compares.
type Declared struct {
	Inputs   Inputs
	FullKind bool
	Scope    string
}

// scopeDigits ends a scope entry that is a prefix: the prefix followed by one or more decimal digits.
const scopeDigits = "[0-9]+"

// The scope entries that are no name: the names a rule's body names, and a Composes rule's whole package.
const (
	scopeUses    = "(uses)"
	scopePackage = "(package)"
)

// scopeEntries are declared's scope entries, as the table writes them, none for a rule that asks no name.
func (declared Declared) scopeEntries() []string {
	if declared.Scope == "" {
		return nil
	}
	return strings.Split(declared.Scope, ",")
}

// AsksScopeName reports whether declared asks its package's scope about name by a name or a prefix it declares
// (Declared.Scope), never by scopeUses or scopePackage, which name no name.
func (declared Declared) AsksScopeName(name string) bool {
	for _, entry := range declared.scopeEntries() {
		prefix, isPrefix := strings.CutSuffix(entry, scopeDigits)
		if !isPrefix {
			if entry == name {
				return true
			}
			continue
		}
		digits, hasPrefix := strings.CutPrefix(name, prefix)
		if hasPrefix && digits != "" && strings.Trim(digits, "0123456789") == "" {
			return true
		}
	}
	return false
}

// DeclaresScope reports whether declared says how it asks its package's scope: by a name, a prefix, its body's own
// names or its whole package (Declared.Scope).
func (declared Declared) DeclaresScope() bool {
	return declared.Scope != ""
}

// ScopeNameAsker is the first rule, by name, whose declaration asks its package's scope about name by a name or a
// prefix it declares (Declared.AsksScopeName), and whether one does. The body reach reads it of a name one side alone
// declares at package level (#b255cfy).
func ScopeNameAsker(name string) (string, bool) {
	declared, parseError := DeclaredInputs()
	if parseError != nil {
		// Invariant: TestTheInputsTableParses keeps a malformed table from landing.
		panic(fmt.Sprintf("rules: the inputs table does not parse: %v", parseError))
	}
	for _, ruleName := range slices.Sorted(maps.Keys(declared)) {
		if declared[ruleName].AsksScopeName(name) {
			return ruleName, true
		}
	}
	return "", false
}

// parseScope is a table line's scope field, `Scope=` and its entries, validated for a rule of inputs: each a name, a
// prefix followed by scopeDigits, scopeUses or scopePackage, the last only for a Composes rule, none twice.
func parseScope(field string, inputs Inputs) (string, error) {
	entries, isScope := strings.CutPrefix(field, "Scope=")
	if !isScope || entries == "" {
		return "", fmt.Errorf("%q is not FullKind nor Scope= and its entries", field)
	}
	seen := map[string]bool{}
	for entry := range strings.SplitSeq(entries, ",") {
		if seen[entry] {
			return "", fmt.Errorf("the scope names %s twice", entry)
		}
		seen[entry] = true
		prefix, _ := strings.CutSuffix(entry, scopeDigits)
		switch {
		case entry == scopePackage && inputs != InputsComposes:
			return "", fmt.Errorf("%s is a Composes rule's scope, and the rule is %s", scopePackage, inputs)
		case entry == scopeUses, entry == scopePackage:
		case !token.IsIdentifier(prefix):
			return "", fmt.Errorf("the scope entry %q is no name, no prefix followed by %s, %s nor %s", entry,
				scopeDigits, scopeUses, scopePackage)
		}
	}
	return entries, nil
}
