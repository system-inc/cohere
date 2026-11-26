package tailwind

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/typescript_language/rules/tailwind/vendored"
)

// The live table is verified against Tailwind, not against itself.
//
// A constructor that composes two halves can be self-consistent and wrong: the halves can agree with
// each other and disagree with Tailwind, which is the failure the whole descriptor slice is tested
// against rather than around. So the contract test builds a table with NewTable from a real design
// system and scores it against Tailwind's own readings in the fixture corpus, case for case, under
// the same rules descriptor_test.go uses for the measured table.
//
// Measured on two consumer repositories when it was written: 6,808 of 6,808 scored cases on one and
// 6,806 of 6,806 on the other. It now runs on the public theme's oracle, which every machine has
// (#f598zk0, #5wj23dd).

// liveTableForOracle is the design system an oracle records, by the entry point its fixture names, with
// the table NewTable builds from it.
func liveTableForOracle(test *testing.T, entryPoint string) (*LoadedDesignSystem, *Table) {
	test.Helper()
	system := oracleSystem(test, entryPoint)
	table := NewTable(system)
	if table == nil {
		test.Fatalf("NewTable returned nil for %s", entryPoint)
	}
	return system, table
}

// TestLiveTableAgreesWithTailwindOverTheFixtureCorpus is the contract.
//
// The live table is scored against Tailwind's own readings, exactly as descriptor_test.go scores
// the measured table, over the same corpus and with the same rules: a case with no Tailwind reading is
// reported and never counted as agreement, and a decline is only free on a per-declaration root.
//
// Scoring against Tailwind rather than against the measured table is deliberate. Two tables built
// from one extraction can agree with each other and disagree with Tailwind, and the whole point of
// this constructor is that its checked-in half was measured to be a fact about the framework.
// Tailwind is the only thing that can refuse it.
//
// Run on the public theme's oracle (#f598zk0), against the design system it records.
func TestLiveTableAgreesWithTailwindOverTheFixtureCorpus(test *testing.T) {
	test.Parallel()
	forEachOracleCopy(test, "descriptor_fixtures.json", func(test *testing.T, fixtureName string) {
		fixtures := loadDescriptorFixtures(test, fixtureName)
		_, live := liveTableForOracle(test, fixtures.EntryPoint)
		expectLiveTableAgreesWithTailwind(test, fixtures, live)
	})
}

// expectLiveTableAgreesWithTailwind scores one live table against Tailwind's readings in one oracle.
func expectLiveTableAgreesWithTailwind(test *testing.T, fixtures *descriptorFixtures, live *Table) {
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
						test.Errorf("%s: the live table declined but Tailwind read %v#%d",
							one.ClassName, one.Reading.Order, one.Reading.Count)
					}
				}
			}
			continue
		}
		if one.Reading == nil {
			// No Tailwind reading is neither an agreement nor a disagreement. See the same branch in
			// descriptor_test.go for why these exist and why they are counted rather than skipped.
			withoutReading++
			continue
		}
		want := Reading{Order: one.Reading.Order, Count: one.Reading.Count}
		if !got.Equal(want) {
			mismatched++
			if mismatched <= 20 {
				test.Errorf("%s (root %q, %s %q, modifier %s %q): the live table read %v#%d, Tailwind said %v#%d",
					one.ClassName, one.Root, one.ValueKind, one.Value, one.ModifierKind, one.ModifierValue,
					got.Order, got.Count, want.Order, want.Count)
			}
			continue
		}
		agreed++
	}

	if mismatched > 0 {
		test.Errorf("%d of %d cases disagreed with Tailwind", mismatched, len(fixtures.Cases))
	}
	scored := agreed + mismatched
	test.Logf("the live table agreed on %d of %d scored cases over %d roots", agreed, scored, fixtures.Counts.Roots)
	test.Logf("  %d cases had no Tailwind reading and were reported rather than scored", withoutReading)
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

// checkTableCarriesTheTheme is the composition a table built from a repository's theme must carry, so a table that
// silently described bare Tailwind fails on exactly the tokens the repository added: namespaces longest-first and
// drawn from the theme, and every `@utility` block composed into the table, functional ones marked PerDeclaration.
func checkTableCarriesTheTheme(test *testing.T, name string, system *LoadedDesignSystem, table *Table) {
	test.Helper()

	if len(table.Namespaces) == 0 {
		test.Fatal("the table carries no namespaces, so every bare value resolves through inference alone")
	}
	// Longest-first is Tailwind's precedence and a contract of the field.
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
// must be recoverable from the `@utility` evaluator, or have no Tailwind reading at all.
//
// Without this, "the evaluator picks it up" is a claim in a comment. A comment cannot fail.
//
// Run on the public theme's oracle (#f598zk0): its fixtures, its measured table and its live table come from
// one design system.
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
			// The measured table answered a case Tailwind gave no reading for, so nothing is lost
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
			test.Errorf("%s: the evaluator read %v#%d where Tailwind said %v#%d",
				one.ClassName, reading.Order, reading.Count, want.Order, want.Count)
			continue
		}
		recovered++
	}

	if moved == 0 {
		test.Fatal("no cases moved from answered to declined, so this test proved nothing")
	}
	test.Logf(
		"%d cases the live table declines and the measured table answers: %d recovered exactly by the evaluator, %d had no Tailwind reading",
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

// benchmarkPublicTheme is the public theme's stylesheet and the vendored tailwindcss it loads over, for a
// benchmark, which has no test to hand vendored.TailwindPackageRootForTest.
func benchmarkPublicTheme(benchmark *testing.B) (entryPoint, packageRoot string) {
	benchmark.Helper()
	entryPoint, absolutePathError := filepath.Abs(filepath.Join("testdata", "public_theme", "theme.css"))
	if absolutePathError != nil {
		benchmark.Fatalf("resolving the public theme: %v", absolutePathError)
	}
	packageRoot, rootError := vendored.TailwindPackageRoot()
	if rootError != nil {
		benchmark.Fatalf("locating the tailwindcss snapshot: %v", rootError)
	}
	return entryPoint, packageRoot
}

// BenchmarkNewTable is what the table costs on top of the design system it is built from.
//
// The relevant comparison is `518fec5`'s 78.758µs for the design system build itself. This runs on
// the same counted path, once per program, so the question a reader has is whether adding it changed
// the order of magnitude of a per-run cost, not whether it is fast in isolation.
func BenchmarkNewTable(benchmark *testing.B) {
	entryPoint, packageRoot := benchmarkPublicTheme(benchmark)
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
	entryPoint, packageRoot := benchmarkPublicTheme(benchmark)

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
