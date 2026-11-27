package tailwind

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rules/tailwind/vendored"
	"github.com/system-inc/cohere/internal/testing/repository"
)

// The fixture is the theme the shipped Tailwind 4.3.3 resolved for every stylesheet in the
// corpus, captured by internal/lint/rules/tailwind/tools/generate_theme and checked in next to this test.
//
// Measured rather than transcribed, because the two are different claims and the ignored-key map is
// where they part. Reading `theme.ts` suggests `--font` names every key beginning `--font-`; asking
// Tailwind says `--font-weight-*` and `--font-size-*` are excluded from it. A hand-written
// expectation set would have encoded the plausible version and agreed with Tailwind on every
// namespace that has no ignored siblings, which is most of them.
//
// The corpus has two halves and needs both. The repository half is the exit criterion for this
// component: two design systems on the same Tailwind, whose themes differ, each diffed key by key against
// what Tailwind built from the same files. They were two real repositories and are now two themes written
// in cohere, testdata/public_theme and testdata/planted_repository/styles, since cohere's tests no longer
// read other repositories (#5wj23dd, #pg78xtp). The synthetic half reaches the branches a real
// theme never does, `initial` deletion, namespace clearing, `@theme default` losing to a
// non-default value, prefixes, and the dot-to-underscore retry, which is roughly half of `theme.ts`
// and all of the half where a port fails silently.

type themeCorpus struct {
	TailwindVersion     string      `json:"tailwindVersion"`
	SyntheticCaseCount  int         `json:"syntheticCaseCount"`
	RepositoryCaseCount int         `json:"repositoryCaseCount"`
	Cases               []themeCase `json:"cases"`
}

// themeCase is one stylesheet and the theme Tailwind resolved from it.
//
// A synthetic case carries its stylesheet inline in Input. A repository case carries EntryPath
// instead, because the Go side must resolve the same `@import` graph from disk: the import
// following is part of what is under test, and inlining the entry file would test the loader
// against a single file it never has to resolve.
type themeCase struct {
	Name      string       `json:"name"`
	Source    string       `json:"source"`
	Input     string       `json:"input"`
	EntryPath string       `json:"entryPath"`
	Theme     themeFixture `json:"theme"`
}

type themeFixture struct {
	// Prefix is null in JSON when the theme carries none, which unmarshals to the empty string,
	// matching Theme.Prefix. See its doc comment for why the two collapse safely.
	Prefix             string                    `json:"prefix"`
	Size               int                       `json:"size"`
	Entries            []themeEntryFixture       `json:"entries"`
	NamespaceResults   []themeNamespaceFixture   `json:"namespaceResults"`
	Resolutions        []themeResolutionFixture  `json:"resolutions"`
	ResolveWithResults []themeResolveWithFixture `json:"resolveWithResults"`
	OptionQueries      []themeOptionFixture      `json:"optionQueries"`
}

type themeEntryFixture struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Options int    `json:"options"`
}

type themeNamespaceFixture struct {
	Namespace string `json:"namespace"`
	Entries   []struct {
		Key       string `json:"key"`
		KeyIsNull bool   `json:"keyIsNull"`
		Value     string `json:"value"`
	} `json:"entries"`
	KeysInNamespace []string `json:"keysInNamespace"`
}

// themeResolutionFixture is one `resolve` / `resolveValue` pair. Both are nullable, and null means
// the lookup found nothing, which is a different answer than the empty string: a theme entry
// declared with an empty value resolves successfully to "".
type themeResolutionFixture struct {
	CandidateValue       string   `json:"candidateValue"`
	CandidateValueIsNull bool     `json:"candidateValueIsNull"`
	ThemeKeys            []string `json:"themeKeys"`
	Resolve              *string  `json:"resolve"`
	ResolveValue         *string  `json:"resolveValue"`
}

type themeResolveWithFixture struct {
	CandidateValue string            `json:"candidateValue"`
	ThemeKeys      []string          `json:"themeKeys"`
	NestedKeys     []string          `json:"nestedKeys"`
	Value          *string           `json:"value"`
	Extra          map[string]string `json:"extra"`
}

type themeOptionFixture struct {
	Key        string  `json:"key"`
	Get        *string `json:"get"`
	HasDefault bool    `json:"hasDefault"`
	Options    int     `json:"options"`
}

