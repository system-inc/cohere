package tailwind

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	tailwindcollapse "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
	"github.com/system-inc/cohere/internal/lint/rules/tailwind/vendored"
	"github.com/system-inc/cohere/internal/testing/repository"
)

// The corpus placement measurement for the rules moved onto the live design system.
//
// # Why a placement count and not only a findings count
//
// This is the acceptance criterion #3r6cxrb was written around, and it exists because of the way the
// previous attempt failed. A migration is scored by comparing the rule's answers before and after,
// and a rule that stops having answers scores perfectly on every comparison that only counts
// disagreements: silence reads as agreement. `#4q5dsn3` was blocked over exactly that, on a
// regression check that named a number without naming the population it had to be measured over.
//
// So every measurement here reports how many classes the rule actually placed (resolved to a root,
// or to a property set, or to an existence answer) beside whatever else it reports. A rule that went
// quiet fails on the denominator rather than passing on the rate.
//
// # The corpus
//
// The repository cases of `internal/lint/rules/tailwind/collapse/testdata/variant_fixtures.json`, the
// same lists `class_order_live_test.go` measures: two repositories written in cohere, `planted` and
// `public`, each a theme under collapse/testdata and 1,200 class lists tools/generate_planted_repository
// drew from it (#pg78xtp). They replaced the lists harvested from two consumer repositories when cohere's
// tests stopped reading other repositories (#5wj23dd). Each list is read as written, before Tailwind's sort
// drops the classes it cannot rank, because the dead classes the generator planted are what the
// no-unknown-classes measurement is measured on. Every measurement runs per repository, since a class's
// answer is a function of the theme it was written against and one repository's half can collapse while
// the other's grows.
//
// Every helper here is prefixed `livePlacement` for the reason design_system_test.go states: several
// agents work in this package at once, and a helper whose name does not say which component owns it
// is a collision waiting to happen.

// livePlacementSourceTrees are the generated source trees, by repository case name, each holding the
// planted.json its generator wrote beside the `.tsx` files.
var livePlacementSourceTrees = map[string]string{
	"planted": filepath.Join("collapse", "testdata", "planted_repository", "source"),
	"public":  filepath.Join("collapse", "testdata", "public_repository", "source"),
}

// livePlacementCorpus is the slice of variant_fixtures.json this file reads.
type livePlacementCorpus struct {
	Cases []struct {
		Name       string `json:"name"`
		Source     string `json:"source"`
		EntryPath  string `json:"entryPath"`
		ClassLists []struct {
			Classes []string `json:"classes"`
		} `json:"classLists"`
	} `json:"cases"`
}

// livePlacementManifest is what tools/generate_planted_repository records about a tree it wrote.
type livePlacementManifest struct {
	EntryPoint  string   `json:"entryPoint"`
	Lists       int      `json:"lists"`
	Classes     int      `json:"classes"`
	DeadClasses []string `json:"deadClasses"`
	// Collapses are the pairs Tailwind's canonicalizer merges, each with the class it becomes.
	Collapses []struct {
		Inputs []string `json:"inputs"`
		Output string   `json:"output"`
	} `json:"collapses"`
}

// livePlacementRepository is one repository's class lists, as written, with what its generator recorded.
type livePlacementRepository struct {
	name      string
	entryPath string
	lists     [][]string
	manifest  livePlacementManifest
}

