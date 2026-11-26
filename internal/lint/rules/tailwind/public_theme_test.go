package tailwind

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tailwindcollapse "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
	"github.com/system-inc/cohere/internal/lint/rules/tailwind/vendored"
)

// Tailwind assertions that need a shape only a repository's own stylesheet has (#f598zk0, tiers B and C), asked
// of collapse/testdata/public_theme, which declares each shape on purpose, so they run on every machine. They
// began as twins of originals that read a consumer's checkout, and replaced them when cohere's tests stopped
// reading other repositories (#5wj23dd).

// publicThemeSystem is the public theme's design system, over the vendored tailwindcss.
func publicThemeSystem(test *testing.T) DesignSystemResult {
	test.Helper()
	entryPoint, absolutePathError := filepath.Abs(filepath.Join("collapse", "testdata", "public_theme", "theme.css"))
	if absolutePathError != nil {
		test.Fatalf("resolving the public theme: %v", absolutePathError)
	}
	system, loadDesignSystemError := tailwindcollapse.LoadDesignSystem(tailwindcollapse.LoadOptions{
		EntryPoint:          entryPoint,
		TailwindPackageRoot: vendored.TailwindPackageRootForTest(test),
	})
	if loadDesignSystemError != nil {
		test.Fatalf("loading the public theme: %v", loadDesignSystemError)
	}
	return loadedDesignSystemResult(system, entryPoint)
}

// themeSystems are the themes an assertion whose expectations hold on any theme runs on (#f598zk0, tier C): the
// public theme, everywhere.
var themeSystems = []tailwindSystem{
	{name: "public", load: publicThemeSystem},
}

// forEachThemeSystem runs an assertion once per theme system, each as its own subtest.
func forEachThemeSystem(test *testing.T, run func(test *testing.T, designSystem DesignSystemResult)) {
	test.Helper()
	for _, system := range themeSystems {
		test.Run(system.name, func(test *testing.T) {
			test.Parallel()
			run(test, system.load(test))
		})
	}
}

// TestClassOrderReadsTheRepositoryOnThePublicTheme is the control that class order reads the repository: a static
// `@utility` only the stylesheet declares gets a key, from the design system and not a table, so a sort that silently
// kept reading generated tables fails here while every other order test stays green.
func TestClassOrderReadsTheRepositoryOnThePublicTheme(test *testing.T) {
	test.Parallel()
	designSystem := publicThemeSystem(test)
	const ownClass = "surface-card"
	if !designSystem.System.HasUtility(ownClass, tailwindcollapse.UtilityKindStatic) {
		test.Fatalf("the public theme declares `@utility %s` and the design system does not know it", ownClass)
	}
	keys, unplaceable, resolved := classOrderKeys([]string{ownClass, "flex"}, designSystem.System, designSystem.Table)
	if !resolved {
		test.Fatalf("could not place %q", unplaceable)
	}
	if _, placed := keys[ownClass]; !placed {
		test.Errorf(
			"%s got no key, so the order came from a table that cannot know a repository's own utilities",
			ownClass,
		)
	}
}

// TestExistenceIsNotReadFromPropertyTablesOnThePublicTheme is the known-dirty control for existence: the property
// tables leave out utilities that declare nothing a rule can compare, so reading existence from them reports valid
// classes. `glow-soft` declares only a custom property, so it exists and no property table can say so.
func TestExistenceIsNotReadFromPropertyTablesOnThePublicTheme(test *testing.T) {
	test.Parallel()
	designSystem := publicThemeSystem(test)
	system := designSystem.System
	lostByShortcut := 0
	for _, className := range []string{"container", "from-black/70", "to-transparent", "glow-soft"} {
		if !classExistsIn(className, system) {
			test.Errorf("%s is real and the rule reports it unknown", className)
			continue
		}
		if _, canResolve := resolveClassFactsIn(className, designSystem); !canResolve {
			lostByShortcut++
		}
	}
	if lostByShortcut == 0 {
		test.Fatal("every class resolved through the property tables, so this fixture cannot tell existence from them")
	}
}

// TestExistenceComesFromTheRepositoryOnThePublicThemes asks two design systems that declare different `@utility`
// blocks about them, and requires them to disagree, which a rule reading a generated table cannot do.
func TestExistenceComesFromTheRepositoryOnThePublicThemes(test *testing.T) {
	test.Parallel()
	public := publicThemeSystem(test).System
	independent := independentLiveSystem(test)
	testCases := []struct {
		className               string
		onPublic, onIndependent bool
	}{
		{"surface-card", true, false},
		{"pane-wide", true, false},
		{"synthetic-static", false, true},
		{"synthetic-fn-small", false, true},
		{"flex", true, true},
		{"px-4", true, true},
	}
	for _, testCase := range testCases {
		if got := classExistsIn(testCase.className, public); got != testCase.onPublic {
			test.Errorf("%s: the public theme says exists=%v, want %v", testCase.className, got, testCase.onPublic)
		}
		if got := classExistsIn(testCase.className, independent); got != testCase.onIndependent {
			test.Errorf(
				"%s: the independent theme says exists=%v, want %v",
				testCase.className,
				got,
				testCase.onIndependent,
			)
		}
	}
}

