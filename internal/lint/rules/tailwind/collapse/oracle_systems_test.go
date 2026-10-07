package tailwind

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/corpus"
	"github.com/system-inc/cohere/internal/lint/rules/tailwind/vendored"
)

// oracleCopies are the copies of an engine oracle a test compares against (#f598zk0): the public theme's,
// generated on testdata/public_theme and run on every machine and on CI, and ahra's, the live one the
// test was written against, opt-in through its corpus. A fixture named name.json has its public copy at
// name_public.json.
//
// The public copies are regenerated from the repository root whenever the public theme changes, since
// every one of them records what that theme compiles to. Node has no tailwindcss of its own here, so the
// install is borrowed from the ahra corpus, which records nothing of ahra's in the output:
//
//	P=internal/lint/rules/tailwind/collapse/testdata/public_theme/theme.css
//	D=internal/lint/rules/tailwind/collapse/testdata
//	T=internal/lint/rules/tailwind/tools
//	for w in wave1 wave2b wave2c wave3 wave4 wave5; do
//	    node $T/generate_functional/$w.mjs $P --resolve-root ahra: > $D/${w}_fixtures_public.json
//	done
//	node $T/generate_descriptor_table/fixtures.mjs $P --resolve-root ahra: > $D/descriptor_fixtures_public.json
//	node $T/generate_descriptors/extract.mjs $P --json $D/descriptor_table_public.json --resolve-root ahra: --no-corpus > /dev/null
//	node $T/generate_descriptor_table/context.mjs $P --resolve-root ahra: > $D/descriptor_context_public.json
//	node $T/generate_utility/enumerate.mjs $P --resolve-root ahra: > $D/utility_fixtures_public.json
var oracleCopies = []struct {
	name   string
	suffix string
}{
	{name: "public", suffix: "_public"},
	{name: "ahra", suffix: ""},
}

// forEachOracleCopy runs an oracle assertion once per copy, each as its own subtest, handing it the
// copy's fixture file name: base with the copy's suffix before its extension.
func forEachOracleCopy(t *testing.T, base string, run func(t *testing.T, fixtureName string)) {
	t.Helper()
	stem, isJson := strings.CutSuffix(base, ".json")
	if !isJson {
		t.Fatalf("%s is not a .json fixture", base)
	}
	for _, oracleCopy := range oracleCopies {
		t.Run(oracleCopy.name, func(t *testing.T) {
			t.Parallel()
			run(t, stem+oracleCopy.suffix+".json")
		})
	}
}

// oracleSibling is the copy of the oracle named base that belongs with fixtureName, the copy
// forEachOracleCopy handed a test: base itself for ahra's, base_public.json for the public theme's. A test
// whose oracle is several files reads each through this, so its copies never mix.
func oracleSibling(t *testing.T, fixtureName, base string) string {
	t.Helper()
	stem, isJson := strings.CutSuffix(base, ".json")
	if !isJson {
		t.Fatalf("%s is not a .json fixture", base)
	}
	for _, oracleCopy := range oracleCopies {
		if oracleCopy.suffix != "" && strings.HasSuffix(fixtureName, oracleCopy.suffix+".json") {
			return stem + oracleCopy.suffix + ".json"
		}
	}
	return base
}

// oracleSystem is the design system an oracle was captured from, by the entry point its fixture
// recorded, over the vendored tailwindcss. A repository path, such as the public theme's, loads on any
// machine; a corpus spelling skips naming its variable when the corpus is unset. Loading what the
// fixture names, rather than a system the test picks, is what keeps an oracle from being compared
// against a design system it never measured.
func oracleSystem(t *testing.T, entryPoint string) *LoadedDesignSystem {
	t.Helper()
	if entryPoint == "" {
		t.Fatal("the fixture records no entry point, so nothing says which design system it measured")
	}
	system, err := LoadDesignSystem(LoadOptions{
		EntryPoint:          corpus.Resolve(t, entryPoint),
		TailwindPackageRoot: vendored.TailwindPackageRoot(),
	})
	if err != nil {
		t.Fatalf("%s resolves, but its design system does not load: %v", entryPoint, err)
	}
	return system
}