// livePlacementRepositories returns the corpus's class lists, one repository at a time, in name order.
//
// Grouped rather than flattened because a class's answer is a function of the repository it was
// written in, which is the whole premise of the migration. Measuring one repository's classes against
// the other's design system would be measuring the defect rather than the fix.
func livePlacementRepositories(test *testing.T) []livePlacementRepository {
	test.Helper()

	path := filepath.Join("collapse", "testdata", "variant_fixtures.json")
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatalf("read %s: %v", path, readError)
	}
	var fixture livePlacementCorpus
	if decodeError := json.Unmarshal(contents, &fixture); decodeError != nil {
		test.Fatalf("parse %s: %v", path, decodeError)
	}

	var repositories []livePlacementRepository
	for _, aCase := range fixture.Cases {
		if aCase.Source != "repository" || len(aCase.ClassLists) == 0 {
			continue
		}
		name := strings.TrimSuffix(aCase.Name, "/class-lists")
		sourceTree, isGenerated := livePlacementSourceTrees[name]
		if !isGenerated {
			test.Fatalf("%s is a repository case with no generated source tree beside it", name)
		}
		manifestPath := filepath.Join(sourceTree, "planted.json")
		manifestContents, manifestReadError := os.ReadFile(manifestPath)
		if manifestReadError != nil {
			test.Fatalf("read %s: %v", manifestPath, manifestReadError)
		}
		corpusRepository := livePlacementRepository{name: name, entryPath: aCase.EntryPath}
		if decodeError := json.Unmarshal(manifestContents, &corpusRepository.manifest); decodeError != nil {
			test.Fatalf("parse %s: %v", manifestPath, decodeError)
		}
		if corpusRepository.manifest.EntryPoint != aCase.EntryPath {
			test.Fatalf("%s was sorted against %s and its tree was drawn from %s",
				name, aCase.EntryPath, corpusRepository.manifest.EntryPoint)
		}
		for _, classList := range aCase.ClassLists {
			corpusRepository.lists = append(corpusRepository.lists, classList.Classes)
		}
		repositories = append(repositories, corpusRepository)
	}
	if len(repositories) != len(livePlacementSourceTrees) {
		test.Fatalf("the corpus holds %d repositories and the generated corpus is %d, so a measurement below "+
			"would be missing a half", len(repositories), len(livePlacementSourceTrees))
	}
	sort.Slice(repositories, func(left int, right int) bool {
		return repositories[left].name < repositories[right].name
	})
	return repositories
}

// livePlacementSystem builds the design system a corpus entry point names, over the vendored tailwindcss.
//
// The fixture records the entry point relative to this repository's root, so it loads the same on every
// machine, and a stylesheet that will not load fails the test rather than leaving a repository unmeasured.
func livePlacementSystem(test *testing.T, entryPath string) DesignSystemResult {
	test.Helper()

	entryPoint := repository.Path(test, entryPath)
	system, loadDesignSystemError := tailwindcollapse.LoadDesignSystem(tailwindcollapse.LoadOptions{
		EntryPoint:          entryPoint,
		TailwindPackageRoot: vendored.TailwindPackageRootForTest(test),
	})
	if loadDesignSystemError != nil {
		test.Fatalf("loading %s: %v", entryPath, loadDesignSystemError)
	}
	return loadedDesignSystemResult(system, entryPoint)
}

// forEachLivePlacementRepository runs a measurement once per corpus repository, each as its own subtest.
func forEachLivePlacementRepository(
	test *testing.T,
	measure func(test *testing.T, corpusRepository livePlacementRepository, designSystem DesignSystemResult),
) {
	test.Helper()
	for _, corpusRepository := range livePlacementRepositories(test) {
		test.Run(corpusRepository.name, func(test *testing.T) {
			test.Parallel()
			measure(test, corpusRepository, livePlacementSystem(test, corpusRepository.entryPath))
		})
	}
}

// TestUnknownClassesReportsExactlyThePlantedDeadClasses is `no-unknown-classes`'s population measurement.
//
// The rule's output is a finding per class it cannot place, so its population is every class it was
// asked about and its finding count is the complement. Both are asserted, because either alone is
// satisfiable by a broken rule: a rule that answers "known" for everything reports zero findings, and
// a rule that answers "unknown" for everything reports the whole corpus.
//
// Over the consumer corpus this was zero findings of about 11,300 classes, since those repositories wrote
// only classes their themes define. The generated corpus has dead classes on purpose, as real code does:
// a value no `@theme` key answers on a few of each theme's own functional roots (`wash--4`, `edge--7`), each
// written once bare and once under a variant, confirmed dead by Tailwind's `candidatesToCss` when they were
// planted. planted.json names them, six on the planted theme and eight on the public one, and the rule must
// report exactly those: every other class in the corpus compiles, so a further finding is a false positive
// and a missing one is a dead class read as live.
func TestUnknownClassesReportsExactlyThePlantedDeadClasses(test *testing.T) {
	test.Parallel()
	forEachLivePlacementRepository(test, func(
		test *testing.T,
		corpusRepository livePlacementRepository,
		designSystem DesignSystemResult,
	) {
		asked := 0
		var unknown []string
		for _, classes := range corpusRepository.lists {
			for _, className := range classes {
				asked++
				if !classExistsIn(className, designSystem.System) {
					unknown = append(unknown, className)
				}
			}
		}
		test.Logf("no-unknown-classes on %s: asked about %d classes, reported %d as unknown: %v",
			corpusRepository.name, asked, len(unknown), unknown)

		// The population guard, first, because every assertion below it is satisfiable by a rule that was
		// never asked anything. The corpus is generated, so the population is known exactly.
		if asked != corpusRepository.manifest.Classes {
			test.Fatalf("the rule was asked about %d classes and %s holds %d; a population that moved means the "+
				"corpus stopped loading, and the finding count below would be measuring a different run",
				asked, corpusRepository.name, corpusRepository.manifest.Classes)
		}

		sort.Strings(unknown)
		planted := slices.Clone(corpusRepository.manifest.DeadClasses)
		sort.Strings(planted)
		if !slices.Equal(unknown, planted) {
			test.Errorf("the rule reported %v as unknown and the generator planted %v. Every other class here "+
				"compiles on the theme it was drawn from, so the two must be the same classes", unknown, planted)
		}
	})
}

