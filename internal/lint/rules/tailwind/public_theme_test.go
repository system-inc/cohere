package tailwind

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tailwindengine "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
	"github.com/system-inc/cohere/internal/lint/rules/tailwind/vendored"
)

// The public twins of the engine assertions that needed a shape only a repository's own stylesheet had
// (#f598zk0, tiers B and C). Each live original stays where it is, opt-in through its corpus; the twin asks the
// same question of collapse/testdata/public_theme, which declares the shape on purpose, so it runs on
// every machine and fails where the original would have skipped.

// publicThemeSystem is the public theme's design system, over the vendored tailwindcss.
func publicThemeSystem(t *testing.T) DesignSystemResult {
	t.Helper()
	entryPoint, err := filepath.Abs(filepath.Join("collapse", "testdata", "public_theme", "theme.css"))
	if err != nil {
		t.Fatalf("resolving the public theme: %v", err)
	}
	system, err := tailwindengine.LoadDesignSystem(tailwindengine.LoadOptions{
		EntryPoint:          entryPoint,
		TailwindPackageRoot: vendored.TailwindPackageRoot(),
	})
	if err != nil {
		t.Fatalf("loading the public theme: %v", err)
	}
	return DesignSystemResult{System: system, Table: tailwindengine.NewTable(system), EntryPoint: entryPoint}
}

// themeSystems are the public theme and ahra's, for an assertion whose expectations were measured on
// both and agree (#f598zk0, tier C): it runs the same expectations on each, the public one everywhere and
// ahra's opt-in through its corpus.
var themeSystems = []engineSystem{
	{name: "public", load: publicThemeSystem},
	{name: "ahra", load: classOrderLiveRepositorySystem},
}

// forEachThemeSystem runs an assertion once per theme system, each as its own subtest.
func forEachThemeSystem(t *testing.T, run func(t *testing.T, designSystem DesignSystemResult)) {
	t.Helper()
	for _, system := range themeSystems {
		t.Run(system.name, func(t *testing.T) {
			t.Parallel()
			run(t, system.load(t))
		})
	}
}

// TestClassOrderReadsTheRepositoryOnThePublicTheme is TestClassOrderLiveReadsTheRepositoryRatherThanATable's
// twin: a static `@utility` only the stylesheet declares gets a key, from the design system and not a table.
func TestClassOrderReadsTheRepositoryOnThePublicTheme(t *testing.T) {
	t.Parallel()
	designSystem := publicThemeSystem(t)
	const ownClass = "surface-card"
	if !designSystem.System.HasUtility(ownClass, tailwindengine.UtilityKindStatic) {
		t.Fatalf("the public theme declares `@utility %s` and the design system does not know it", ownClass)
	}
	keys, unplaceable, resolved := classOrderKeys([]string{ownClass, "flex"}, designSystem.System, designSystem.Table)
	if !resolved {
		t.Fatalf("could not place %q", unplaceable)
	}
	if _, placed := keys[ownClass]; !placed {
		t.Errorf("%s got no key, so the order came from a table that cannot know a repository's own utilities", ownClass)
	}
}

// TestExistenceIsNotReadFromPropertyTablesOnThePublicTheme is TestExistenceIsNotReadFromPropertyTables'
// twin. `glow-soft` declares only a custom property, so it exists and no property table can say so.
func TestExistenceIsNotReadFromPropertyTablesOnThePublicTheme(t *testing.T) {
	t.Parallel()
	system := publicThemeSystem(t).System
	lostByShortcut := 0
	for _, className := range []string{"container", "from-black/70", "to-transparent", "glow-soft"} {
		if !classExistsIn(className, system) {
			t.Errorf("%s is real and the rule reports it unknown", className)
			continue
		}
		if _, canResolve := resolveClassFactsIn(className, DesignSystemResult{System: system}); !canResolve {
			lostByShortcut++
		}
	}
	if lostByShortcut == 0 {
		t.Fatal("every class resolved through the property tables, so this fixture cannot tell existence from them")
	}
}

// TestExistenceComesFromTheRepositoryOnThePublicThemes is TestExistenceComesFromTheRepositoryRatherThanATable's
// twin, with the public theme in ahra's place: two design systems that declare different `@utility`
// blocks must disagree about them, which a generated table cannot do.
func TestExistenceComesFromTheRepositoryOnThePublicThemes(t *testing.T) {
	t.Parallel()
	public := publicThemeSystem(t).System
	independent := independentLiveSystem(t)
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
			t.Errorf("%s: the public theme says exists=%v, want %v", testCase.className, got, testCase.onPublic)
		}
		if got := classExistsIn(testCase.className, independent); got != testCase.onIndependent {
			t.Errorf("%s: the independent theme says exists=%v, want %v", testCase.className, got, testCase.onIndependent)
		}
	}
}

