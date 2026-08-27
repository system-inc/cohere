package tailwind

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rule_testing"
)

// The violation half. Each case is a class written twice, on one of the three surfaces the
// configuration names.
func TestNoDuplicateClassesReportsRepeats(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
		wantIds  []string
	}{
		{
			name:     "jsx attribute",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex items-center flex" />;`,
			wantIds:  []string{"duplicateClass"},
		},
		{
			name:     "class attribute",
			fileName: "Component.tsx",
			source:   `const element = <div class="px-4 py-2 px-4" />;`,
			wantIds:  []string{"duplicateClass"},
		},
		{
			name:     "expression container",
			fileName: "Component.tsx",
			source:   `const element = <div className={"flex flex"} />;`,
			wantIds:  []string{"duplicateClass"},
		},
		{
			// The surface an attribute-only rule cannot see. On the real tree these hold 2,067
			// literals.
			name:     "callee argument",
			fileName: "Component.tsx",
			source:   `const merged = mergeClassNames('px-4 py-2 px-4');`,
			wantIds:  []string{"duplicateClass"},
		},
		{
			name:     "callee with several arguments",
			fileName: "Component.tsx",
			source:   `const merged = mergeClassNames('flex flex', 'gap-2 gap-2');`,
			wantIds:  []string{"duplicateClass", "duplicateClass"},
		},
		{
			// The third surface, 125 literals on the real tree.
			name:     "variable by name",
			fileName: "Styles.ts",
			source:   `const buttonClassName = 'rounded-md rounded-md';`,
			wantIds:  []string{"duplicateClass"},
		},
		{
			name:     "plural variable name",
			fileName: "Styles.ts",
			source:   `const buttonClassNames = 'p-2 p-2';`,
			wantIds:  []string{"duplicateClass"},
		},
		{
			// Two distinct classes each repeated: two findings, not four and not one.
			name:     "two different duplicates",
			fileName: "Component.tsx",
			source:   `const element = <div className="a b c a b" />;`,
			wantIds:  []string{"duplicateClass", "duplicateClass"},
		},
		{
			// A class repeated three times is still one finding for that class.
			name:     "same class three times",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex flex flex" />;`,
			wantIds:  []string{"duplicateClass"},
		},
		{
			// Irregular whitespace must not hide the repeat; `no-unnecessary-whitespace` owns the
			// spacing itself.
			name:     "extra whitespace between repeats",
			fileName: "Component.tsx",
			source:   "const element = <div className=\"text-sm   text-sm\" />;",
			wantIds:  []string{"duplicateClass"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, NoDuplicateClasses, testCase.fileName, testCase.source)
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// The clean half, which is the one the old corpus never had.
//
// A rule with only violation fixtures has been shown to detect and never to discriminate, and the
// defect it misses is the expensive one: a rule that fires on correct code gets disabled, and a
// disabled rule enforces nothing.
func TestNoDuplicateClassesStaysSilent(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
	}{
		{
			name:     "distinct classes",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex items-center gap-2" />;`,
		},
		{
			// The pair that collapses is a different rule's finding. This one must not fire on it.
			name:     "collapsible but not duplicate",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-4 py-4" />;`,
		},
		{
			name:     "single class",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex" />;`,
		},
		{
			name:     "empty string",
			fileName: "Component.tsx",
			source:   `const element = <div className="" />;`,
		},
		{
			name:     "only whitespace",
			fileName: "Component.tsx",
			source:   `const element = <div className="   " />;`,
		},
		{
			// A near-miss: same prefix, different class.
			name:     "similar but distinct",
			fileName: "Component.tsx",
			source:   `const element = <div className="border-l border-r" />;`,
		},
		{
			// An attribute nobody said carries classes.
			name:     "unrelated attribute",
			fileName: "Component.tsx",
			source:   `const element = <div title="flex flex" />;`,
		},
		{
			// A function nobody said combines classes.
			name:     "unrelated callee",
			fileName: "Component.tsx",
			source:   `const value = someOtherFunction('flex flex');`,
		},
		{
			// A variable whose name does not claim to hold classes.
			name:     "unrelated variable",
			fileName: "Styles.ts",
			source:   `const description = 'flex flex';`,
		},
		{
			// A method on an object is not the configured callee, even though the name matches.
			name:     "method with a matching name",
			fileName: "Component.tsx",
			source:   `const value = theme.mergeClassNames('flex flex');`,
		},
		{
			// A dynamic class is `no-concatenated-classes`'s finding, and the visible fragments here
			// hold no repeat.
			name:     "template hole with distinct statics",
			fileName: "Component.tsx",
			source:   "const element = <div className={`flex ${dynamic} gap-2`} />;",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, NoDuplicateClasses, testCase.fileName, testCase.source)
			rule_testing.ExpectClean(t, result)
		})
	}
}