// tailwindPackageRoots maps a repository case's name to the tailwindcss install its theme resolves
// `@import "tailwindcss"` against.
//
// Both are 4.3.3, which is the point: the two design systems differ by their own `@theme` content on
// an identical framework, so a table generated from either one is wrong for the other. That is the
// defect this whole component exists to fix, and the corpus is what proves it is real rather than
// theorized. Both are the vendored snapshot of 4.3.3, which is that identical framework, on any machine
// (#sycrdr6).
//
// theme_fixtures.json carries two repository cases, "public" (testdata/public_theme) and "planted"
// (testdata/planted_repository/styles), and the public theme's copy (#f598zk0) carries the first alone.
func tailwindPackageRoots(test *testing.T) map[string]string {
	test.Helper()
	snapshot := vendored.TailwindPackageRootForTest(test)
	return map[string]string{
		"public":  snapshot,
		"planted": snapshot,
	}
}

func loadThemeCorpus(test *testing.T, name string) themeCorpus {
	test.Helper()

	raw, readError := os.ReadFile(filepath.Join("testdata", name))
	if readError != nil {
		test.Fatalf("read theme fixture: %v", readError)
	}

	var fixture themeCorpus
	if decodeError := json.Unmarshal(raw, &fixture); decodeError != nil {
		test.Fatalf("decode theme fixture: %v", decodeError)
	}
	if len(fixture.Cases) == 0 {
		test.Fatal("theme fixture holds no cases")
	}
	return fixture
}

// buildThemeForCase builds a Theme the way the case demands: a synthetic case is parsed from its
// inline stylesheet, a repository case is loaded from disk through its whole import graph.
//
// A repository case's stylesheet is a path inside this repository, so a case whose stylesheet is
// missing fails rather than skips: a silently absent repository half would turn the exit criterion
// into a synthetic-only suite that still prints a green line, which is exactly the failure mode this
// slice has been bitten by.
func buildThemeForCase(test *testing.T, aCase themeCase) (*Theme, []SkippedDirective, bool) {
	test.Helper()

	if aCase.Source == "repository" {
		theme, skipped := loadRepositoryTheme(test, aCase)
		return theme, skipped, true
	}

	nodes, parseCssError := ParseCss(aCase.Input)
	if parseCssError != nil {
		test.Fatalf("parse synthetic stylesheet: %v", parseCssError)
	}
	loader := &themeLoader{theme: NewTheme(), visiting: make(map[string]bool)}
	if ingestError := loader.ingest(nodes, aCase.Name); ingestError != nil {
		test.Fatalf("ingest synthetic stylesheet: %v", ingestError)
	}
	return loader.theme, loader.skipped, true
}

// loadRepositoryTheme loads a repository case's theme from the stylesheet it records inside this
// repository, through the vendored tailwindcss its name maps to.
func loadRepositoryTheme(test *testing.T, aCase themeCase) (*Theme, []SkippedDirective) {
	test.Helper()
	packageRoot, ok := tailwindPackageRoots(test)[aCase.Name]
	if !ok {
		test.Fatalf("repository case %q has no tailwindcss package root registered in tailwindPackageRoots", aCase.Name)
	}
	if _, statError := os.Stat(filepath.Join(packageRoot, "index.css")); statError != nil {
		test.Fatalf("the vendored tailwindcss for %q is not at %s: %v", aCase.Name, packageRoot, statError)
	}
	entryPath := repository.Path(test, aCase.EntryPath)
	theme, skipped, loadThemeFromFileError := LoadThemeFromFile(entryPath, NodeStylesheetResolver(packageRoot))
	if loadThemeFromFileError != nil {
		test.Fatalf("load %s: %v", entryPath, loadThemeFromFileError)
	}
	return theme, skipped
}

// coveredRepositoryCases is how many repository cases the fixture's list holds, which is how many a run
// must check. Each records its stylesheet as a path inside this repository, so none is ever skipped for a
// missing checkout. The count is read from the list and held against the header the generator wrote, so
// a fixture whose list lost a case fails here rather than lowering the bar the coverage check holds.
func coveredRepositoryCases(test *testing.T, fixture themeCorpus) int {
	test.Helper()
	covered := 0
	for _, aCase := range fixture.Cases {
		if aCase.Source != "repository" {
			continue
		}
		if aCase.EntryPath == "" || filepath.IsAbs(aCase.EntryPath) {
			test.Fatalf(
				"repository case %q records the entry path %q, and it must be a path inside this repository",
				aCase.Name,
				aCase.EntryPath,
			)
		}
		covered++
	}
	if covered != fixture.RepositoryCaseCount {
		test.Fatalf("the fixture lists %d repository cases and its header says %d", covered, fixture.RepositoryCaseCount)
	}
	return covered
}

