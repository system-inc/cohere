package registry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Every rule must ship a fixture pair: a case proving it fires, and a case proving it stays quiet.
//
// This guard exists because of what the corpus is now for. While eslint still runs, a rule's real
// specification is the tool it was ported from, and its Go test is a convenience. When eslint is
// removed, nothing else checks these rules, and the pair becomes the only statement of what the rule
// means. A rule whose test asserts only the firing direction cannot tell a correct rule from one
// that reports on everything.
//
// Measured on the ahra tree at 03:41: the 139 unported rules produce zero findings, because the gate
// has been rejecting those violations for months. So a clean differential cannot be the signal to
// remove the old tool. It reads clean today and would keep reading clean until somebody wrote a
// violation of an unported rule, at which point it reads clean while the tree gets worse. The
// completion signal is 171 rules with pairs, which makes this guard the thing standing between a
// finished-looking catalog and a real one.
//
// Parsing rather than grepping. A grep for `rule.Rule{` over the same files reports
// internal/rules/upstream/adapt.go as an unpaired rule: it builds a rule.Rule inside a function, so
// the text matches while the declaration does not exist. That false positive is cheap here and
// expensive at 171 rules, and it is the same class as two probes that searched for invented harness
// names and returned confident wrong answers.
func TestEveryRuleShipsAFixturePair(t *testing.T) {
	rules := ruleDeclarationsByFile(t)
	if len(rules) == 0 {
		// A sweep that finds nothing to check passes for the wrong reason, which is the same shape
		// as the defect it guards against.
		t.Fatal("found no rule declarations, so this test proved nothing")
	}

	for path, names := range rules {
		testPath := strings.TrimSuffix(path, ".go") + "_test.go"
		assertions := harnessAssertionsIn(t, testPath)

		for _, name := range names {
			switch {
			case !assertions.exists:
				t.Errorf("rule %s has no test file at %s, so nothing proves it fires or stays quiet",
					name, filepath.Base(testPath))
			case !assertions.expectsFindings:
				t.Errorf("rule %s never calls ruletest.ExpectFindings, so nothing proves it can fire; "+
					"a rule that cannot be shown to detect is indistinguishable from one that is inert",
					name)
			case !assertions.expectsClean:
				t.Errorf("rule %s never calls ruletest.ExpectClean, so nothing proves it stays quiet; "+
					"a violation-only corpus proves a rule can detect and never that it can discriminate",
					name)
			}
		}
	}
}

// harnessAssertions records which directions a rule's test file actually asserts.
type harnessAssertions struct {
	exists          bool
	expectsFindings bool
	expectsClean    bool
}

// harnessAssertionsIn reads a test file for calls to the two ruletest assertions.
//
// The names are read from the harness rather than remembered. Two sweeps tonight searched for
// invented names, `Valid:`/`Invalid:` and `ExpectNoFindings`, and each returned a confident wrong
// answer: one reported that no rule tests existed, the other that no rule asserted the silent
// direction. Both were the probe. If these names ever change, this guard fails loudly on every rule
// at once, which is the signature to distrust the probe rather than the population.
func harnessAssertionsIn(t *testing.T, path string) harnessAssertions {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return harnessAssertions{}
	}

	assertions := harnessAssertions{exists: true}
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		packageIdentifier, isIdentifier := selector.X.(*ast.Ident)
		if !isIdentifier || packageIdentifier.Name != "ruletest" {
			return true
		}
		switch selector.Sel.Name {
		case "ExpectFindings":
			assertions.expectsFindings = true
		case "ExpectClean":
			assertions.expectsClean = true
		}
		return true
	})
	return assertions
}

// ruleDeclarationsByFile returns the exported rule.Rule declarations in each rules file.
//
// Only package-level `var X = rule.Rule{...}` counts, which is what separates a rule from the
// adapter that builds one at runtime.
func ruleDeclarationsByFile(t *testing.T) map[string][]string {
	t.Helper()

	matches, err := filepath.Glob("../rules/*/*.go")
	if err != nil {
		t.Fatalf("globbing rule files: %v", err)
	}

	byFile := make(map[string][]string)
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
				if !isRuleLiteral(value.Values[0]) || !ast.IsExported(value.Names[0].Name) {
					continue
				}
				byFile[path] = append(byFile[path], parsed.Name.Name+"."+value.Names[0].Name)
			}
		}
	}
	return byFile
}