// TestCanonicalClassesPlacementIsAccountedFor is `enforce-canonical-classes`'s measurement, and it
// is the one that had to explain a drop rather than only report one.
//
// When the split moved onto the live design system the consumer corpus measured placed 9,428 -> 7,287
// with the same two findings, and the drop was decomposed rather than asserted away: every dropped class
// was one whose first Tailwind reading is static, and a static has no root-plus-value structure for a
// collapse family to relate (`flex`, `block`, `border-dashed`). The shipped prefix walk had placed them by
// forcing a functional reading Tailwind does not take, `border-dashed` as root `border` with value
// `dashed`, and those placements never became findings.
//
// That decomposition is what this asserts over the generated corpus: every class the split declines is
// one Tailwind reads as static or arbitrary. The split is syntactic, so the planted dead classes split
// like any functional class and are counted placed. The placed floors sit just under what each repository
// places now, so losing a category of class fails.
//
// The findings are the collapses the generator planted, four per repository, each a pair Tailwind's
// canonicalizer merges (`w-px h-px` to `size-px`) in a list where it merges nothing else, so the rule must
// report exactly those, each as the class Tailwind writes: a rule that stopped merging fails, and so does
// one that merges what Tailwind leaves alone.
func TestCanonicalClassesPlacementIsAccountedFor(test *testing.T) {
	test.Parallel()
	// 4,625 of planted's 6,021 classes and 5,078 of public's 6,104 split now.
	placedFloors := map[string]int{"planted": 4550, "public": 5000}
	forEachLivePlacementRepository(test, func(
		test *testing.T,
		corpusRepository livePlacementRepository,
		designSystem DesignSystemResult,
	) {
		placed, asked := 0, 0
		droppedStatics, droppedOther := 0, 0
		droppedExamples := map[string]string{}
		var reported []string
		// The rule's default options, and its canonicalizer signing under them, as the rule builds both.
		collapse := canonicalCollapseOptions{enabled: true, logical: true}
		canonicalizer := designSystem.Canonicalizer.WithFeatures(
			tailwindcollapse.SignatureFeaturesFor(collapse.enabled, collapse.logical),
		)
		for _, classes := range corpusRepository.lists {
			for _, className := range classes {
				asked++
				if _, canSplit := splitCandidateIn(className, designSystem.System); canSplit {
					placed++
					continue
				}

				// Every class the live split declines must be one Tailwind reads as something other
				// than a functional utility, or the decline is a lost answer rather than a correct
				// one.
				parsed := tailwindcollapse.ParseCandidate(className, designSystem.System)
				switch {
				case len(parsed) == 0:
					droppedOther++
					droppedExamples[className] = "no reading at all"
				case parsed[0].Kind == tailwindcollapse.ParsedCandidateKindFunctional:
					// A functional class the split declined is a lost answer, since the split's only
					// other exit is a design system it could not read.
					droppedOther++
					droppedExamples[className] = "reads functionally but was not split"
				default:
					// Static or arbitrary. Neither has the root-plus-value structure a collapse
					// family relates, and `[mask-type:luminance]` in particular is a whole declaration
					// the author wrote out, with no root at all.
					droppedStatics++
				}
			}

			// The rule's own pass, so this counts findings rather than merge opportunities.
			collapses, _ := collapsesIn(classes, nil, nil, designSystem, canonicalizer, collapse)
			for _, found := range collapses {
				inputs := slices.Clone(found.inputs)
				sort.Strings(inputs)
				reported = append(reported, strings.Join(inputs, " ")+" => "+found.output)
			}
		}
		// What the rule should report: every planted collapse, as Tailwind writes it. `w-rail h-rail` is
		// `size-14`, not `size-rail`, since Tailwind rewrites a theme key to the bare multiplier it equals.
		var planted []string
		for _, collapse := range corpusRepository.manifest.Collapses {
			inputs := slices.Clone(collapse.Inputs)
			sort.Strings(inputs)
			planted = append(planted, strings.Join(inputs, " ")+" => "+collapse.Output)
		}
		sort.Strings(reported)
		sort.Strings(planted)

		test.Logf("enforce-canonical-classes on %s: asked about %d classes, placed %d, reported %d findings: %v",
			corpusRepository.name, asked, placed, len(reported), reported)
		test.Logf("  declined: %d that read as static or arbitrary, %d other", droppedStatics, droppedOther)

		// The population guard. A rule that placed nothing reports nothing and would otherwise satisfy
		// the finding assertion below.
		if placed < placedFloors[corpusRepository.name] {
			test.Fatalf("the rule placed %d classes on %s, under the floor of %d; a population that dropped this "+
				"far means a whole category of class stopped resolving to a root, and the finding count would be "+
				"measuring that silence", placed, corpusRepository.name, placedFloors[corpusRepository.name])
		}

		// The decomposition, and it is the assertion that makes the declines reviewable rather than merely
		// reported. A class the live split declines for any reason other than "Tailwind does not read this
		// as a functional utility" is an answer the migration lost.
		if droppedOther != 0 {
			names := make([]string, 0, len(droppedExamples))
			for className := range droppedExamples {
				names = append(names, className+" ("+droppedExamples[className]+")")
			}
			sort.Strings(names)
			if len(names) > 12 {
				names = names[:12]
			}
			test.Errorf(
				"%d classes were declined despite reading as functional utilities, so the declines are not "+
					"fully explained by classes that have no root-plus-value structure: %s",
				droppedOther, strings.Join(names, ", "),
			)
		}

		// The findings are what the rule actually says, and they are the planted collapses and nothing else.
		if !slices.Equal(reported, planted) {
			test.Errorf("the rule reported %v on %s and Tailwind's canonicalizer merges exactly %v there",
				reported, corpusRepository.name, planted)
		}
	})
}

