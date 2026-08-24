package tailwind

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// Expectations measured against the real plugin, and against the real corpus for the silent half.
// The silent cases matter more than usual here: this rule's failure mode is reporting a class that
// works, and a rule that flags working classes gets turned off the first time it does it.

func TestNoUnknownClassesReportsTypos(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
		wantIds  []string
	}{
		{
			name:     "misspelled utility",
			fileName: "Component.tsx",
			source:   `const element = <div className="flx items-center" />;`,
			wantIds:  []string{"unknownClass"},
		},
		{
			name:     "invented utility",
			fileName: "Component.tsx",
			source:   `const element = <div className="not-a-real-class-xyz" />;`,
			wantIds:  []string{"unknownClass"},
		},
		{
			// A project's own CSS class. Real, but not Tailwind's, so it needs the ignore list rather
			// than a fix. Reported so the choice is deliberate.
			name:     "stylesheet class Tailwind does not know",
			fileName: "Component.tsx",
			source:   `const element = <div className="ahralia-splash" />;`,
			wantIds:  []string{"unknownClass"},
		},
		{
			// Misspellings that start with a real root and continue without a separator. These are
			// the cases the value-boundary check exists for: `px4` begins with root `px`, `wfull`
			// with `w`, `textcenter` with `text`. Dropping the boundary check makes every one of
			// them look valid, which is a rule that catches almost no typos while appearing to work.
			name:     "typo starting with a real root",
			fileName: "Component.tsx",
			source:   `const element = <div className="px4" />;`,
			wantIds:  []string{"unknownClass"},
		},
		{
			name:     "another root-prefixed typo",
			fileName: "Component.tsx",
			source:   `const element = <div className="wfull textcenter" />;`,
			wantIds:  []string{"unknownClass", "unknownClass"},
		},
		{
			name:     "two unknowns in one literal",
			fileName: "Component.tsx",
			source:   `const element = <div className="flx grd" />;`,
			wantIds:  []string{"unknownClass", "unknownClass"},
		},
		{
			name:     "on a callee surface",
			fileName: "Component.tsx",
			source:   `const merged = mergeClassNames('flx');`,
			wantIds:  []string{"unknownClass"},
		},
		{
			name:     "on a variable surface",
			fileName: "Styles.ts",
			source:   `const buttonClassName = 'not-a-real-class-xyz';`,
			wantIds:  []string{"unknownClass"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, NoUnknownClasses, testCase.fileName, testCase.source)
			ruletest.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

// The silent half, and every case here is one an earlier version of this rule reported.
//
// Answering existence from the property tables produced 12 findings on valid classes across the real
// corpus. These are those shapes, pinned so the rule cannot regress into asking the wrong question.
func TestNoUnknownClassesStaysSilent(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
		source   string
	}{
		{
			name:     "ordinary utilities",
			fileName: "Component.tsx",
			source:   `const element = <div className="flex items-center gap-2" />;`,
		},
		{
			// Declares only `--tw-gradient-from`, so it is absent from the property tables and real.
			name:     "gradient stops",
			fileName: "Component.tsx",
			source:   `const element = <div className="from-black/70 to-transparent" />;`,
		},
		{
			// Emits several rules, so `declaredProperties` skips it deliberately.
			name:     "multi-rule component class",
			fileName: "Component.tsx",
			source:   `const element = <div className="container" />;`,
		},
		{
			// Markers that generate no CSS of their own; they exist to be referenced by
			// `group-hover:` and `peer-checked:` on other elements.
			name:     "group and peer",
			fileName: "Component.tsx",
			source:   `const element = <div className="group peer" />;`,
		},
		{
			name:     "group with a name",
			fileName: "Component.tsx",
			source:   `const element = <div className="group/item" />;`,
		},
		{
			// The author wrote the declaration directly, so there is no root to look up and nothing
			// to check it against.
			name:     "arbitrary property",
			fileName: "Component.tsx",
			source:   `const element = <div className="[font:inherit]" />;`,
		},
		{
			name:     "arbitrary value",
			fileName: "Component.tsx",
			source:   `const element = <div className="w-[13px] h-[calc(100%-3px)]" />;`,
		},
		{
			// A custom property as the value.
			name:     "custom property value",
			fileName: "Component.tsx",
			source:   `const element = <div className="from-(--color-background--1)" />;`,
		},
		{
			name:     "variants",
			fileName: "Component.tsx",
			source:   `const element = <div className="hover:px-4 sm:hover:flex" />;`,
		},
		{
			// A data-attribute variant on a class that declares only custom properties.
			name:     "data variant on an animation class",
			fileName: "Component.tsx",
			source:   `const element = <div className="data-[show=true]:fade-in" />;`,
		},
		{
			name:     "importance",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-4!" />;`,
		},
		{
			name:     "unrelated attribute",
			fileName: "Component.tsx",
			source:   `const element = <div title="flx" />;`,
		},
		{
			name:     "unrelated callee",
			fileName: "Component.tsx",
			source:   `const value = someOtherFunction('flx');`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, NoUnknownClasses, testCase.fileName, testCase.source)
			ruletest.ExpectClean(t, result)
		})
	}
}

