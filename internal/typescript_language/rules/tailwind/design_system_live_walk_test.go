package tailwind

import (
	"context"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/typescript_language/program"
	"github.com/system-inc/cohere/internal/typescript_language/rule"
	"github.com/system-inc/cohere/internal/typescript_language/rule_testing"
	tailwindcollapse "github.com/system-inc/cohere/internal/typescript_language/rules/tailwind/collapse"
)

// Build-once, asserted with every rule this package registers asking at once, the arrangement the shipped run has.
//
// `TestDesignSystemIsBuiltOncePerProgram` in design_system_test.go counts builds across a real
// `program.Graph.Walk` with a single probe rule. This file is the second half of the same claim, and it
// exists because a single rule cannot reach what the shipped run does: every Tailwind rule calling
// `DesignSystemForProgram` on every file, which is the arrangement where a per-rule cache rather than a
// per-run one would still pass a single-rule test, on a tree big enough that the walk strides across
// files on several workers for long enough that a racing second build has room to happen.
//
// It used to walk a consumer's checkout, 3,481 files over a nine-file `@import` graph. Cohere's tests no
// longer read other repositories (#5wj23dd), so it walks the same fixture project as the probe test, laid
// out with as many files as its naming allows; the build count is the property, and it doesn't depend on
// the stylesheet's size.
//
// The counter is process-scoped, so what is asserted is a delta across the walk rather than an
// absolute. That is deliberate: `go test` runs this package's tests in one process, and an absolute
// assertion here would be a test that passes or fails on which other tests ran first.
//
// Every helper is prefixed `liveWalk` for the reason design_system_test.go states: several agents
// work in this package at once, and a helper whose name does not say which component owns it is a
// collision waiting to happen.

// liveWalkRules is every rule in this package, as the shipped run registers them, each run unconfigured.
//
// Listed rather than read back out of the catalog, because the catalog is global and another
// package's rules landing in it would change what this test walks without anyone editing this file.
// If a tenth Tailwind rule is added and not added here, this test keeps passing while covering less,
// which `TestLiveWalkCoversEveryRegisteredTailwindRule` below turns into a failure.
func liveWalkRules(test testing.TB) []rule.Entry {
	return []rule.Entry{
		rule_testing.EntryOf(test, EnforceConsistentClassOrder),
		rule_testing.EntryOf(test, EnforceConsistentVariantOrder),
		rule_testing.EntryOf(test, EnforceConsistentImportantPosition),
		rule_testing.EntryOf(test, EnforceConsistentVariableSyntax),
		rule_testing.EntryOf(test, EnforceShorthandClasses),
		rule_testing.EntryOf(test, EnforceCanonicalClasses),
		rule_testing.EntryOf(test, NoConcatenatedClasses),
		rule_testing.EntryOf(test, NoConflictingClasses),
		rule_testing.EntryOf(test, NoDeprecatedClasses),
		rule_testing.EntryOf(test, NoDuplicateClasses),
		rule_testing.EntryOf(test, NoUnknownClasses),
		rule_testing.EntryOf(test, NoUnnecessaryWhitespace),
		rule_testing.EntryOf(test, NoPhysicalDirection),
	}
}

// liveWalkFileCount is how many components the fixture project lays down: designSystemProject names each by a letter
// and a digit, so 260 is every name it has, and far more files than workers.
const liveWalkFileCount = 260

// liveWalkMeasuredBuildCost is what one build of a real repository's design system costs.
//
// Measured at 1.66ms mean over 30 cold builds of a consumer's nine-file stylesheet graph on 2026-08-25,
// covering the design system and the descriptor table that rides the same cache entry. It appears only
// inside a failure message, so drift costs an inaccurate estimate rather than a wrong verdict.
const liveWalkMeasuredBuildCost = 1660 * time.Microsecond