// TestRepositoryNamesAreNoLongerVouchedForOnThePublicThemes: names one stylesheet declares are known there and
// unknown on a system that does not declare them, so no repository's tokens ride the framework's tables.
func TestRepositoryNamesAreNoLongerVouchedForOnThePublicThemes(test *testing.T) {
	test.Parallel()
	public := publicThemeSystem(test).System
	independent := independentLiveSystem(test)
	for _, name := range []string{"surface-card", "glow-soft", "pane", "rail"} {
		if !classExistsIn(name, public) {
			test.Errorf("%s is declared by the public theme and the rule reports it unknown there", name)
		}
		if classExistsIn(name, independent) {
			test.Errorf("%s is known on the independent theme, which never declares it, so something vouches for it "+
				"beyond the repository", name)
		}
	}
}

// TestClassOrderRanksByTheReadingThatCompilesOnThePublicTheme: a class ranks by the first of its readings that
// compiles, and a class none of whose readings compile is a null (#f598zk0, tier C). The wanted orders are
// Prettier's Tailwind plugin (0.8.1) over the public theme: each list formatted as a className with
// tailwindStylesheet set to collapse/testdata/public_theme/theme.css.
//
// `shadow--0` reads first as the theme's `@utility shadow--*` with value `0`, needing a `--shadow-0` the
// theme does not declare, and second as the framework's `shadow` with value `-0`, which finds
// `--shadow--0`. `hover:tone--0-4` reads only as `@utility tone--*` with value `0-4`, which no
// `--color-tone-*` key answers, so it ranks as a null.
func TestClassOrderRanksByTheReadingThatCompilesOnThePublicTheme(test *testing.T) {
	test.Parallel()
	designSystem := publicThemeSystem(test)

	for _, testCase := range []struct {
		input, want []string
	}{
		{
			[]string{"shadow--0", "surface--2", "edge--1", "border"},
			[]string{"border", "edge--1", "surface--2", "shadow--0"},
		},
		{
			[]string{"shadow--6", "hover:shadow--3", "p-2", "rounded-md"},
			[]string{"rounded-md", "p-2", "shadow--6", "hover:shadow--3"},
		},
		{
			[]string{"flex", "dark:bg-transparent", "hover:tone--0-4"},
			[]string{"hover:tone--0-4", "flex", "dark:bg-transparent"},
		},
	} {
		ordered, decided := orderClasses(testCase.input, designSystem, defaultClassOrderOptions())
		if !decided {
			test.Errorf("%v was declined; every class in it has Tailwind answer", testCase.input)
			continue
		}
		if strings.Join(ordered, " ") != strings.Join(testCase.want, " ") {
			test.Errorf("%v ordered as %v, the plugin writes %v", testCase.input, ordered, testCase.want)
		}
	}
}

// TestClassExistenceAsksTheEvaluatorForRepositoryRootsOnThePublicTheme: existence asks the evaluator for a
// repository root rather than trusting it, and takes any reading that resolves. The answers are Tailwind's
// `candidatesToCss` over the public theme: `tone--0-4` has no `--color-tone-0-4`, `surface--2/50` puts a
// modifier on a utility that takes none, `shadow--0` is reached only by its second reading, and
// `prose-flow` declares nothing at its own level and still generates CSS.
func TestClassExistenceAsksTheEvaluatorForRepositoryRootsOnThePublicTheme(test *testing.T) {
	test.Parallel()
	designSystem := publicThemeSystem(test)

	for _, dead := range []string{"tone--0-4", "hover:tone--0-4", "surface--2/50"} {
		if classExistsIn(dead, designSystem.System) {
			test.Errorf("%q generates no CSS and was read as existing", dead)
		}
	}
	for _, live := range []string{"tone--1", "tone--4", "surface--2", "shadow--0", "hover:shadow--3", "prose-flow"} {
		if !classExistsIn(live, designSystem.System) {
			test.Errorf("%q generates CSS and was read as unknown", live)
		}
	}
}

// TestRepositoryUtilityRootsAreNeverReportedOnThePublicTheme: a repository's own `@utility` root is never reported,
// whatever its value looks like. Every class here reads as a root the theme declares by `@utility`, so each one is
// checked, and a theme that stopped declaring them is a failure rather than a skip.
func TestRepositoryUtilityRootsAreNeverReportedOnThePublicTheme(test *testing.T) {
	test.Parallel()
	system := publicThemeSystem(test).System

	for _, className := range []string{"shadow--3", "tone--1", "edge--1", "surface--2"} {
		candidates := tailwindcollapse.ParseCandidate(className, system)
		if len(candidates) == 0 {
			test.Errorf("%s does not parse, so it checks nothing", className)
			continue
		}
		if !system.DeclaresFunctionalUtility(candidates[0].Root) {
			test.Errorf(
				"%s reads as %q, which the public theme does not declare by `@utility`",
				className,
				candidates[0].Root,
			)
			continue
		}
		if !classExistsIn(className, system) {
			test.Errorf(
				"%s reads as the theme's root %q and is reported, which is a false positive on a declared utility",
				className,
				candidates[0].Root,
			)
		}
	}
}