// TestUnknownIgnoreExemptsProjectClasses covers the option this rule cannot ship without.
//
// Any project with hand-written CSS has class names Tailwind does not define. On the ahra tree those
// are the five `ahralia-splash*` classes, and they are the entire real-world finding count.
func TestUnknownIgnoreExemptsProjectClasses(t *testing.T) {
	options := NoUnknownClassesOptions{Ignore: []string{`^ahralia-`}}

	exempted := ruletest.RunWithOptions(t, NoUnknownClasses, "Component.tsx",
		`const element = <div className="ahralia-splash flex" />;`, options)
	if len(exempted.Diagnostics) != 0 {
		t.Errorf("an ignored class should not be reported, got %d findings", len(exempted.Diagnostics))
	}

	// Without the exemption it reports, so the test above is not passing because the rule is inert.
	unexempted := ruletest.Run(t, NoUnknownClasses, "Component.tsx",
		`const element = <div className="ahralia-splash flex" />;`)
	if len(unexempted.Diagnostics) == 0 {
		t.Fatal("the rule stayed silent without an ignore option, so the exemption proves nothing")
	}

	// And the exemption must not swallow real typos alongside it.
	stillReported := ruletest.RunWithOptions(t, NoUnknownClasses, "Component.tsx",
		`const element = <div className="ahralia-splash flx" />;`, options)
	if len(stillReported.Diagnostics) != 1 {
		t.Errorf("the ignore pattern should exempt only what it matches, got %d findings",
			len(stillReported.Diagnostics))
	}
}

// TestExistenceIsNotReadFromPropertyTables is the known-dirty control.
//
// The tempting implementation asks the property tables whether a class is known, because they are
// already there and every other rule uses them. It passes every violation fixture above and reports
// 12 valid classes on the real corpus, because those tables deliberately exclude utilities that
// declare nothing a rule can compare.
//
// So the shortcut is implemented here and required to be wrong about classes the real rule gets
// right.
func TestExistenceIsNotReadFromPropertyTables(t *testing.T) {
	// Classes that are real and absent from the property tables.
	realButUndeclared := []string{
		"container",
		"from-black/70",
		"to-transparent",
		"fade-in",
	}

	lostByShortcut := 0
	for _, className := range realButUndeclared {
		if !classExists(className) {
			t.Errorf("the real rule reports %q as unknown, and it is a valid class", className)
			continue
		}

		// The shortcut: resolve through the property tables, as the other rules do.
		if _, canResolve := resolveClassFacts(className); !canResolve {
			lostByShortcut++
		}
	}

	if lostByShortcut == 0 {
		t.Fatal("the property-table shortcut resolved every one of these, so this control no longer " +
			"demonstrates the defect it exists to demonstrate")
	}
	t.Logf("the property-table shortcut would report %d of %d valid classes as unknown",
		lostByShortcut, len(realButUndeclared))
}

// TestKnownRootWithUnknownValueIsNotReported pins a deliberate gap.
//
// `text-huge` has root `text` and does not compile, so it is genuinely unknown and this rule stays
// quiet. Catching it needs the theme's per-root value scales plus the arbitrary-value grammar, which
// is closer to reimplementing the utility resolver than to reading a table.
//
// Pinned as a test rather than left as a comment so that a future change closing the gap has to
// delete an assertion deliberately, rather than discovering the behavior by surprise.
func TestKnownRootWithUnknownValueIsNotReported(t *testing.T) {
	if !classExists("text-huge") {
		t.Fatal("this rule now validates values against the theme's scales, which is a real improvement " +
			"and means this assertion should be replaced rather than kept")
	}

	// The complement: an unknown root is still caught, so the gap is about values rather than about
	// the rule being unable to report at all.
	if classExists("txt-huge") {
		t.Error("an unknown root must still be reported, or the rule catches nothing")
	}
}