// TestThemeMatchesTailwind is the differential: every answer the port gives, against the answer
// Tailwind gave for the same question.
//
// The counts are printed on success rather than only on failure. A suite that compared twelve
// answers and a suite that compared two hundred thousand are indistinguishable from a green line,
// and this component's whole claim is about scale: it is checked against a design system Tailwind built,
// not against a reading.
//
// Run on the public theme's copy of the fixture (#f598zk0), whose repository case is testdata/public_theme,
// and on theme_fixtures.json, which adds the planted repository's theme beside it (#pg78xtp). Both themes
// are written in cohere, so the repository half runs on every machine.
func TestThemeMatchesTailwind(test *testing.T) {
	test.Parallel()
	forEachOracleCopy(test, "theme_fixtures.json", func(test *testing.T, fixtureName string) {
		expectThemeMatchesTailwind(test, loadThemeCorpus(test, fixtureName))
	})
	test.Run("two_themes", func(test *testing.T) {
		test.Parallel()
		expectThemeMatchesTailwind(test, loadThemeCorpus(test, "theme_fixtures.json"))
	})
}

// expectThemeMatchesTailwind compares every answer one copy of the fixture holds with the port's.
func expectThemeMatchesTailwind(test *testing.T, fixture themeCorpus) {
	test.Helper()

	var totalEntries int
	var totalNamespaces int
	var totalNamespaceEntries int
	var totalKeysInNamespace int
	var totalResolutions int
	var totalResolveWith int
	var totalOptionQueries int
	var totalNested int
	var repositoriesChecked int
	var syntheticChecked int

	for _, aCase := range fixture.Cases {
		// Not parallel: its subtests add to totals the test checks after them, and a parallel subtest would run
		// only after the test had returned, so the check would pass on nothing.
		test.Run(aCase.Name, func(test *testing.T) {
			theme, _, ok := buildThemeForCase(test, aCase)
			if !ok {
				return
			}

			if theme.Prefix != aCase.Theme.Prefix {
				test.Errorf("Prefix = %q, Tailwind says %q", theme.Prefix, aCase.Theme.Prefix)
			}
			if theme.Size() != aCase.Theme.Size {
				test.Errorf("Size() = %d, Tailwind says %d", theme.Size(), aCase.Theme.Size)
			}

			// Entries, compared positionally. Insertion order is part of the contract; comparing
			// as sets would pass a port that returned the right keys in an arbitrary order, and Go
			// map iteration is randomized per run, so that port would be nondeterministically
			// wrong in production while green here.
			entries := theme.Entries()
			if len(entries) != len(aCase.Theme.Entries) {
				test.Errorf(
					"Entries() returned %d entries, Tailwind returned %d",
					len(entries),
					len(aCase.Theme.Entries),
				)
			}
			for index := range aCase.Theme.Entries {
				if index >= len(entries) {
					test.Errorf("Entries()[%d] missing; Tailwind has %q", index, aCase.Theme.Entries[index].Key)
					continue
				}
				want := aCase.Theme.Entries[index]
				got := entries[index]
				if got.Key != want.Key || got.Value != want.Value || int(got.Options) != want.Options {
					test.Errorf(
						"Entries()[%d] = {%q, %q, %d}, Tailwind says {%q, %q, %d}",
						index, got.Key, got.Value, got.Options, want.Key, want.Value, want.Options,
					)
				}
			}
			totalEntries += len(aCase.Theme.Entries)

			// Namespaces. Both reads, because they differ in three ways that each change a real
			// count: the null key, sub-variables, and ignored-key filtering.
			for _, wantNamespace := range aCase.Theme.NamespaceResults {
				gotEntries := theme.Namespace(wantNamespace.Namespace)
				if len(gotEntries) != len(wantNamespace.Entries) {
					test.Errorf(
						"Namespace(%q) returned %d entries, Tailwind returned %d",
						wantNamespace.Namespace, len(gotEntries), len(wantNamespace.Entries),
					)
				}
				for index := range wantNamespace.Entries {
					if index >= len(gotEntries) {
						break
					}
					want := wantNamespace.Entries[index]
					got := gotEntries[index]
					if got.KeyIsNull != want.KeyIsNull || got.Key != want.Key || got.Value != want.Value {
						test.Errorf(
							"Namespace(%q)[%d] = {null=%v, %q, %q}, Tailwind says {null=%v, %q, %q}",
							wantNamespace.Namespace, index,
							got.KeyIsNull, got.Key, got.Value,
							want.KeyIsNull, want.Key, want.Value,
						)
					}
				}
				totalNamespaceEntries += len(wantNamespace.Entries)

				gotKeys := theme.KeysInNamespaces([]string{wantNamespace.Namespace})
				if !equalStringSlices(gotKeys, wantNamespace.KeysInNamespace) {
					test.Errorf(
						"KeysInNamespaces([%q]) = %s, Tailwind says %s",
						wantNamespace.Namespace,
						summarizeStrings(gotKeys),
						summarizeStrings(wantNamespace.KeysInNamespace),
					)
				}
				totalKeysInNamespace += len(wantNamespace.KeysInNamespace)
			}
			totalNamespaces += len(aCase.Theme.NamespaceResults)

			// Resolution. Both the `var()`-building Resolve and the literal ResolveValue, since a
			// port that confused them would answer every inline entry correctly and every other
			// one with the wrong half of the pair.
			for _, want := range aCase.Theme.Resolutions {
				candidatePresent := !want.CandidateValueIsNull

				gotResolve, gotResolveOk := theme.Resolve(
					want.CandidateValue,
					candidatePresent,
					want.ThemeKeys,
					ThemeOptionNone,
				)
				assertOptionalString(
					test, gotResolve, gotResolveOk, want.Resolve,
					fmt.Sprintf(
						"Resolve(%s, %v)",
						describeCandidate(want.CandidateValue, candidatePresent),
						want.ThemeKeys,
					),
				)

				gotValue, gotValueOk := theme.ResolveValue(want.CandidateValue, candidatePresent, want.ThemeKeys)
				assertOptionalString(
					test, gotValue, gotValueOk, want.ResolveValue,
					fmt.Sprintf(
						"ResolveValue(%s, %v)",
						describeCandidate(want.CandidateValue, candidatePresent),
						want.ThemeKeys,
					),
				)
			}
			totalResolutions += len(aCase.Theme.Resolutions)

			// ResolveWith and its nested keys, the `-*--nested` suffix form.
			for _, want := range aCase.Theme.ResolveWithResults {
				gotValue, gotExtra, gotOk := theme.ResolveWith(want.CandidateValue, want.ThemeKeys, want.NestedKeys)
				assertOptionalString(
					test, gotValue, gotOk, want.Value,
					fmt.Sprintf("ResolveWith(%q, %v, %v)", want.CandidateValue, want.ThemeKeys, want.NestedKeys),
				)
				if !gotOk {
					continue
				}
				if len(gotExtra) != len(want.Extra) {
					test.Errorf(
						"ResolveWith(%q, %v, %v) extra has %d keys, Tailwind has %d",
						want.CandidateValue, want.ThemeKeys, want.NestedKeys, len(gotExtra), len(want.Extra),
					)
				}
				for nestedKey, wantNested := range want.Extra {
					gotNested, present := gotExtra[nestedKey]
					if !present {
						test.Errorf(
							"ResolveWith(%q, %v, %v) extra missing %q; Tailwind says %q",
							want.CandidateValue, want.ThemeKeys, want.NestedKeys, nestedKey, wantNested,
						)
						continue
					}
					if gotNested != wantNested {
						test.Errorf(
							"ResolveWith(%q, %v, %v) extra[%q] = %q, Tailwind says %q",
							want.CandidateValue, want.ThemeKeys, want.NestedKeys, nestedKey, gotNested, wantNested,
						)
					}
				}
				totalNested += len(want.Extra)
			}
			totalResolveWith += len(aCase.Theme.ResolveWithResults)

			// Get, HasDefault and GetOptions. These read the options bitfield, which is what
			// decides whether Resolve returns a `var()` or a literal, so a port that stored the
			// values correctly and the bits wrongly would pass every count above.
			for _, want := range aCase.Theme.OptionQueries {
				gotGet, gotGetOk := theme.Get([]string{want.Key})
				assertOptionalString(test, gotGet, gotGetOk, want.Get, fmt.Sprintf("Get([%q])", want.Key))

				if got := theme.HasDefault(want.Key); got != want.HasDefault {
					test.Errorf("HasDefault(%q) = %v, Tailwind says %v", want.Key, got, want.HasDefault)
				}
				if got := theme.GetOptions(want.Key); int(got) != want.Options {
					test.Errorf("GetOptions(%q) = %d, Tailwind says %d", want.Key, got, want.Options)
				}
			}
			totalOptionQueries += len(aCase.Theme.OptionQueries)

			if aCase.Source == "repository" {
				repositoriesChecked++
			} else {
				syntheticChecked++
			}
		})
	}

	// Coverage. A count that has never been shown to be large is not a large count, and a suite
	// that lost its repository half would otherwise report the same green line as one that ran it.
	// Every repository case the fixture records must have run.
	if covered := coveredRepositoryCases(test, fixture); repositoriesChecked != covered {
		test.Errorf(
			"ran %d of %d repository cases: the exit criterion for this component is a comparison against two design systems",
			repositoriesChecked,
			covered,
		)
	}
	if syntheticChecked != fixture.SyntheticCaseCount {
		test.Errorf("ran %d of %d synthetic cases", syntheticChecked, fixture.SyntheticCaseCount)
	}

	totalComparisons := totalEntries + totalNamespaceEntries + totalKeysInNamespace +
		totalResolutions*2 + totalResolveWith + totalNested + totalOptionQueries*3

	test.Logf(
		"tailwind %s: %d repository and %d synthetic design systems; %d theme entries, "+
			"%d namespaces holding %d entries and %d namespace keys, %d resolutions, "+
			"%d resolveWith answers carrying %d nested values, %d option queries; %d compared answers",
		fixture.TailwindVersion, repositoriesChecked, syntheticChecked,
		totalEntries, totalNamespaces, totalNamespaceEntries, totalKeysInNamespace,
		totalResolutions, totalResolveWith, totalNested, totalOptionQueries,
		totalComparisons,
	)
}