// TestConflictingClassesPlacementIsAccountedFor is `no-conflicting-classes`'s measurement.
//
// Seven tables moved, the largest change of the three, so the population is the thing to watch. The
// rule resolves a class to a property set, a selector shape, a variant string and a composition flag,
// and a class it cannot resolve is skipped rather than reported, which is precisely how a migration
// of this rule could go silent without a single test failing.
//
// The generator writes no two classes in a list that set the same property under the same variants,
// read from the CSS Tailwind compiles for each, the way a list someone wrote does not. So the findings
// are counted exactly beside the population, for the reason the file comment gives: on its own a finding
// count is satisfiable by a rule that resolved nothing. The resolved floors sit just under what each
// repository resolves now, and the classes left unresolved are logged: the planted dead classes, which
// set nothing, and classes that set only custom properties, which no conflict can be about.
func TestConflictingClassesPlacementIsAccountedFor(test *testing.T) {
	test.Parallel()
	expected := map[string]struct{ resolvedFloor, findings int }{
		"planted": {resolvedFloor: 5100, findings: 0},
		"public":  {resolvedFloor: 4950, findings: 0},
	}
	forEachLivePlacementRepository(test, func(
		test *testing.T,
		corpusRepository livePlacementRepository,
		designSystem DesignSystemResult,
	) {
		asked, resolved, findings := 0, 0, 0
		unresolvedExamples := map[string]int{}
		var findingExamples []string
		for _, classes := range corpusRepository.lists {
			for _, className := range classes {
				asked++
				if _, canResolve := resolveClassFactsIn(className, designSystem); canResolve {
					resolved++
					continue
				}
				unresolvedExamples[className]++
			}
			found := conflictFindingsIn(classes, designSystem)
			findings += len(found)
			if len(found) > 0 && len(findingExamples) < 12 {
				findingExamples = append(findingExamples, strings.Join(classes, " "))
			}
		}

		test.Logf("no-conflicting-classes on %s: asked about %d classes, resolved %d, reported %d findings",
			corpusRepository.name, asked, resolved, findings)
		for _, example := range livePlacementTopKeys(unresolvedExamples, 20) {
			test.Logf("  unresolved %4d  %s", unresolvedExamples[example], example)
		}
		for _, example := range findingExamples {
			test.Logf("  finding in  %s", example)
		}

		// The population guard, and this rule is the one it matters most for: seven tables moved at once,
		// and a rule that resolved nothing would report nothing and look exactly like a clean corpus.
		want := expected[corpusRepository.name]
		if resolved < want.resolvedFloor {
			test.Fatalf("the rule resolved %d classes on %s to a property set, under the floor of %d; a "+
				"population that dropped this far means a whole category of class stopped resolving, and the "+
				"finding count below would be measuring its silence",
				resolved, corpusRepository.name, want.resolvedFloor)
		}
		if findings != want.findings {
			test.Errorf("the rule reported %d findings on %s and reported %d when this was measured",
				findings, corpusRepository.name, want.findings)
		}
	})
}

