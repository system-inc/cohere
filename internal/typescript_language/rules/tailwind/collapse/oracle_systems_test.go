package tailwind

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/repository"
	"github.com/system-inc/cohere/internal/typescript_language/rules/tailwind/vendored"
)

// oracleCopies are the copies of Tailwind oracle a test compares against (#f598zk0): the public theme's,
// generated on testdata/public_theme and run on every machine and on CI. A fixture named name.json has its
// public copy at name_public.json; the unsuffixed copies were captured on a consumer's theme, which cohere's
// tests no longer read (#5wj23dd).
//
// The public copies are regenerated from the repository root whenever the public theme changes, since
// every one of them records what that theme compiles to. Node has no tailwindcss of its own here, so the
// install is borrowed from R, any directory whose node_modules holds tailwindcss 4.3.3, which records
// nothing of its own in the output:
//
//	R=/path/to/a/checkout/with/tailwindcss
//	P=internal/typescript_language/rules/tailwind/collapse/testdata/public_theme/theme.css
//	D=internal/typescript_language/rules/tailwind/collapse/testdata
//	T=internal/typescript_language/rules/tailwind/tools
//	for w in wave1 wave2b wave2c wave3 wave4 wave5; do
//	    node $T/generate_functional/$w.mjs $P --resolve-root $R > $D/${w}_fixtures_public.json
//	done
//	node $T/generate_descriptor_table/fixtures.mjs $P --resolve-root $R > $D/descriptor_fixtures_public.json
//	node $T/generate_descriptors/extract.mjs $P --json $D/descriptor_table_public.json --resolve-root $R \
//	    --no-corpus > /dev/null
//	node $T/generate_descriptor_table/context.mjs $P --resolve-root $R > $D/descriptor_context_public.json
//	node $T/generate_utility/enumerate.mjs $P --resolve-root $R > $D/utility_fixtures_public.json
var oracleCopies = []struct {
	name   string
	suffix string
}{
	{name: "public", suffix: "_public"},
}

// forEachOracleCopy runs an oracle assertion once per copy, each as its own subtest, handing it the
// copy's fixture file name: base with the copy's suffix before its extension.
func forEachOracleCopy(test *testing.T, base string, run func(test *testing.T, fixtureName string)) {
	test.Helper()
	stem, isJson := strings.CutSuffix(base, ".json")
	if !isJson {
		test.Fatalf("%s is not a .json fixture", base)
	}
	for _, oracleCopy := range oracleCopies {
		test.Run(oracleCopy.name, func(test *testing.T) {
			test.Parallel()
			run(test, stem+oracleCopy.suffix+".json")
		})
	}
}

// oracleSibling is the copy of the oracle named base that belongs with fixtureName, the copy
// forEachOracleCopy handed a test: base_public.json for the public theme's, and base itself for a copy with
// no suffix. A test whose oracle is several files reads each through this, so its copies never mix.
func oracleSibling(test *testing.T, fixtureName, base string) string {
	test.Helper()
	stem, isJson := strings.CutSuffix(base, ".json")
	if !isJson {
		test.Fatalf("%s is not a .json fixture", base)
	}
	for _, oracleCopy := range oracleCopies {
		if oracleCopy.suffix != "" && strings.HasSuffix(fixtureName, oracleCopy.suffix+".json") {
			return stem + oracleCopy.suffix + ".json"
		}
	}
	return base
}

// oracleSystem is the design system an oracle was captured from, by the entry point its fixture
// recorded, over the vendored tailwindcss. The entry point is a path inside this repository, such as
// the public theme's, so it loads on any machine. Loading what the fixture names, rather than a system
// the test picks, is what keeps an oracle from being compared against a design system it never
// measured.
func oracleSystem(test *testing.T, entryPoint string) *LoadedDesignSystem {
	test.Helper()
	if entryPoint == "" {
		test.Fatal("the fixture records no entry point, so nothing says which design system it measured")
	}
	system, loadDesignSystemError := LoadDesignSystem(LoadOptions{
		EntryPoint:          repository.Path(test, entryPoint),
		TailwindPackageRoot: vendored.TailwindPackageRootForTest(test),
	})
	if loadDesignSystemError != nil {
		test.Fatalf("%s resolves, but its design system does not load: %v", entryPoint, loadDesignSystemError)
	}
	return system
}