// TestRepositoryNamesAreNoLongerVouchedForOnThePublicThemes is
// TestRepositoryNamesAreNoLongerVouchedForByTheFramework's twin: names one stylesheet declares are
// known there and unknown on a system that does not declare them.
func TestRepositoryNamesAreNoLongerVouchedForOnThePublicThemes(t *testing.T) {
	t.Parallel()
	public := publicThemeSystem(t).System
	independent := independentLiveSystem(t)
	for _, name := range []string{"surface-card", "glow-soft", "pane", "rail"} {
		if !classExistsIn(name, public) {
			t.Errorf("%s is declared by the public theme and the rule reports it unknown there", name)
		}
		if classExistsIn(name, independent) {
			t.Errorf("%s is known on the independent theme, which never declares it, so something vouches for it "+
				"beyond the repository", name)
		}
	}
}

// TestClassOrderRanksByTheReadingThatCompilesOnThePublicTheme is TestClassOrderRanksByTheReadingThatCompiles'
// twin (#f598zk0, tier C). The wanted orders are Prettier's Tailwind plugin (0.8.1) over the public
// theme, measured the way the original's were over ahra's: each list formatted as a className with
// tailwindStylesheet set to collapse/testdata/public_theme/theme.css.
//
// `shadow--0` reads first as the theme's `@utility shadow--*` with value `0`, needing a `--shadow-0` the
// theme does not declare, and second as the framework's `shadow` with value `-0`, which finds
// `--shadow--0`. `hover:tone--0-4` reads only as `@utility tone--*` with value `0-4`, which no
// `--color-tone-*` key answers, so it ranks as a null.
func TestClassOrderRanksByTheReadingThatCompilesOnThePublicTheme(t *testing.T) {
	t.Parallel()
	designSystem := publicThemeSystem(t)

	for _, testCase := range []struct {
		input, want []string
	}{
		{[]string{"shadow--0", "surface--2", "edge--1", "border"}, []string{"border", "edge--1", "surface--2", "shadow--0"}},
		{[]string{"shadow--6", "hover:shadow--3", "p-2", "rounded-md"}, []string{"rounded-md", "p-2", "shadow--6", "hover:shadow--3"}},
		{[]string{"flex", "dark:bg-transparent", "hover:tone--0-4"}, []string{"hover:tone--0-4", "flex", "dark:bg-transparent"}},
	} {
		ordered, decided := orderClasses(testCase.input, designSystem, defaultClassOrderOptions())
		if !decided {
			t.Errorf("%v was declined; every class in it has an engine answer", testCase.input)
			continue
		}
		if strings.Join(ordered, " ") != strings.Join(testCase.want, " ") {
			t.Errorf("%v ordered as %v, the plugin writes %v", testCase.input, ordered, testCase.want)
		}
	}
}

// TestClassExistenceAsksTheEvaluatorForRepositoryRootsOnThePublicTheme is
// TestClassExistenceAsksTheEvaluatorForRepositoryRoots' twin. The answers are the engine's
// `candidatesToCss` over the public theme: `tone--0-4` has no `--color-tone-0-4`, `surface--2/50` puts a
// modifier on a utility that takes none, `shadow--0` is reached only by its second reading, and
// `prose-flow` declares nothing at its own level and still generates CSS.
func TestClassExistenceAsksTheEvaluatorForRepositoryRootsOnThePublicTheme(t *testing.T) {
	t.Parallel()
	designSystem := publicThemeSystem(t)

	for _, dead := range []string{"tone--0-4", "hover:tone--0-4", "surface--2/50"} {
		if classExistsIn(dead, designSystem.System) {
			t.Errorf("%q generates no CSS and was read as existing", dead)
		}
	}
	for _, live := range []string{"tone--1", "tone--4", "surface--2", "shadow--0", "hover:shadow--3", "prose-flow"} {
		if !classExistsIn(live, designSystem.System) {
			t.Errorf("%q generates CSS and was read as unknown", live)
		}
	}
}

