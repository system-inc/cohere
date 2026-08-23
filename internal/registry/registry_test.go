package registry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Every rule declared in the rules packages must appear in the registry.
//
// A rule that exists on disk and is absent from the registry compiles, tests green in its own
// package, and never runs. That is the worst defect this tool can carry: the file says the rule is
// enforced, the gate says the tree is clean, and neither is lying about what it knows.
//
// This was not hypothetical. Two boundary rules were written, tested, and left out of the registry;
// nothing failed until this test existed.
func TestEveryRuleIsRegistered(t *testing.T) {
	declared := declaredRules(t)
	registered := registeredRules(t)

	if len(declared) == 0 {
		// A test that finds nothing to check passes for the wrong reason, which is the same shape
		// as the defect it guards against.
		t.Fatal("found no rule declarations to check, so this test proved nothing")
	}

	for _, name := range declared {
		if !registered[name] {
			t.Errorf("rule %s is declared but not in the registry, so it would never run", name)
		}
	}
}

// declaredRules parses the rules packages for `var Name = rule.Rule{...}` declarations.
//
// Parsing rather than reflecting: a rule absent from the registry is also absent from anything the
// registry could reflect over, so reflection would see exactly the rules that are already fine.
func declaredRules(t *testing.T) []string {
	t.Helper()

	var names []string
	matches, err := filepath.Glob("../rules/*/*.go")
	if err != nil {
		t.Fatalf("globbing rule files: %v", err)
	}

	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		fileSet := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", path, parseErr)
		}

		for _, declaration := range parsed.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				value, isValue := spec.(*ast.ValueSpec)
				if !isValue || len(value.Names) != 1 || len(value.Values) != 1 {
					continue
				}
				if !isRuleLiteral(value.Values[0]) {
					continue
				}
				name := value.Names[0].Name
				if !ast.IsExported(name) {
					continue
				}
				names = append(names, parsed.Name.Name+"."+name)
			}
		}
	}
	return names
}

// isRuleLiteral reports whether an expression is a `rule.Rule{...}` composite literal.
func isRuleLiteral(expression ast.Expr) bool {
	composite, isComposite := expression.(*ast.CompositeLit)
	if !isComposite {
		return false
	}
	selector, isSelector := composite.Type.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	packageIdentifier, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && packageIdentifier.Name == "rule" && selector.Sel.Name == "Rule"
}

// registeredRules reads the registry source for the rules All() returns.
func registeredRules(t *testing.T) map[string]bool {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "registry.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing registry.go: %v", err)
	}

	registered := make(map[string]bool)
	ast.Inspect(parsed, func(node ast.Node) bool {
		selector, isSelector := node.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		packageIdentifier, isIdentifier := selector.X.(*ast.Ident)
		if !isIdentifier {
			return true
		}
		registered[packageIdentifier.Name+"."+selector.Sel.Name] = true
		return true
	})
	return registered
}

// The registry must not contain duplicates: a rule listed twice reports every finding twice.
func TestNoDuplicateRules(t *testing.T) {
	seen := make(map[string]bool)
	for _, subject := range All() {
		if seen[subject.Name] {
			t.Errorf("rule %q appears in the registry more than once", subject.Name)
		}
		seen[subject.Name] = true
	}
}

// Count must reflect what All actually returns, since the coverage line reports it and a coverage
// line that overstates what ran is the failure this tool exists to prevent.
func TestCountMatchesAll(t *testing.T) {
	if Count() != len(All()) {
		t.Errorf("Count() = %d but All() returned %d rules", Count(), len(All()))
	}
}
