package tailwind

import (
	"os"
	"path/filepath"
	"testing"

	tailwindcollapse "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
	"github.com/system-inc/cohere/internal/lint/rules/tailwind/vendored"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// Expectations measured against the real plugin, and against the real corpus for the silent half.
// The silent cases matter more than usual here: this rule's failure mode is reporting a class that
// works, and a rule that flags working classes gets turned off the first time it does it.
//
// # Why every fixture here runs through a program
//
// Same reason `enforce_consistent_class_order_test.go` gives, and the same trap. The rule now reads
// the live design system, so it declares `ProgramReads` and returns nil listeners when the Program
// is nil. `rule_testing.Run` hands it exactly that. So every fixture in this file that kept using
// `rule_testing.Run` after the swap would exercise the nil-Program branch: the reporting half would fail
// loudly and the whole silent half would pass while proving nothing.
//
// Measured before these were converted: with the rule swapped and the fixtures still on
// `rule_testing.Run`, the eight reporting cases failed and all thirteen silent cases passed. Thirteen
// green assertions over a rule that never ran.
//
// `TestUnknownClassFixturesActuallyRan` is what stops the converted file from going green on nothing
// in the other direction, where the walk for an installed tailwindcss finds none and every fixture
// skips while `go test` prints ok.

// unknownFixtureStylesheetPath is where the fixture writes its root stylesheet.
//
// The first entry in `tailwindEntryPointCandidates`, so the rule's own entry-point search finds it
// by the same path it uses on a real repository.
const unknownFixtureStylesheetPath = "app/_theme/styles/theme.css"

// unknownFixtureStylesheet is the fixture's root stylesheet, and it declares utilities of its own.
//
// This is the one place this file's fixtures differ from the class-order ones, and the difference is
// the whole point of the migration. Those declare no tokens deliberately, because per-repository
// behaviour is not what they measure. Here it is exactly what is measured: a class this repository
// declares and no framework contains must read as known, and the shipped table could only answer
// that for the repository it was generated from.
//
// The names come from `internal/lint/rules/tailwind/tools/generate_descriptor_base/testdata/independent_theme.css`,
// which exists so a framework fact can be told from a repository fact. `synthetic-static` and
// `synthetic-fn-*` appear in no framework table and in neither corpus repository, so a rule that
// calls them known has consulted this stylesheet rather than a table.
const unknownFixtureStylesheet = `@import "tailwindcss";

@theme {
    --frobnicate-small: 2px;
    --frobnicate-large: 9px;
}

@utility synthetic-static {
    display: grid;
    opacity: 0.5;
}

@utility synthetic-fn-* {
    margin: --value(--frobnicate-*, [length]);
}`

// unknownFixturePackageRoot is the installed tailwindcss the fixture stylesheet imports: the vendored
// snapshot, so the fixtures run on any machine (#sycrdr6). It used to be found by walking up from ahra's
// checkout, which only one laptop has.
func unknownFixturePackageRoot(test *testing.T) string {
	test.Helper()
	return vendored.TailwindPackageRootForTest(test)
}

// runUnknownFixture runs the rule against a one-file program that has a real design system.
//
// The symlink is planted before the program is built, into the same temp directory
// `rule_testing.RunTypedFiles` writes the fixture files to, so the rule's own upward walk finds it.
// Pointing the `@import` at an absolute path instead does not work: the rule never resolves the
// import itself, it walks up for `node_modules/tailwindcss` and declines when the walk fails.
func runUnknownFixture(test *testing.T, fileName string, source string) rule_testing.Result {
	test.Helper()
	return runUnknownFixtureWithOptions(test, fileName, source, nil)
}

// runUnknownFixtureWithOptions is runUnknownFixture for the cases that configure the ignore list.
func runUnknownFixtureWithOptions(
	test *testing.T,
	fileName string,
	source string,
	options *NoUnknownClassesOptions,
) rule_testing.Result {
	test.Helper()

	packageRoot := unknownFixturePackageRoot(test)
	if packageRoot == "" {
		test.Skip("no installed tailwindcss on this machine, so the rule's design system cannot be " +
			"built and these fixtures would measure a decline rather than an existence check")
	}

	files := map[string]string{
		fileName:                     source,
		unknownFixtureStylesheetPath: unknownFixtureStylesheet,
	}
	plantPackage := func(root string) {
		modules := filepath.Join(root, "node_modules")
		if createDirectoryError := os.MkdirAll(modules, 0o755); createDirectoryError != nil {
			test.Fatalf("creating the fixture node_modules: %v", createDirectoryError)
		}
		if symlinkError := os.Symlink(packageRoot, filepath.Join(modules, "tailwindcss")); symlinkError != nil {
			test.Fatalf("linking the installed tailwindcss into the fixture: %v", symlinkError)
		}
	}

	if options == nil {
		return rule_testing.RunTypedFilesWithSetup(test, NoUnknownClasses, files, fileName, plantPackage)
	}
	return rule_testing.RunTypedFilesWithSetupAndOptions(
		test, NoUnknownClasses, files, fileName, *options, plantPackage)
}

func TestNoUnknownClassesReportsTypos(test *testing.T) {
	test.Parallel()
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
			source:   `const element = <div className="orbit-banner" />;`,
			wantIds:  []string{"unknownClass"},
		},
		{
			// Misspellings that start with a real root and continue without a separator. These are
			// the cases the value-boundary check exists for: `px4` begins with root `px`, `wfull`
			// with `w`, `textcenter` with `text`. Dropping the boundary check makes every one of
			// them look valid, which is a rule that catches almost no typos while appearing to work.
			//
			// The boundary is now `parseCandidate`'s rather than this rule's own prefix walk, which
			// is the correction: the walk approximated where a root ends, and the same approximation
			// in a different guise is what lost `border-x` during the migration.
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
			// A variant this design system never declared makes the whole class unreadable, which is
			// correct: `notavariant:flex` compiles to nothing. Only reachable now that the variants
			// are parsed by the same design system that answers for the utility; the shipped rule
			// stripped everything before the last colon and never looked at it.
			name:     "unknown variant on a real utility",
			fileName: "Component.tsx",
			source:   `const element = <div className="notavariant:flex" />;`,
			wantIds:  []string{"unknownClass"},
		},
		{
			name:     "on a callee surface",
			fileName: "Component.tsx",
			source:   `const merged = cn('flx');`,
			wantIds:  []string{"unknownClass"},
		},
		{
			name:     "on a variable surface",
			fileName: "Styles.ts",
			source:   `const className = 'not-a-real-class-xyz';`,
			wantIds:  []string{"unknownClass"},
		},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runUnknownFixture(test, testCase.fileName, testCase.source)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// The silent half, and every case here is one an earlier version of this rule reported.
//
// Answering existence from the property tables produced 12 findings on valid classes across the real
// corpus. These are those shapes, pinned so the rule cannot regress into asking the wrong question.
func TestNoUnknownClassesStaysSilent(test *testing.T) {
	test.Parallel()
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
			// `group-hover:` and `peer-checked:` on other elements. `ParseCandidate` returns no
			// readings for them for exactly that reason, so they must be recognised before it is
			// asked or the two most common classes in any codebase get reported.
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
			name:     "variants",
			fileName: "Component.tsx",
			source:   `const element = <div className="hover:px-4 sm:hover:flex" />;`,
		},
		{
			name:     "importance",
			fileName: "Component.tsx",
			source:   `const element = <div className="px-4!" />;`,
		},
		{
			// This fixture's own `@utility` blocks, which no generated table contains. The static
			// one and the functional one exercise the two halves of a repository's contribution.
			name:     "this repository's own static utility",
			fileName: "Component.tsx",
			source:   `const element = <div className="synthetic-static" />;`,
		},
		{
			name:     "this repository's own functional utility",
			fileName: "Component.tsx",
			source:   `const element = <div className="synthetic-fn-small synthetic-fn-large" />;`,
		},
		{
			name:     "this repository's own utility under a variant",
			fileName: "Component.tsx",
			source:   `const element = <div className="hover:synthetic-static" />;`,
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
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runUnknownFixture(test, testCase.fileName, testCase.source)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestNoUnknownClassesAtHolesAndConcatenation is the one place the glued edges change a verdict.
//
// Measured on eslint-plugin-better-tailwindcss 4.9.1 under ESLint on 2026-11-07, with ahra's theme.
// Each piece of a template is judged as a literal, and a class a hole or a `+` touches with no
// whitespace between is skipped, since the value beside it completes it:
//
//	`block notaclass ${x}`              reports notaclass
//	`notaclass ${x} block`              reports notaclass
//	`block ${x} notaclass`              reports notaclass
//	`a ${x}b c${y}d`                    reports a only
//	`notaclass${x} block`               silent
//	`block ${x}notaclass`               silent
//	`block ${x}notaclass${y} flex`      silent
//	'notaclass ' + x, x + ' notaclass'  reports notaclass
//	'notaclass\u00a0' + x, x + '\u3000notaclass'   report notaclass, JavaScript's whitespace
//	'notaclass' + x, x + 'notaclass'    silent
//	'block notaclass' + x               silent
func TestNoUnknownClassesAtHolesAndConcatenation(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name    string
		source  string
		classes []string
	}{
		{
			name:    "a run beside a hole",
			source:  "const element = <div className={`block notaclass ${size}`} />;",
			classes: []string{"notaclass"},
		},
		{
			name:    "after a hole, with whitespace",
			source:  "const element = <div className={`block ${size} notaclass`} />;",
			classes: []string{"notaclass"},
		},
		{
			name:    "only the unglued class of several runs",
			source:  "const element = <div className={`a ${size}b c${tone}d`} />;",
			classes: []string{"a"},
		},
		{
			name:    "glued before a hole",
			source:  "const element = <div className={`notaclass${size} block`} />;",
			classes: []string{},
		},
		{
			name:    "glued after a hole",
			source:  "const element = <div className={`block ${size}notaclass`} />;",
			classes: []string{},
		},
		{
			name:    "glued on both sides",
			source:  "const element = <div className={`block ${size}notaclass${tone} flex`} />;",
			classes: []string{},
		},
		{
			// Where a string literal is joined with `+`, the finding sits on the literal, as every
			// literal finding here does today.
			name:    "a concatenated literal that ends in whitespace",
			source:  "const element = (size: string) => <div className={'notaclass ' + size} />;",
			classes: []string{"notaclass "},
		},
		{
			name:    "a concatenated literal that starts with whitespace",
			source:  "const element = (size: string) => <div className={size + ' notaclass'} />;",
			classes: []string{" notaclass"},
		},
		{
			// JavaScript's whitespace, not ASCII's: a no-break space before the join sets the class
			// apart, and an ideographic space after it does too.
			name:    "a concatenated literal that ends in a no-break space",
			source:  "const element = (size: string) => <div className={'notaclass\u00a0' + size} />;",
			classes: []string{"notaclass\u00a0"},
		},
		{
			name:    "a concatenated literal that starts with an ideographic space",
			source:  "const element = (size: string) => <div className={size + '\u3000notaclass'} />;",
			classes: []string{"\u3000notaclass"},
		},
		{
			name:    "a literal concatenated on its right",
			source:  "const element = (size: string) => <div className={'block notaclass' + size} />;",
			classes: []string{},
		},
		{
			name:    "a literal concatenated on its left",
			source:  "const element = (size: string) => <div className={size + 'notaclass block'} />;",
			classes: []string{},
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runUnknownFixture(test, "Component.tsx", testCase.source)
			if len(result.Diagnostics) != len(testCase.classes) {
				test.Fatalf("got %d findings, want %d", len(result.Diagnostics), len(testCase.classes))
			}
			text := result.SourceFile.Text()
			for index, diagnostic := range result.Diagnostics {
				reported := text[diagnostic.Range.Pos():diagnostic.Range.End()]
				if reported != testCase.classes[index] {
					test.Errorf("finding %d covers %q, want %q", index, reported, testCase.classes[index])
				}
			}
		})
	}
}