// TestThemeRepositoriesDiffer is the measurement the whole port argues from.
//
// cohere shipped generated tables built from one repository and linted three with them. That is
// only a defect if two repositories on the same Tailwind actually resolve different themes, so this
// asserts it directly rather than leaving it as the premise of a design document. The two themes in
// theme_fixtures.json differ on purpose, the way two repositories do: the public theme declares keys the
// planted one never names (`--pane-*`, `--percentage-*`, `--color-x--2`), and the planted theme declares
// breakpoints of its own (`--breakpoint-tablet`, `--breakpoint-desk`), a numbered scale with a negative key
// (`--color-wash--1`), and an animation, radius and shadow set the public one lacks. If this test ever
// passes trivially because the two themes became identical, the assertion below fails rather than
// quietly succeeding.
func TestThemeRepositoriesDiffer(test *testing.T) {
	test.Parallel()
	fixture := loadThemeCorpus(test, "theme_fixtures.json")

	type resolved struct {
		name string
		keys map[string]string
		size int
	}
	var themes []resolved

	for _, aCase := range fixture.Cases {
		if aCase.Source != "repository" {
			continue
		}
		theme, _ := loadRepositoryTheme(test, aCase)
		keys := make(map[string]string, theme.Size())
		for _, entry := range theme.Entries() {
			keys[entry.Key] = entry.Value
		}
		themes = append(themes, resolved{name: aCase.Name, keys: keys, size: theme.Size()})
	}

	if len(themes) < 2 {
		test.Fatalf("need two repositories to compare, have %d", len(themes))
	}

	first, second := themes[0], themes[1]

	var onlyInFirst int
	var onlyInSecond int
	var differingValues int
	for key, value := range first.keys {
		otherValue, present := second.keys[key]
		switch {
		case !present:
			onlyInFirst++
		case otherValue != value:
			differingValues++
		}
	}
	for key := range second.keys {
		if _, present := first.keys[key]; !present {
			onlyInSecond++
		}
	}

	if onlyInFirst == 0 && onlyInSecond == 0 && differingValues == 0 {
		test.Errorf(
			"%s and %s resolved identical themes; the argument for this port is that they do not, "+
				"so either the corpus regressed to one repository or the loader is not reading the repository in front of it",
			first.name, second.name,
		)
	}

	test.Logf(
		"%s resolved %d entries, %s resolved %d: %d keys only in %s, %d only in %s, %d shared keys with different values",
		first.name, first.size, second.name, second.size,
		onlyInFirst, first.name, onlyInSecond, second.name, differingValues,
	)
}

