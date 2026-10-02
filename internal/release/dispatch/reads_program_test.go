package dispatch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A rule that reads ctx.Program must declare ReadsProgram, and this is what makes that structural
// rather than remembered.
//
// ctx.Program reaches every source file in the run, so a rule that touches it is not pure per-file.
// The findings cache keys on the linted file's hash, so an undeclared cross-file rule serves a
// stale result when the other file changes: zero findings, forever, indistinguishable from a clean
// tree. Nothing downstream notices, which is why the check lives here instead of in a convention.
//
// The check reads each rule file's syntax, not its text and not its behavior. Behavior would need a
// program to run against and would cover only the rules it happened to exercise. Text was what this
// checked until #ym4v8bc found two holes in it, both in the Tailwind rules:
//
//   - A helper read the program for its callers. DesignSystemForProgram took a rule.Context, so five
//     rules reached the program without writing ctx.Program, and they passed only because each also
//     happened to check ctx.Program == nil somewhere. A file that reads the program through a
//     rule.Context and defines no rule now fails here: its callers would read the program without
//     naming it, and nothing would make them declare. Such a helper takes *compiler.Program instead,
//     and every call site then names ctx.Program, which this sees.
//   - A comment satisfied it. The helper's own file passed because its doc comment contained the
//     text `ReadsProgram: true`. A declaration is now a ReadsProgram key set to true in a composite
//     literal, and a mention of ctx.Program in a comment is not a read.
func TestRulesReadingProgramDeclareIt(t *testing.T) {
	ruleFiles := ruleSourceFiles(t)
	if len(ruleFiles) == 0 {
		// A check that found nothing to check passes for the wrong reason, which is the same shape
		// as the defect it guards against.
		t.Fatal("found no rule source files, so this test proved nothing")
	}

	sawProgramReader := false
	for _, path := range ruleFiles {
		facts := readProgramFacts(t, path)
		if !facts.readsProgram {
			continue
		}
		sawProgramReader = true

		if !facts.definesRule {
			t.Errorf("%s reads the program through a rule.Context and defines no rule, so the rules "+
				"calling it read the program without naming it and nothing makes them declare "+
				"ReadsProgram. Take a *compiler.Program instead, so each call site names ctx.Program",
				path)
			continue
		}
		if !facts.declares {
			t.Errorf("%s reads the program and does not declare ReadsProgram: true, so a findings cache "+
				"keyed on one file's hash would serve a stale result when the other file changes",
				path)
		}
	}

	if !sawProgramReader {
		// Dozens of rules read the program today. Zero would mean the search is looking in the wrong
		// place rather than that the tree got cleaner.
		t.Fatal("no rule source reads ctx.Program, which is implausible: this check is " +
			"looking at the wrong files")
	}
}

// The check sees code and not text, in both directions it was wrong in before.
//
// Each source is one this guard used to misjudge, written to a file and read the way the guard reads
// the tree, so the guard is shown able to fail rather than only shown passing.
func TestReadProgramFactsSeesCodeNotComments(t *testing.T) {
	testCases := []struct {
		name   string
		source string
		want   programFacts
	}{
		{
			// The shape DesignSystemForProgram had: a helper reading the program for its callers.
			name: "a helper reading the program through a context",
			source: `package probe
func helper(ctx rule.Context) bool { return ctx.Program != nil }`,
			want: programFacts{readsProgram: true},
		},
		{
			// The shape the helper's own file had: the declaration's text in a comment.
			name: "a declaration in a comment",
			source: `package probe
// Every caller must declare ` + "`ReadsProgram: true`" + `.
var Probe = rule.Rule{Run: func(ctx rule.Context, options any) rule.Listeners { _ = ctx.Program; return nil }}`,
			want: programFacts{readsProgram: true, definesRule: true},
		},
		{
			name: "a read in a comment only",
			source: `package probe
// This rule never touches ctx.Program.
var Probe = rule.Rule{Run: func(ctx rule.Context, options any) rule.Listeners { return nil }}`,
			want: programFacts{definesRule: true},
		},
		{
			// A context under another name is still the context.
			name: "a context parameter with a different name",
			source: `package probe
var Probe = rule.Rule{ReadsProgram: true, Run: func(c rule.Context, options any) rule.Listeners { _ = c.Program; return nil }}`,
			want: programFacts{readsProgram: true, declares: true, definesRule: true},
		},
		{
			name: "a declaration set to false",
			source: `package probe
var Probe = rule.Rule{ReadsProgram: false, Run: func(ctx rule.Context, options any) rule.Listeners { _ = ctx.Program; return nil }}`,
			want: programFacts{readsProgram: true, definesRule: true},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "probe.go")
			if err := os.WriteFile(path, []byte(testCase.source), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := readProgramFacts(t, path); got != testCase.want {
				t.Errorf("got %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// programFacts is what one rule file does with the program, read from its syntax.
type programFacts struct {
	// readsProgram is a `.Program` selector on a rule.Context parameter, under any name.
	readsProgram bool
	// declares is a `ReadsProgram: true` key in a composite literal.
	declares bool
	// definesRule is a rule.Rule composite literal.
	definesRule bool
}

// readProgramFacts parses one file and reports what it does with the program.
//
// Names of rule.Context parameters are collected first, since a rule may call its context anything;
// ctx and context are added for the rare function literal whose parameter types are elided.
func readProgramFacts(t *testing.T, path string) programFacts {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	contextNames := map[string]bool{"ctx": true, "context": true}
	ast.Inspect(file, func(node ast.Node) bool {
		if function, isFunction := node.(*ast.FuncType); isFunction && function.Params != nil {
			for _, field := range function.Params.List {
				if isRuleSelector(field.Type, "Context") {
					for _, name := range field.Names {
						contextNames[name.Name] = true
					}
				}
			}
		}
		return true
	})

	facts := programFacts{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			if receiver, isIdentifier := typed.X.(*ast.Ident); isIdentifier &&
				typed.Sel.Name == "Program" && contextNames[receiver.Name] {
				facts.readsProgram = true
			}
		case *ast.KeyValueExpr:
			key, keyIsIdentifier := typed.Key.(*ast.Ident)
			value, valueIsIdentifier := typed.Value.(*ast.Ident)
			if keyIsIdentifier && key.Name == "ReadsProgram" && valueIsIdentifier && value.Name == "true" {
				facts.declares = true
			}
		case *ast.CompositeLit:
			if isRuleSelector(typed.Type, "Rule") {
				facts.definesRule = true
			}
		}
		return true
	})
	return facts
}

// isRuleSelector reports whether an expression is `rule.<name>`.
func isRuleSelector(expression ast.Expr, name string) bool {
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	packageName, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && packageName.Name == "rule" && selector.Sel.Name == name
}

// ruleSourceFiles lists the non-test Go files under internal/rules.
func ruleSourceFiles(t *testing.T) []string {
	t.Helper()

	var found []string
	err := filepath.Walk("../../lint/rules", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking rule packages: %v", err)
	}
	return found
}