// TestNoDuplicateClassesFixIsCorrect checks what the fix would actually write.
//
// A fix is a rewrite of the user's source that lands unattended, so asserting only that a finding
// was reported leaves the dangerous half untested. The two properties that matter: the surviving
// classes are the distinct ones, and they keep the order the author wrote them in, because
// reordering here would collide with `enforce-consistent-class-order`.
func TestNoDuplicateClassesFixIsCorrect(t *testing.T) {
	testCases := []struct {
		name     string
		source   string
		wantText string
	}{
		{
			name:     "removes the later repeat",
			source:   `const element = <div className="flex items-center flex" />;`,
			wantText: "flex items-center",
		},
		{
			name:     "keeps original order",
			source:   `const element = <div className="a b c a b" />;`,
			wantText: "a b c",
		},
		{
			name:     "collapses three into one",
			source:   `const element = <div className="flex flex flex" />;`,
			wantText: "flex",
		},
		{
			name:     "normalizes the gap it leaves behind",
			source:   "const element = <div className=\"text-sm   text-sm\" />;",
			wantText: "text-sm",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, NoDuplicateClasses, "Component.tsx", testCase.source)
			if len(result.Diagnostics) == 0 {
				t.Fatal("expected a finding, got none")
			}

			fixes := result.Diagnostics[0].Fixes
			if len(fixes) != 1 {
				t.Fatalf("expected exactly one fix, got %d", len(fixes))
			}
			if fixes[0].Text != testCase.wantText {
				t.Errorf("fix writes %q, want %q", fixes[0].Text, testCase.wantText)
			}

			// The fix must replace the class text and nothing else. If the range included the
			// quotes, applying it would produce an unterminated string.
			replaced := testCase.source[fixes[0].Range.Pos():fixes[0].Range.End()]
			if strings.Contains(replaced, `"`) || strings.Contains(replaced, "'") {
				t.Errorf("fix range covers a quote character: %q", replaced)
			}
		})
	}
}

// TestTemplateHoleIsReportedButNotFixed covers the case where the rule knows enough to complain and
// not enough to repair.
//
// The visible fragments genuinely repeat, so the finding is real. Rewriting from those fragments
// would silently delete the interpolation, which is a fix that changes what the code means.
func TestTemplateHoleIsReportedButNotFixed(t *testing.T) {
	// A template literal is not read as a class literal at all today, so the guard is asserted
	// directly against the reporting path rather than through the parser.
	literal := ClassLiteral{
		Text:   "flex ${dynamic} flex",
		Origin: ClassLiteralOriginAttribute,
	}

	var diagnostics []rule.Diagnostic
	ctx := rule.Context{
		Report: func(diagnostic rule.Diagnostic) { diagnostics = append(diagnostics, diagnostic) },
	}
	reportDuplicates(ctx, literal)

	if len(diagnostics) != 1 {
		t.Fatalf("expected one finding for the repeated visible class, got %d", len(diagnostics))
	}
	if len(diagnostics[0].Fixes) != 0 {
		t.Fatal("a literal with a template hole must not be rewritten, because the fix would drop the interpolation")
	}
}

// TestAttributeOnlyReadingLosesFindings is the known-dirty control.
//
// The tempting shape for this rule is to listen to JSX attributes and stop, which is what the
// obvious reading of "class names live in className" produces. It measures correct on most of the
// tree: attributes are 7,773 of the 9,965 class literals in the real corpus. It is also blind to
// 2,192 of them.
//
// So a deliberately narrowed reader runs against the same sources and is required to lose findings
// that the real rule catches. A guard that has never returned a positive has not been shown to work.
func TestAttributeOnlyReadingLosesFindings(t *testing.T) {
	sourcesOnlyNonAttributeSurfacesCatch := []string{
		`const merged = mergeClassNames('px-4 py-2 px-4');`,
		`const buttonClassName = 'rounded-md rounded-md';`,
	}

	attributeOnly := NewClassLiteralReader(ClassLiteralSettings{
		AttributeNames: DefaultClassLiteralSettings().AttributeNames,
		// Deliberately empty: this is the mistake being reproduced.
		CalleeNames:      nil,
		VariablePatterns: nil,
	})

	lost := 0
	for _, source := range sourcesOnlyNonAttributeSurfacesCatch {
		// The real rule must find it.
		result := rule_testing.Run(t, NoDuplicateClasses, "Component.tsx", source)
		if len(result.Diagnostics) == 0 {
			t.Fatalf("the real rule missed %q, which this control depends on it catching", source)
		}

		// The narrowed reader must not, which is what makes it dangerous.
		narrowed := rule_testing.Run(t, rule.Rule{
			Name: "attribute-only-control",
			Run: func(ctx rule.Context, _ any) rule.Listeners {
				listeners := rule.Listeners{}
				for _, kind := range ListenerKinds() {
					listeners[kind] = func(node *ast.Node) {
						for _, literal := range attributeOnly.ClassLiteralsIn(node) {
							reportDuplicates(ctx, literal)
						}
					}
				}
				return listeners
			},
		}, "Component.tsx", source)

		if len(narrowed.Diagnostics) == 0 {
			lost++
		}
	}

	if lost == 0 {
		t.Fatal("the attribute-only reader lost nothing, so this corpus does not exercise the callee " +
			"and variable surfaces and the passing tests above prove less than they appear to")
	}
	if lost != len(sourcesOnlyNonAttributeSurfacesCatch) {
		t.Errorf("expected the attribute-only reader to lose all %d, it lost %d",
			len(sourcesOnlyNonAttributeSurfacesCatch), lost)
	}
}