// TestThemeLoaderReportsSkippedDirectives pins which directives the loader knows it is not acting
// on.
//
// `@config` can, upstream, contribute theme values through a JavaScript config that cohere never
// runs. That is a real gap in this component, and the honest handling is to report it: a
// repository that started declaring theme values in TypeScript would produce a theme quietly short
// by that many keys, and this is what makes that arrive as a visible directive rather than as a
// wrong count. See the boundary note in themeloader.go. testdata/directives_theme writes an `@config`
// and an `@plugin` in a file its entry point imports, so the loader must find each where it was written.
func TestThemeLoaderReportsSkippedDirectives(test *testing.T) {
	test.Parallel()
	entryPoint, absolutePathError := filepath.Abs(filepath.Join("testdata", "directives_theme", "theme.css"))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	theme, skipped, loadThemeFromFileError := LoadThemeFromFile(
		entryPoint,
		NodeStylesheetResolver(vendored.TailwindPackageRootForTest(test)),
	)
	if loadThemeFromFileError != nil {
		test.Fatalf("loading the directives theme: %v", loadThemeFromFileError)
	}

	var names []string
	for _, directive := range skipped {
		names = append(names, directive.Name+" "+directive.Parameters)
		if filepath.Base(directive.Path) != "settings.css" {
			test.Errorf("%s %s was reported in %s, and it is written in settings.css", directive.Name,
				directive.Parameters, directive.Path)
		}
	}
	if want := []string{"@config './tailwind.config.ts'", "@plugin './plugin.js'"}; !slices.Equal(names, want) {
		test.Errorf("skipped %q, want %q", names, want)
	}
	if value, found := theme.Get([]string{"--color-signal"}); !found || value != "#dc2626" {
		test.Errorf("--color-signal read %q, %v beside the skipped directives; the theme in that file must still load",
			value, found)
	}
}