// TestUnknownIgnoreExemptsProjectClasses covers the option this rule cannot ship without.
//
// Any project with hand-written CSS has class names Tailwind does not define, usually under a prefix
// of its own, and on a real tree those can be the entire finding count. `orbit-banner` stands in for
// one: a hand-written class under the project's `orbit-` prefix, which no theme here declares.
func TestUnknownIgnoreExemptsProjectClasses(test *testing.T) {
	test.Parallel()
	options := NoUnknownClassesOptions{Ignore: []string{`^orbit-`}}

	exempted := runUnknownFixtureWithOptions(test, "Component.tsx",
		`const element = <div className="orbit-banner flex" />;`, &options)
	if len(exempted.Diagnostics) != 0 {
		test.Errorf("an ignored class should not be reported, got %d findings", len(exempted.Diagnostics))
	}

	// Without the exemption it reports, so the test above is not passing because the rule is inert.
	unexempted := runUnknownFixture(test, "Component.tsx",
		`const element = <div className="orbit-banner flex" />;`)
	if len(unexempted.Diagnostics) == 0 {
		test.Fatal("the rule stayed silent without an ignore option, so the exemption proves nothing")
	}

	// And the exemption must not swallow real typos alongside it.
	stillReported := runUnknownFixtureWithOptions(test, "Component.tsx",
		`const element = <div className="orbit-banner flx" />;`, &options)
	if len(stillReported.Diagnostics) != 1 {
		test.Errorf("the ignore pattern should exempt only what it matches, got %d findings",
			len(stillReported.Diagnostics))
	}
}

