package tailwind

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/corpus"
)

// The live table is verified against the engine, not against itself.
//
// A constructor that composes two halves can be self-consistent and wrong: the halves can agree with
// each other and disagree with Tailwind, which is the failure the whole descriptor slice is tested
// against rather than around. So the contract test builds a table with NewTable from a real design
// system and scores it against the engine's own readings in the fixture corpus, case for case, under
// the same rules descriptor_test.go uses for the measured table.
//
// Measured on both corpus repositories: 6,808 of 6,808 scored cases on ahra, and 6,806 of 6,806 on
// www-connected-app against that repository's own extraction. The second run is not automated here
// because it needs a fixture file generated against that repository, which is untracked for the same
// reason the table is. Reproduce it with:
//
//	node internal/lint/rules/tailwind/tools/generate_descriptor_table/fixtures.mjs \
//	    connected:app/_theme/styles/theme.css > /tmp/connected_fixtures.json
//
// The corpus repositories are read through internal/corpus. When one is unset the test skips naming
// its variable rather than passing, since a silent pass is what a broken lookup looks like, and when
// one is set but lacks its stylesheet or tailwindcss the test fails.

// corpusRepositories are the design systems the port is measured on.
//
// Both are named rather than one, because the two halves this file composes can only be told apart
// by a system whose theme differs, and a single repository would let a per-repository token pass as
// a framework fact.
var corpusRepositories = []struct {
	name     string
	spelling string
}{
	{name: "ahra", spelling: "ahra:app/_theme/styles/theme.css"},
	{name: "connected", spelling: "connected:app/_theme/styles/theme.css"},
}

// liveEntryPointFor resolves a corpus repository's stylesheet and the tailwindcss installed beside it: a
// skip naming the variable when the corpus is unset, and a failure when it is set but lacks either.
func liveEntryPointFor(test testing.TB, spelling string) (entryPoint, packageRoot string) {
	test.Helper()
	entryPoint = corpus.Resolve(test, spelling)
	packageRoot = findTailwindPackageRootForTest(filepath.Dir(entryPoint))
	if packageRoot == "" {
		test.Fatalf("%s holds no installed tailwindcss reachable from %s", spelling, entryPoint)
	}
	return entryPoint, packageRoot
}

// liveTableForOracle is liveTableFor for the design system an oracle records: a corpus repository with
// the tailwindcss installed beside it, as before, or a repository path such as the public theme's over
// the vendored tailwindcss.
func liveTableForOracle(test *testing.T, entryPoint string) (*LoadedDesignSystem, *Table) {
	test.Helper()
	_, _, spelled, spelledError := corpus.Spelled(entryPoint)
	if spelledError != nil {
		test.Fatalf("reading the corpus %s names: %v", entryPoint, spelledError)
	}
	if spelled {
		return liveTableFor(test, entryPoint)
	}
	system := oracleSystem(test, entryPoint)
	table := NewTable(system)
	if table == nil {
		test.Fatalf("NewTable returned nil for %s", entryPoint)
	}
	return system, table
}

// liveTableFor loads a repository's design system and builds its table, or skips.
func liveTableFor(test *testing.T, spelling string) (*LoadedDesignSystem, *Table) {
	test.Helper()
	entryPoint, packageRoot := liveEntryPointFor(test, spelling)
	system, loadDesignSystemError := LoadDesignSystem(
		LoadOptions{EntryPoint: entryPoint, TailwindPackageRoot: packageRoot},
	)
	if loadDesignSystemError != nil {
		test.Fatalf("loading the design system at %s: %v", entryPoint, loadDesignSystemError)
	}
	table := NewTable(system)
	if table == nil {
		test.Fatalf("NewTable returned nil for %s", entryPoint)
	}
	return system, table
}