// TestDesignSystemReportsSkippedDirectivesFromAnywhereInTheGraph is TestThemeLoaderReportsSkippedDirectives'
// question asked of LoadDesignSystem (#f598zk0), which is what production loads through, rather than of the
// test-only theme loader. testdata/directives_theme writes an
// `@config` and an `@plugin` in a file its entry point imports, so each must be found where it was
// written, and the theme beside them still loads.
func TestDesignSystemReportsSkippedDirectivesFromAnywhereInTheGraph(test *testing.T) {
	test.Parallel()
	entryPoint, absolutePathError := filepath.Abs(filepath.Join("testdata", "directives_theme", "theme.css"))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	system, loadDesignSystemError := LoadDesignSystem(
		LoadOptions{EntryPoint: entryPoint, TailwindPackageRoot: vendored.TailwindPackageRootForTest(test)},
	)
	if loadDesignSystemError != nil {
		test.Fatalf("loading the directives theme: %v", loadDesignSystemError)
	}

	var names []string
	for _, skipped := range system.SkippedDirectives {
		names = append(names, skipped.Name+" "+skipped.Parameters)
		if filepath.Base(skipped.Path) != "settings.css" {
			test.Errorf(
				"%s %s was reported in %s, and it is written in settings.css",
				skipped.Name,
				skipped.Parameters,
				skipped.Path,
			)
		}
	}
	if want := []string{"@config './tailwind.config.ts'", "@plugin './plugin.js'"}; !slices.Equal(names, want) {
		test.Errorf("skipped %q, want %q", names, want)
	}
	if value, found := system.Theme().Get([]string{"--color-signal"}); !found || value != "#dc2626" {
		test.Errorf(
			"--color-signal read %q, %v beside the skipped directives; the theme in that file must still load",
			value,
			found,
		)
	}
}