// TestRepositoryUtilityRootsAreNeverReportedOnThePublicTheme is TestRepositoryUtilityRootsAreNeverReported's
// twin, and fails where the original skips: every class here reads as a root the theme declares by
// `@utility`, so each one is checked, and a theme that stopped declaring them is a failure rather than a
// skip.
func TestRepositoryUtilityRootsAreNeverReportedOnThePublicTheme(t *testing.T) {
	t.Parallel()
	system := publicThemeSystem(t).System

	for _, className := range []string{"shadow--3", "tone--1", "edge--1", "surface--2"} {
		candidates := tailwindengine.ParseCandidate(className, system)
		if len(candidates) == 0 {
			t.Errorf("%s does not parse, so it checks nothing", className)
			continue
		}
		if !system.DeclaresFunctionalUtility(candidates[0].Root) {
			t.Errorf("%s reads as %q, which the public theme does not declare by `@utility`", className, candidates[0].Root)
			continue
		}
		if !classExistsIn(className, system) {
			t.Errorf("%s reads as the theme's root %q and is reported, which is a false positive on a declared utility",
				className, candidates[0].Root)
		}
	}
}

// TestClassOrderPlacesRootsDeclaredBothWaysOnThePublicTheme is TestClassOrderLiveDeclinesNoList's question
// on the public theme (#f598zk0): `pane`, `rail`, `appear`, `swell` and `drift-up` are each declared as a
// static `@utility` and as a functional one, and a list holding both forms is placed and sorted, never
// declined. The wanted orders are Prettier's Tailwind plugin (0.8.1) over the public theme.
func TestClassOrderPlacesRootsDeclaredBothWaysOnThePublicTheme(t *testing.T) {
	t.Parallel()
	designSystem := publicThemeSystem(t)

	for _, testCase := range []struct {
		input, want []string
	}{
		{[]string{"appear-50", "pane-wide", "appear", "rail", "pane", "rail-thin"}, []string{"pane", "pane-wide", "rail-thin", "rail", "appear-50", "appear"}},
		{[]string{"swell-50", "drift-up", "swell", "drift-up-4", "p-2", "flex"}, []string{"flex", "p-2", "swell-50", "drift-up", "drift-up-4", "swell"}},
	} {
		ordered, decided := orderClasses(testCase.input, designSystem, defaultClassOrderOptions())
		if !decided {
			t.Errorf("%v was declined; every class in it is a root the theme declares", testCase.input)
			continue
		}
		if strings.Join(ordered, " ") != strings.Join(testCase.want, " ") {
			t.Errorf("%v ordered as %v, the plugin writes %v", testCase.input, ordered, testCase.want)
		}
	}
}

// TestRepositoryUtilitiesAreCompiledOnThePublicTheme is TestRepositoryUtilitiesAreCompiledRatherThanLookedUp's
// repository half on the public theme (#f598zk0): a class the theme declares by `@utility` is answered by
// compiling that block, to the property the block sets, and the same class on a design system that does
// not declare it is not compiled there. `appear-50` sets only a custom property, which is not a property
// a conflict can be about, so the repository half declines it rather than answering with nothing.
func TestRepositoryUtilitiesAreCompiledOnThePublicTheme(t *testing.T) {
	t.Parallel()
	public := publicThemeSystem(t)

	for className, properties := range map[string][]string{
		"surface--2": {"background-color"}, "tone--1": {"color"}, "tone--4": {"color"}, "edge--1": {"border-color"},
		"hover:surface--2": {"background-color"}, "dark:tone--1": {"color"}, "ink--strong": {"color"},
		"lift--sm": {"box-shadow"},
	} {
		facts, compiled := repositoryClassFacts(className, "", public)
		if !compiled {
			t.Errorf("%s was not answered by compiling the public theme's own @utility block", className)
			continue
		}
		if !slices.Equal(facts.Properties, properties) {
			t.Errorf("%s compiled to %v, want %v, the properties its @utility block sets", className, facts.Properties, properties)
		}
	}

	if facts, compiled := repositoryClassFacts("appear-50", "", public); compiled {
		t.Errorf("appear-50 sets only a custom property and was answered with %v", facts.Properties)
	}

	independent := DesignSystemResult{System: independentLiveSystem(t)}
	if _, compiledElsewhere := repositoryClassFacts("surface--2", "", independent); compiledElsewhere {
		t.Error("surface--2 was answered by compiling a block on a design system that declares none, so " +
			"the repository half is not reading the repository in front of the rule")
	}
}