// findTailwindPackageRootForTest walks up looking for an installed tailwindcss.
//
// A copy of the rules package's walk rather than an import, because internal/tailwind must not
// depend on internal/rules/tailwind; the dependency runs the other way.
func findTailwindPackageRootForTest(start string) string {
	directory := start
	for {
		candidate := filepath.Join(directory, "node_modules", "tailwindcss")
		if info, statError := os.Stat(filepath.Join(candidate, "index.css")); statError == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

// TestLiveTableAgreesWithTheEngineOverTheFixtureCorpus is the contract.
//
// The live table is scored against the engine's own readings, exactly as descriptor_test.go scores
// the measured table, over the same corpus and with the same rules: a case with no engine reading is
// reported and never counted as agreement, and a decline is only free on a per-declaration root.
//
// Scoring against the engine rather than against the measured table is deliberate. Two tables built
// from one extraction can agree with each other and disagree with Tailwind, and the whole point of
// this constructor is that its checked-in half was measured to be a fact about the framework. The
// engine is the only thing that can refuse it.
//
// Run on the public theme's oracle and on ahra's (#f598zk0), each against the design system it records.
func TestLiveTableAgreesWithTheEngineOverTheFixtureCorpus(test *testing.T) {
	test.Parallel()
	forEachOracleCopy(test, "descriptor_fixtures.json", func(test *testing.T, fixtureName string) {
		fixtures := loadDescriptorFixtures(test, fixtureName)
		_, live := liveTableForOracle(test, fixtures.EntryPoint)
		expectLiveTableAgreesWithTheEngine(test, fixtures, live)
	})
}

// expectLiveTableAgreesWithTheEngine scores one live table against the engine's readings in one oracle.
func expectLiveTableAgreesWithTheEngine(test *testing.T, fixtures *descriptorFixtures, live *Table) {
	test.Helper()

	if fixtures.TailwindVersion != live.TailwindVersion {
		test.Fatalf(
			"the fixtures are Tailwind %s and the live table is Tailwind %s",
			fixtures.TailwindVersion,
			live.TailwindVersion,
		)
	}

	agreed, mismatched, declined, withoutReading := 0, 0, 0, 0
	for _, one := range fixtures.Cases {
		candidate := one.parsed()
		got, answered := live.Lookup(candidate)
		if !answered {
			declined++
			// A decline is only free where the model states it cannot answer. Anywhere else it is
			// the failure that blocked #4q5dsn3: a rule going silent and scoring as agreement.
			if one.Reading != nil {
				descriptor, found := live.Descriptors[one.Root]
				if !found || !descriptor.PerDeclaration {
					mismatched++
					if mismatched <= 20 {
						test.Errorf("%s: the live table declined but the engine read %v#%d",
							one.ClassName, one.Reading.Order, one.Reading.Count)
					}
				}
			}
			continue
		}
		if one.Reading == nil {
			// No engine reading is neither an agreement nor a disagreement. See the same branch in
			// descriptor_test.go for why these exist and why they are counted rather than skipped.
			withoutReading++
			continue
		}
		want := Reading{Order: one.Reading.Order, Count: one.Reading.Count}
		if !got.Equal(want) {
			mismatched++
			if mismatched <= 20 {
				test.Errorf("%s (root %q, %s %q, modifier %s %q): the live table read %v#%d, the engine said %v#%d",
					one.ClassName, one.Root, one.ValueKind, one.Value, one.ModifierKind, one.ModifierValue,
					got.Order, got.Count, want.Order, want.Count)
			}
			continue
		}
		agreed++
	}

	if mismatched > 0 {
		test.Errorf("%d of %d cases disagreed with the engine", mismatched, len(fixtures.Cases))
	}
	scored := agreed + mismatched
	test.Logf("the live table agreed on %d of %d scored cases over %d roots", agreed, scored, fixtures.Counts.Roots)
	test.Logf("  %d cases had no engine reading and were reported rather than scored", withoutReading)
	test.Logf("  %d lookups declined", declined)

	// Volume assertions, in the extractor's spirit: a suite that agreed on everything because it
	// tested almost nothing reports the same green line as one that did the work.
	if withoutReading > len(fixtures.Cases)/2 {
		test.Errorf("%d of %d cases had no reading; the corpus is mostly unscored", withoutReading, len(fixtures.Cases))
	}
	if agreed < 5000 {
		test.Errorf("only %d agreements; the corpus is too small for the number to mean anything", agreed)
	}
}

// TestLiveTableCarriesTheRepositoryTheme asserts the per-repository half is actually present.
//
// This is the test the whole task turns on. A table that silently described bare Tailwind while
// claiming to describe this repository would answer most classes correctly and be wrong on exactly
// the tokens the repository added, which is the failure the port exists to remove and the one no
// aggregate agreement number would surface.
func TestLiveTableCarriesTheRepositoryTheme(test *testing.T) {
	test.Parallel()
	for _, repository := range corpusRepositories {
		test.Run(repository.name, func(test *testing.T) {
			test.Parallel()
			system, table := liveTableFor(test, repository.spelling)
			checkTableCarriesTheTheme(test, repository.name, system, table)
		})
	}
}

// checkTableCarriesTheTheme is the composition TestLiveTableCarriesTheRepositoryTheme asserts, shared with
// its public twin: namespaces longest-first and drawn from the theme, and every `@utility` block composed
// into the table, functional ones marked PerDeclaration.
func checkTableCarriesTheTheme(test *testing.T, name string, system *LoadedDesignSystem, table *Table) {
	test.Helper()

	if len(table.Namespaces) == 0 {
		test.Fatal("the table carries no namespaces, so every bare value resolves through inference alone")
	}
	// Longest-first is the engine's precedence and a contract of the field.
	for index := 1; index < len(table.Namespaces); index++ {
		if len(table.Namespaces[index-1]) < len(table.Namespaces[index]) {
			test.Fatalf("namespaces are not longest-first at %d: %q before %q",
				index, table.Namespaces[index-1], table.Namespaces[index])
		}
	}

	// Every namespace the table names must actually hold keys from this theme, and every
	// key must be one the theme reports. Otherwise the table's namespaces came from
	// somewhere other than the repository.
	for _, namespace := range table.Namespaces {
		keys := table.KeysByNamespace[namespace]
		if len(keys) == 0 {
			test.Fatalf("namespace %q holds no keys", namespace)
		}
		fromTheme := map[string]bool{}
		for _, key := range system.Theme().KeysInNamespaces([]string{namespace}) {
			fromTheme[key] = true
		}
		for key := range keys {
			if !fromTheme[key] {
				test.Fatalf("namespace %q holds key %q, which this repository's theme does not", namespace, key)
			}
		}
	}

	// The repository's own contributions, by count rather than by name, so the assertion
	// holds on a repository this test has never seen.
	staticRoots, functionalRoots := 0, 0
	// Each kind separately rather than a switch, because a root can be declared both ways:
	// `@utility fade-in` and `@utility fade-in-*` are two blocks naming one root, and
	// sixteen roots in this repository have that shape. A switch counted each such root once
	// and checked only whichever kind happened to win.
	for root, kinds := range system.utilityRoots {
		if kinds[UtilityKindStatic] {
			if _, found := table.Statics[root]; !found {
				test.Errorf("static `@utility %s` is missing from the table's statics", root)
			}
			staticRoots++
		}
		if kinds[UtilityKindFunctional] {
			descriptor, found := table.Descriptors[root]
			if !found {
				test.Errorf("functional `@utility %s` is missing from the table's descriptors", root)
				continue
			}
			if !descriptor.PerDeclaration {
				test.Errorf(
					"functional `@utility %s` is not marked PerDeclaration, so the table would answer it from a row rather than declining to the evaluator",
					root,
				)
			}
			functionalRoots++
		}
	}
	if staticRoots+functionalRoots == 0 {
		test.Fatal("this repository declares no `@utility` blocks, so this test proved nothing about composition")
	}
	test.Logf("%s: %d namespaces, %d static and %d functional `@utility` roots composed in",
		name, len(table.Namespaces), staticRoots, functionalRoots)
}

// TestLiveTableBaseHalfCarriesNoRepositoryTokens is the other side of the same claim.
//
// The checked-in half must be a fact about Tailwind. Every namespace it keys a reading on has to be
// one of the framework's, or the file is carrying one repository's tokens under a name that says
// otherwise, which is the exact bug descriptor.go's comment on Table refuses.
func TestLiveTableBaseHalfCarriesNoRepositoryTokens(test *testing.T) {
	test.Parallel()
	checked := 0
	for root, descriptor := range baseDescriptors {
		for _, axis := range []AxisReadings{descriptor.Absent, descriptor.Alpha, descriptor.Themed} {
			for namespace := range axis.ByNamespace {
				if namespace == namespaceColorKeyword || namespace == namespaceNone {
					continue
				}
				if !FrameworkNamespaces[namespace] {
					test.Errorf(
						"%s keys a reading on namespace %q, which is not one of the framework's",
						root,
						namespace,
					)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		test.Fatal("no namespace buckets were checked, so this test proved nothing")
	}
	test.Logf(
		"%d namespace buckets in the base table, all keyed on the %d framework namespaces",
		checked,
		len(FrameworkNamespaces),
	)
}

// TestLiveTableDeclinesOnlyWhereTheEvaluatorAnswers is the price of declining, measured.
//
// The live table declines on every repository `@utility` root, where the measured table carries a
// row for the ones that are not per-declaration. That is a deliberate loss and this test is what
// stops it being a silent one: every case the live table declines and the measured table answers
// must be recoverable from the `@utility` evaluator, or have no engine reading at all.
//
// Without this, "the evaluator picks it up" is a claim in a comment. A comment cannot fail.
//
// Run on the public theme's oracle and on ahra's (#f598zk0): each copy's fixtures, its measured table and
// its live table come from one design system.
func TestLiveTableDeclinesOnlyWhereTheEvaluatorAnswers(test *testing.T) {
	test.Parallel()
	forEachOracleCopy(test, "descriptor_fixtures.json", func(test *testing.T, fixtureName string) {
		fixtures := loadDescriptorFixtures(test, fixtureName)
		measured := testTable(test)
		if tableName := oracleSibling(
			test,
			fixtureName,
			"descriptor_table.json",
		); tableName != "descriptor_table.json" {
			var loadTestTableError error
			measured, loadTestTableError = loadTestTable(
				tableName,
				oracleSibling(test, fixtureName, "descriptor_context.json"),
			)
			if loadTestTableError != nil {
				test.Fatalf("building the measured table from %s: %v", tableName, loadTestTableError)
			}
		}
		system, live := liveTableForOracle(test, fixtures.EntryPoint)
		expectLiveTableDeclinesOnlyWhereTheEvaluatorAnswers(test, fixtures, measured, system, live)
	})
}

// expectLiveTableDeclinesOnlyWhereTheEvaluatorAnswers checks every case one live table declines and its
// measured table answers against the evaluator.
func expectLiveTableDeclinesOnlyWhereTheEvaluatorAnswers(
	test *testing.T,
	fixtures *descriptorFixtures,
	measured *Table,
	system *LoadedDesignSystem,
	live *Table,
) {
	test.Helper()

	evaluator := system.Utilities()
	if evaluator == nil {
		test.Fatal("this repository declares no `@utility` blocks, so this test proved nothing")
	}

	moved, recovered, unscored := 0, 0, 0
	for _, one := range fixtures.Cases {
		candidate := one.parsed()
		if _, liveAnswered := live.Lookup(candidate); liveAnswered {
			continue
		}
		if _, measuredAnswered := measured.Lookup(candidate); !measuredAnswered {
			continue
		}
		moved++
		if one.Reading == nil {
			// The measured table answered a case the engine gave no reading for, so nothing is lost
			// by declining it: it was never scorable.
			unscored++
			continue
		}
		reading, answered := evaluator.Reading(candidate)
		if !answered {
			test.Errorf(
				"%s: the live table declined, the measured table answered, and the evaluator cannot answer it either",
				one.ClassName,
			)
			continue
		}
		want := Reading{Order: one.Reading.Order, Count: one.Reading.Count}
		if !reading.Equal(want) {
			test.Errorf("%s: the evaluator read %v#%d where the engine said %v#%d",
				one.ClassName, reading.Order, reading.Count, want.Order, want.Count)
			continue
		}
		recovered++
	}

	if moved == 0 {
		test.Fatal("no cases moved from answered to declined, so this test proved nothing")
	}
	test.Logf(
		"%d cases the live table declines and the measured table answers: %d recovered exactly by the evaluator, %d had no engine reading",
		moved,
		recovered,
		unscored,
	)
}

// TestLiveTableIsNilWithoutADesignSystem pins the failing-safe behaviour.
//
// A bare framework table would answer every class confidently and be wrong on every repository
// token. Nil forces the caller to decline instead.
func TestLiveTableIsNilWithoutADesignSystem(test *testing.T) {
	test.Parallel()
	if table := NewTable(nil); table != nil {
		test.Fatalf(
			"NewTable(nil) returned a table; a caller with no design system must decline rather than read a framework-only one",
		)
	}
}

// BenchmarkNewTable is what the table costs on top of the design system it is built from.
//
// The relevant comparison is `518fec5`'s 78.758µs for the design system build itself. This runs on
// the same counted path, once per program, so the question a reader has is whether adding it changed
// the order of magnitude of a per-run cost, not whether it is fast in isolation.
func BenchmarkNewTable(benchmark *testing.B) {
	entryPoint, packageRoot := liveEntryPointFor(benchmark, corpusRepositories[0].spelling)
	system, loadDesignSystemError := LoadDesignSystem(
		LoadOptions{EntryPoint: entryPoint, TailwindPackageRoot: packageRoot},
	)
	if loadDesignSystemError != nil {
		benchmark.Fatalf("loading the design system: %v", loadDesignSystemError)
	}

	benchmark.ReportAllocs()
	benchmark.ResetTimer()
	for range benchmark.N {
		if table := NewTable(system); table == nil {
			benchmark.Fatal("NewTable returned nil")
		}
	}
}

// BenchmarkLoadDesignSystemWithTable is the whole per-run cost the rules actually pay.
//
// Load plus table, which is what `loadDesignSystemForProgram` does once per program.
func BenchmarkLoadDesignSystemWithTable(benchmark *testing.B) {
	entryPoint, packageRoot := liveEntryPointFor(benchmark, corpusRepositories[0].spelling)

	benchmark.ReportAllocs()
	benchmark.ResetTimer()
	for range benchmark.N {
		system, loadDesignSystemError := LoadDesignSystem(
			LoadOptions{EntryPoint: entryPoint, TailwindPackageRoot: packageRoot},
		)
		if loadDesignSystemError != nil {
			benchmark.Fatalf("loading the design system: %v", loadDesignSystemError)
		}
		if table := NewTable(system); table == nil {
			benchmark.Fatal("NewTable returned nil")
		}
	}
}