// TestThemeInsertionOrderSurvivesRedefinition pins the JavaScript `Map.set` behaviour that a Go
// port is most likely to get wrong, in isolation from the corpus.
//
// The corpus covers it, but only incidentally and only where a repository happens to redefine a
// framework key. Pinned here it names the rule: overwriting keeps position, deleting and re-adding
// does not. A port that appends on every write reorders every redefinition, which is what a
// repository's `@theme` does to nearly every framework default it overrides.
func TestThemeInsertionOrderSurvivesRedefinition(test *testing.T) {
	test.Parallel()
	theme := NewTheme()
	for _, entry := range [][2]string{{"--color-a", "red"}, {"--color-b", "blue"}, {"--color-c", "green"}} {
		if addError := theme.Add(entry[0], entry[1], ThemeOptionNone); addError != nil {
			test.Fatalf("Add(%q): %v", entry[0], addError)
		}
	}

	if addError := theme.Add("--color-a", "rebeccapurple", ThemeOptionNone); addError != nil {
		test.Fatalf("redefine: %v", addError)
	}
	assertThemeKeyOrder(test, theme, "overwrite keeps position", "--color-a", "--color-b", "--color-c")
	if value, _ := theme.Get([]string{"--color-a"}); value != "rebeccapurple" {
		test.Errorf("--color-a = %q after redefinition, want rebeccapurple", value)
	}

	// Deleting genuinely gives up the position, so re-adding lands at the end.
	if addError := theme.Add("--color-a", "initial", ThemeOptionNone); addError != nil {
		test.Fatalf("delete: %v", addError)
	}
	if addError := theme.Add("--color-a", "red", ThemeOptionNone); addError != nil {
		test.Fatalf("re-add: %v", addError)
	}
	assertThemeKeyOrder(test, theme, "delete then re-add moves to the end", "--color-b", "--color-c", "--color-a")
}

// TestThemeKeyOrderCompacts checks that the order slice does not grow without bound under repeated
// deletion, and that compaction preserves order.
//
// Order is tracked in a slice that deletion does not compact in place, so without the amortized
// reclaim in compactKeyOrder a stylesheet that cleared a namespace repeatedly would grow it
// forever. The bound is the property worth pinning; the exact threshold is an implementation
// choice and is not asserted.
func TestThemeKeyOrderCompacts(test *testing.T) {
	test.Parallel()
	theme := NewTheme()

	const rounds = 500
	for round := 0; round < rounds; round++ {
		key := fmt.Sprintf("--churn-%d", round)
		if addError := theme.Add(key, "x", ThemeOptionNone); addError != nil {
			test.Fatalf("Add: %v", addError)
		}
		if addError := theme.Add(key, "initial", ThemeOptionNone); addError != nil {
			test.Fatalf("delete: %v", addError)
		}
	}
	if addError := theme.Add("--survivor", "y", ThemeOptionNone); addError != nil {
		test.Fatalf("Add survivor: %v", addError)
	}

	if theme.Size() != 1 {
		test.Errorf("Size() = %d after %d add/delete rounds, want 1", theme.Size(), rounds)
	}
	assertThemeKeyOrder(test, theme, "only the survivor remains", "--survivor")

	// The slice is bounded by twice the live size plus the in-flight dead entries, never by the
	// number of writes. A port without compaction reaches `rounds` here.
	if len(theme.keyOrder) > 2*theme.Size()+2 {
		test.Errorf(
			"keyOrder holds %d slots for %d live keys after %d rounds; compaction is not reclaiming",
			len(theme.keyOrder),
			theme.Size(),
			rounds,
		)
	}
}

// TestThemeAddRejectsInvalidNamespaceClear pins that a clear directive with a value other than
// `initial` is an error rather than an entry.
//
// Upstream throws. A port that stored `--color-*: red` as a key named `--color-*` would answer
// every namespace read with one extra bogus member, and no corpus stylesheet contains the input
// that would reveal it.
func TestThemeAddRejectsInvalidNamespaceClear(test *testing.T) {
	test.Parallel()
	theme := NewTheme()
	if addError := theme.Add("--color-*", "red", ThemeOptionNone); addError == nil {
		test.Fatal("Add(\"--color-*\", \"red\") returned no error; Tailwind throws")
	}
	if theme.Size() != 0 {
		test.Errorf("Size() = %d after a rejected clear directive, want 0", theme.Size())
	}
}

