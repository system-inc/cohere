package typescript

// Code generated from typescript-eslint v8.71.0's tests/rules/no-shadow/*.test.ts and edge rows. Do not edit by hand.
//
// The 239 upstream rows are every row in the two test files, 139 valid and 100 invalid, extracted by
// loading the files with RuleTester stubbed. 84 edge rows were added for what they leave open. Before recording,
// every upstream row was linted untyped, as upstream's RuleTester runs it, and agreed with upstream's own
// assertions: message ids, lines, columns and the shadowed declaration's place. Every row was then replayed
// against the installed rule (@typescript-eslint/eslint-plugin 8.71.0, one program per row) under
// noShadowCorpusTsconfig, on the bytes this harness writes: the source trimmed plus one newline, in file.ts, or
// in file.d.ts for a row upstream names a definition file. Upstream's rows are modules, and one with no import
// or export gets `export {};` appended (exportAppended), since cohere's module is TypeScript's. Each finding
// records upstream's message id, the exact text its location covers, where it starts, and the shadowed
// declaration's line and column as the message quotes them, or "global". The rows that configure `globals`
// are pinned and recorded with it dropped, since cohere carries no globals configuration; edge rows make the
// same points with the globals the program has. TestNoShadowUpstreamCorpus replays this table.

import "slices"

// noShadowCorpusTsconfig is the no-unused-vars options corpus's tsconfig. include names the row's file.
const noShadowCorpusTsconfig = "{\n \"compilerOptions\": {\n  \"strict\": true,\n  \"target\": \"ES2022\",\n  \"lib\": [\n   \"ES2022\",\n   \"ESNext.Disposable\"\n  ],\n  \"types\": []\n },\n \"include\": [\n  \"file.ts\"\n ]\n}"

// noShadowCorpusFinding is one finding the installed rule produced, by upstream's message id.
type noShadowCorpusFinding struct {
	id       string
	text     string
	at       string
	shadowed string
}

// noShadowCorpusCase is one row with the installed rule's verdict. edge is empty for upstream's own rows.
type noShadowCorpusCase struct {
	index          int
	edge           string
	valid          bool
	fileName       string
	source         string
	options        string
	exportAppended bool
	pinned         []string
	findings       []noShadowCorpusFinding
}

// noShadowCorpusUpstreamValid, noShadowCorpusUpstreamInvalid and noShadowCorpusUpstreamPinned pin how many of
// upstream's rows are here and how many carry a pin, so a filter cannot empty either direction.
const noShadowCorpusUpstreamValid, noShadowCorpusUpstreamInvalid, noShadowCorpusUpstreamPinned = 139, 100, 22

// noShadowCorpus is the whole table in its recorded order: no-shadow.test.ts's valid then invalid rows,
// then no-shadow-eslint.test.ts's, then the edge rows. One table would pass the long-file limit by itself, and
// upstream's valid and invalid rows are not contiguous across the two files, so each file's valid rows and
// its invalid rows are a piece of their own, split at index 146, where no-shadow-eslint.test.ts begins. Each
// piece lives in a file named for it, under the header above.
var noShadowCorpus = slices.Concat(
	noShadowUpstreamTypeScriptValidCorpus,
	noShadowUpstreamTypeScriptInvalidCorpus,
	noShadowUpstreamEslintValidCorpus,
	noShadowUpstreamEslintInvalidCorpus,
	noShadowEdgeCorpus,
)