// TestKnownRootWithUnknownValueIsReported is what the pinned gap became.
//
// The gap this replaces said `text-huge` had root `text`, did not compile, and was not reported,
// because catching it "needs the theme's per-root value scales plus the arbitrary-value grammar,
// which is closer to reimplementing the utility resolver than to reading a table". Its own comment
// asked for the assertion to be deleted deliberately when that changed.
//
// It changed. `ResolveFunctionalUtilityValue` is that resolver, ported, and #31bbwty gave it a
// description for all 57 closure-registered roots, so `classExistsIn` asks whether the value resolves
// rather than stopping at whether the class parses.
//
// The complement is asserted beside it, since a rule that reports everything would pass the first
// half alone.
func TestKnownRootWithUnknownValueIsReported(test *testing.T) {
	test.Parallel()
	forEachTailwindSystem(test, func(test *testing.T, designSystem DesignSystemResult) {
		system := designSystem.System

		if classExistsIn("text-huge", system) {
			test.Error("`text-huge` has root `text` and compiles to nothing, so the rule must report it")
		}
		if !classExistsIn("text-lg", system) {
			test.Error("`text-lg` compiles, so reporting it would be a false positive on working code")
		}

		// An unknown root is still caught, which is the half that worked before this change.
		if classExistsIn("txt-huge", system) {
			test.Error("an unknown root must still be reported, or the rule catches nothing")
		}
	})
}