// TestDesignSystemIsBuiltOnceUnderTheRealWalk is the end-to-end form of the build-once claim.
//
// The failure it guards has no symptom other than time. A sibling rule in this tree spent 2,329ms of
// setup against 0.54ms of listening across 1,862 files because its scan was redone per file, while
// its own comment said it happened once. Every finding stayed correct and every test stayed green.
// So the claim is a counter read across a real walk with every rule, which is the only form of it that
// cannot decay into a sentence someone believes.
// Not parallel: it resets the package designSystemCache and asserts the process-wide
// tailwindcollapse.BuildsSoFar moved by exactly one across the walk, which any parallel test building a
// design system or evicting the cache's one slot would inflate.
func TestDesignSystemIsBuiltOnceUnderTheRealWalk(test *testing.T) {
	liveWalkRepository := designSystemProject(test, liveWalkFileCount)

	resetDesignSystemCacheForTest()

	graph, buildError := program.Build(program.Options{
		ConfigFileName:   "tsconfig.json",
		CurrentDirectory: liveWalkRepository,
	})
	if buildError != nil {
		test.Fatalf("building a program over %s: %v", liveWalkRepository, buildError)
	}

	files := graph.ProjectFiles()
	if len(files) < liveWalkFileCount {
		test.Fatalf("the fixture project yielded %d project files of the %d laid down, so the walk covers less "+
			"than it claims", len(files), liveWalkFileCount)
	}

	// The pool is asserted rather than assumed. A walk that silently ran single-threaded would pass
	// every count below while proving nothing about the concurrency the cache's mutex exists for.
	if workers := graph.Workers(); workers < 2 {
		test.Fatalf("the walk would run with %d worker(s), so this test cannot observe a race between "+
			"two builds and the property it claims to prove would be untested", workers)
	}

	rules := liveWalkRules(test)
	before := tailwindcollapse.BuildsSoFar()
	if _, walkError := graph.Walk(context.Background(), files, rules); walkError != nil {
		test.Fatalf("walking %s: %v", liveWalkRepository, walkError)
	}
	built := tailwindcollapse.BuildsSoFar() - before

	if built != 1 {
		// The counterfactual is stated in the failure rather than left for the reader to compute,
		// because the whole defect is that the number is invisible until someone multiplies it out.
		test.Fatalf("the design system was built %d times across %d files by %d rules on %d workers; "+
			"it must be built exactly once per run. At the measured %v per build on a real "+
			"repository, %d extra builds is roughly %v of pure waste, and nothing else would fail",
			built, len(files), len(rules), graph.Workers(), liveWalkMeasuredBuildCost,
			built-1, time.Duration(built-1)*liveWalkMeasuredBuildCost)
	}
}

// TestLiveWalkCoversEveryRegisteredTailwindRule keeps the list above honest.
//
// `liveWalkRules` is written by hand, so the failure mode is a tenth rule that reads the design
// system, is never added here, and is therefore never covered by the build-once assertion. That is a
// test that keeps passing while measuring less, which is the quietest way for a guard to stop
// guarding. Counting the list against the package's own registrations turns it into a failure that
// names the missing rule.
func TestLiveWalkCoversEveryRegisteredTailwindRule(test *testing.T) {
	test.Parallel()
	covered := make(map[string]bool)
	for _, entry := range liveWalkRules(test) {
		covered[entry.Name] = true
	}

	// The catalog, which in this package's test binary holds this package's registrations and
	// nothing else: `rule.Register` is called from each rule package's own `init`, and a test binary
	// links only the packages under test and their imports.
	//
	// Guarded rather than trusted. If that ever stops being true the count grows, and a silent grow
	// would make this test start demanding coverage of rules that are not this package's business,
	// which is a failure that would read as this test being wrong rather than as the catalog moving.
	registered := rule.Registered()
	if len(registered) != len(liveWalkRules(test)) {
		test.Fatalf("the catalog holds %d rules and liveWalkRules lists %d; either a Tailwind rule was "+
			"added without being added to liveWalkRules, or another package's rules are now linked "+
			"into this test binary and this comparison is measuring the wrong set",
			len(registered), len(liveWalkRules(test)))
	}
	for _, entry := range registered {
		if !covered[entry.Name] {
			test.Errorf("rule %q is registered by this package but is not in liveWalkRules, so the "+
				"build-once assertion never walks it", entry.Name)
		}
	}
}