// TestEscapeRoundTrip checks escapeCSSIdentifier and unescapeCSSIdentifier against the cases whose
// answers a reasonable guess gets wrong.
//
// These run on every `var()` the theme builds and on every key GetOptions is asked about, and the
// rules are not intuitive: a lone `-` is escaped, a leading digit becomes a hex escape with a
// trailing space, and a digit in second position is escaped only after a `-`.
func TestEscapeRoundTrip(test *testing.T) {
	test.Parallel()
	cases := []struct {
		value  string
		escape string
	}{
		{"--color-red-500", "--color-red-500"},
		{"-", "\\-"},
		{"--", "--"},
		{"--color-a/b", "--color-a\\/b"},
		{"--spacing-1.5", "--spacing-1\\.5"},
		{"0abc", "\\30 abc"},
		{"-0abc", "-\\30 abc"},
		{"--a b", "--a\\ b"},
		{"--emoji-\U0001F49C", "--emoji-\U0001F49C"},
		{"--aéb", "--aéb"},
	}

	for _, aCase := range cases {
		if got := escapeCssIdentifier(aCase.value); got != aCase.escape {
			test.Errorf("escapeCSSIdentifier(%q) = %q, want %q", aCase.value, got, aCase.escape)
		}
		if got := unescapeCssIdentifier(aCase.escape); got != aCase.value {
			test.Errorf("unescapeCSSIdentifier(%q) = %q, want %q", aCase.escape, got, aCase.value)
		}
	}

	// Unescape-only cases: inputs the escaper never produces but a stylesheet can contain.
	unescapeOnly := []struct{ escaped, want string }{
		{"--a\\62 c", "--abc"},
		{"--a\\000062c", "--abc"},
		{"--a\\0 b", "--a�b"},
		{"--a\\d800 b", "--a�b"},
		{"--a\\110000 b", "--a�b"},
		{"--a\\\\b", "--a\\b"},
		{"--a\\", "--a\\"},
		{"--a\\\U0001F49Cb", "--a\U0001F49Cb"},
	}
	for _, aCase := range unescapeOnly {
		if got := unescapeCssIdentifier(aCase.escaped); got != aCase.want {
			test.Errorf("unescapeCSSIdentifier(%q) = %q, want %q", aCase.escaped, got, aCase.want)
		}
	}

	test.Logf("%d escape round trips and %d unescape-only cases", len(cases), len(unescapeOnly))
}

// assertThemeKeyOrder compares the theme's live key order against an expected sequence.
func assertThemeKeyOrder(test *testing.T, theme *Theme, what string, want ...string) {
	test.Helper()

	var got []string
	for _, entry := range theme.Entries() {
		got = append(got, entry.Key)
	}
	if !equalStringSlices(got, want) {
		test.Errorf("%s: key order is %v, want %v", what, got, want)
	}
}

// assertOptionalString compares a (value, ok) pair against the fixture's nullable string.
//
// The distinction between "resolved to the empty string" and "did not resolve" is what this exists
// to preserve. A theme entry declared with an empty value resolves successfully to "", and a port
// that returned "" for a miss would agree with Tailwind on every hit and disagree on every miss
// while looking identical in a printout.
func assertOptionalString(test *testing.T, got string, ok bool, want *string, what string) {
	test.Helper()

	if want == nil {
		if ok {
			test.Errorf("%s = %q, Tailwind resolved nothing", what, got)
		}
		return
	}
	if !ok {
		test.Errorf("%s resolved nothing, Tailwind says %q", what, *want)
		return
	}
	if got != *want {
		test.Errorf("%s = %q, Tailwind says %q", what, got, *want)
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// summarizeStrings renders a slice for an error message, truncated so that a disagreement on a
// 558-key namespace reports its shape rather than scrolling the failure off the screen.
func summarizeStrings(values []string) string {
	const limit = 8
	if len(values) <= limit {
		return fmt.Sprintf("%d%v", len(values), values)
	}
	return fmt.Sprintf("%d[%s ...]", len(values), strings.Join(values[:limit], " "))
}

// describeCandidate renders a candidate value for an error message, keeping the null case
// distinguishable from the empty-string case.
func describeCandidate(value string, present bool) string {
	if !present {
		return "null"
	}
	return fmt.Sprintf("%q", value)
}