// TestUnknownClassFixturesActuallyRan is what stops this file from going green on nothing.
//
// Every fixture above skips when no installed tailwindcss can be found, and a skip is the honest
// answer for a machine that genuinely has none. The problem is that `go test` prints `ok` for a
// package whose every case skipped, so a mistake in where the walk starts looks identical to a
// machine that lacks the package. That was not hypothetical for the class-order fixtures and the
// same helper shape is used here.
//
// So this test fails where the others skip, and it asserts the end-to-end claim rather than only the
// precondition: the fixture stylesheet's own `@utility` block must read as known and a typo must
// still report, in the same program. A design system that built but resolved nothing would fail the
// first; a rule that had stopped answering would fail the second.
func TestUnknownClassFixturesActuallyRan(test *testing.T) {
	test.Parallel()
	packageRoot := unknownFixturePackageRoot(test)
	if packageRoot == "" {
		test.Fatalf(
			"the vendored tailwindcss at %s is missing, so every fixture in this file would skip and the "+
				"package still report ok; restore it (internal/lint/rules/tailwind/vendored)",
			vendored.TailwindPackageRootForTest(test),
		)
	}

	result := runUnknownFixture(test, "Component.tsx",
		`const element = <div className="synthetic-static flx" />;`)
	rule_testing.ExpectFindings(test, result, "unknownClass")
}

// independentLiveSystem builds a design system that shares nothing with any consumer repository's theme.
//
// The stylesheet is copied beside a staged `node_modules` rather than read in place,
// because `@import "tailwindcss"` resolves through whichever `node_modules` is reachable from where
// the file sits, and this checkout vendors none. Its own file comment says the same.
func independentLiveSystem(test *testing.T) *tailwindcollapse.LoadedDesignSystem {
	test.Helper()

	// The theme is this package's own testdata, under its generator, so it is read from the package
	// directory and is never absent. It used to be read from five directories up, outside the repository,
	// where it never was, so every test that asked for it skipped on every machine (#sycrdr6).
	source := filepath.Join("tools", "generate_descriptor_base", "testdata", "independent_theme.css")
	contents, readError := os.ReadFile(source)
	if readError != nil {
		test.Fatalf("reading the independent theme: %v", readError)
	}

	packageRoot := unknownFixturePackageRoot(test)
	if packageRoot == "" {
		test.Skip("no installed tailwindcss on this machine")
	}

	// Beside the installed package's own tree, so the bare specifier resolves by the same upward
	// walk a repository uses. Written into a directory this test owns and removed after.
	staging := test.TempDir()
	if symlinkError := os.Symlink(packageRoot, filepath.Join(staging, "node_modules_link")); symlinkError != nil {
		test.Fatalf("linking the installed tailwindcss beside the independent stylesheet: %v", symlinkError)
	}
	modules := filepath.Join(staging, "node_modules")
	if createDirectoryError := os.MkdirAll(modules, 0o755); createDirectoryError != nil {
		test.Fatalf("creating the staging node_modules: %v", createDirectoryError)
	}
	if symlinkError := os.Symlink(packageRoot, filepath.Join(modules, "tailwindcss")); symlinkError != nil {
		test.Fatalf("linking the installed tailwindcss into the staging directory: %v", symlinkError)
	}

	entryPoint := filepath.Join(staging, "independent_theme.css")
	if writeError := os.WriteFile(entryPoint, contents, 0o644); writeError != nil {
		test.Fatalf("staging the independent stylesheet: %v", writeError)
	}

	system, loadDesignSystemError := tailwindcollapse.LoadDesignSystem(tailwindcollapse.LoadOptions{
		EntryPoint:          entryPoint,
		TailwindPackageRoot: findTailwindPackageRoot(staging, diskFileExists),
	})
	if loadDesignSystemError != nil {
		test.Fatalf("loading the independent design system: %v", loadDesignSystemError)
	}
	return system
}