// livePlacementTopKeys returns the most frequent keys of a count map, for readable logs.
func livePlacementTopKeys(counts map[string]int, limit int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left int, right int) bool {
		if counts[keys[left]] != counts[keys[right]] {
			return counts[keys[left]] > counts[keys[right]]
		}
		return keys[left] < keys[right]
	})
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return keys
}

// The resolution comparison that justified the deletion, and why it is not here.
//
// `TestConflictingClassesResolutionsDidNotChange` compared the computed answer against the four
// deleted tables, class for class, over the consumer corpus: 11,260 class occurrences answered by both,
// 11,182 identical, 11 different. All 11 were `text-[<size>]` with no modifier, where the tables
// said `{font-size, line-height}` and the computed answer says `font-size` alone. Upstream at v4.3.3
// returns `[decl('font-size', value)]` outside `if (candidate.modifier)`, so the computed side is
// right and the correction makes the rule stricter rather than quieter.
//
// It is deleted along with the tables it read, because a comparison with one side missing is not a
// comparison. What survives is the per-repository placement above, which catches the rule going
// quiet, and `TestComputedPropertiesMakeTheDistinctionsTheOverrideTablesHeld` in Tailwind package,
// which pins each distinction the deleted tables encoded, including this `text` one in both
// directions.

// TestRepositoryColorTokensReadAsColors: a repository's own colour tokens are recognised as colours,
// which the deleted table could not do.
//
// `valueIsColorIn` read `ColorNames`, 561 names generated from one consumer, and a second consumer
// declared three the table did not carry. A class using one of them read as not-a-colour, so `bg-brand`
// was answered on the wrong arm: `background-image` rather than `background-color`, which is a wrong
// property set rather than a missing answer, and it fails to conflict with `bg-red-500` when it should.
//
// Asserted against each corpus theme rather than a list, so a theme adding a colour joins this test
// rather than needing to be added to it. Each theme's own keys are named, because they are the ones no
// framework table could vouch for: the planted theme's palette and numbered `wash-*` scale, the public
// theme's `brand`, `tone-*` and `ink-*`. A key holding a double dash (`signal--2`, `wash--1`, `x--2`) is
// left out of the namespace's keys, as upstream's `keysInNamespaces` leaves it out, so none is named.
func TestRepositoryColorTokensReadAsColors(test *testing.T) {
	test.Parallel()
	ownKeys := map[string][]string{
		"planted": {"moss", "moss-deep", "clay", "slate-ink", "wash-0", "wash-1", "wash-8"},
		"public":  {"brand", "tone-1", "tone-4", "surface-2", "ink-strong", "ink-soft"},
	}
	forEachLivePlacementRepository(test, func(
		test *testing.T,
		corpusRepository livePlacementRepository,
		designSystem DesignSystemResult,
	) {
		keys := designSystem.System.Theme().KeysInNamespaces([]string{"--color"})
		for _, key := range keys {
			if !valueIsColorIn("bg-"+key, "bg", designSystem.System) {
				test.Errorf("%s declares --color-%s and `bg-%s` does not read as a colour",
					corpusRepository.name, key, key)
			}
		}
		for _, key := range ownKeys[corpusRepository.name] {
			if !slices.Contains(keys, key) {
				test.Errorf("%s declares --color-%s and its theme does not list it, so this checked only the palette",
					corpusRepository.name, key)
			}
		}
		test.Logf("%s: %d colour tokens read as colours", corpusRepository.name, len(keys))
		if len(keys) < 200 {
			test.Fatalf("%s lists %d colour tokens, too few to hold the framework palette",
				corpusRepository.name, len(keys))
		}
	})
}