// TestClassOrderPlacesRootsDeclaredBothWaysOnThePublicTheme pins that a root declared both ways never costs a list
// its order (#f598zk0): `pane`, `rail`, `appear`, `swell` and `drift-up` are each declared as a static `@utility`
// and as a functional one, and a list holding both forms is placed and sorted, never declined. A design system that
// kept one kind per root once declined a consumer's list on `data-[show=false]:fade-out` this way. The wanted
// orders are Prettier's Tailwind plugin (0.8.1) over the public theme.
func TestClassOrderPlacesRootsDeclaredBothWaysOnThePublicTheme(test *testing.T) {
	test.Parallel()
	designSystem := publicThemeSystem(test)

	for _, testCase := range []struct {
		input, want []string
	}{
		{
			[]string{"appear-50", "pane-wide", "appear", "rail", "pane", "rail-thin"},
			[]string{"pane", "pane-wide", "rail-thin", "rail", "appear-50", "appear"},
		},
		{
			[]string{"swell-50", "drift-up", "swell", "drift-up-4", "p-2", "flex"},
			[]string{"flex", "p-2", "swell-50", "drift-up", "drift-up-4", "swell"},
		},
	} {
		ordered, decided := orderClasses(testCase.input, designSystem, defaultClassOrderOptions())
		if !decided {
			test.Errorf("%v was declined; every class in it is a root the theme declares", testCase.input)
			continue
		}
		if strings.Join(ordered, " ") != strings.Join(testCase.want, " ") {
			test.Errorf("%v ordered as %v, the plugin writes %v", testCase.input, ordered, testCase.want)
		}
	}
}

// TestRepositoryUtilitiesAreCompiledOnThePublicTheme is the repository half of no-conflicting-classes on the
// public theme (#f598zk0): a class the theme declares by `@utility` is answered by
// compiling that block, to the property the block sets, and the same class on a design system that does
// not declare it is not compiled there. `appear-50` sets only a custom property, which is not a property
// a conflict can be about, so the repository half declines it rather than answering with nothing.
func TestRepositoryUtilitiesAreCompiledOnThePublicTheme(test *testing.T) {
	test.Parallel()
	public := publicThemeSystem(test)

	for className, properties := range map[string][]string{
		"surface--2": {"background-color"}, "tone--1": {"color"}, "tone--4": {"color"}, "edge--1": {"border-color"},
		"hover:surface--2": {"background-color"}, "dark:tone--1": {"color"}, "ink--strong": {"color"},
		"lift--sm": {"box-shadow"},
	} {
		facts, compiled := repositoryClassFacts(className, "", public)
		if !compiled {
			test.Errorf("%s was not answered by compiling the public theme's own @utility block", className)
			continue
		}
		if !slices.Equal(facts.Properties, properties) {
			test.Errorf(
				"%s compiled to %v, want %v, the properties its @utility block sets",
				className,
				facts.Properties,
				properties,
			)
		}
	}

	if facts, compiled := repositoryClassFacts("appear-50", "", public); compiled {
		test.Errorf("appear-50 sets only a custom property and was answered with %v", facts.Properties)
	}

	independent := loadedDesignSystemResult(independentLiveSystem(test), "")
	if _, compiledElsewhere := repositoryClassFacts("surface--2", "", independent); compiledElsewhere {
		test.Error("surface--2 was answered by compiling a block on a design system that declares none, so " +
			"the repository half is not reading the repository in front of the rule")
	}
}

// TestColorTokensReadAsColorsOnThePublicTheme: a repository's own colour tokens are recognised as colours,
// which a generated table could not do. `valueIsColorIn` once read `ColorNames`, names generated from one
// consumer, and a theme declaring a colour the table lacked read `bg-brand` on the wrong arm,
// `background-image` rather than `background-color`, so it failed to conflict with `bg-red-500`. Asked of
// the independent theme, so it runs on every machine (#f598zk0): every `--color-*` key reads as a colour,
// the framework palette's and the theme's own `--color-weird-*` alike.
func TestColorTokensReadAsColorsOnThePublicTheme(test *testing.T) {
	test.Parallel()
	system := publicDesignSystem(test).System

	checkedTokens, ownTokens := 0, 0
	for _, key := range system.Theme().KeysInNamespaces([]string{"--color"}) {
		if !valueIsColorIn("bg-"+key, "bg", system) {
			test.Errorf("the public theme declares --color-%s and `bg-%s` does not read as a colour", key, key)
		}
		checkedTokens++
		if strings.HasPrefix(key, "weird-") {
			ownTokens++
		}
	}
	// The theme's own keys are the ones no framework table could vouch for, so they must be among
	// those checked, or the test proves only the palette.
	if checkedTokens < 200 || ownTokens != 2 {
		test.Fatalf(
			"checked %d colour tokens, %d of them the theme's own; want the framework palette and both --color-weird keys",
			checkedTokens,
			ownTokens,
		)
	}
}
