// Tests for the text-to-structure layer, fed output the two gates actually printed.
//
// This is a separate file from differential_test.go because it defends a different failure. Those
// tests prove the comparison can detect a difference given findings. These prove findings exist to
// compare at all.
//
// The gap matters more than it looks. Every parse failure in this layer is silent by construction:
// a regex that stops matching yields zero findings, and zero findings on one side makes every
// finding on the other look one-sided while the diff stays perfectly well-formed. A path
// normalization bug does the same thing — verify prints absolute paths, the gate prints relative
// ones, and if they fail to reconcile then two identical findings never match and the harness
// reports total disagreement in a shape that looks like a working result.
//
// So the inputs below are copied from real runs on the ahra tree at 01:16 on 2026-08-23, not
// written to match the patterns. A parser tested only against its author's idea of the format is a
// parser tested against nothing.
package differential

import "testing"

const ahraTreeRoot = "/Users/kirkouimet/Projects/ahra"

// TestParsesRealVerifyOutput uses verify's own stdout, coverage lines and all, so the summary
// lines have to be recognized as summaries rather than counted as unparsed findings.
func TestParsesRealVerifyOutput(test *testing.T) {
	output := `graph built in 1.752s — 9973 files in the program, 3407 of them ours
/Users/kirkouimet/Projects/ahra/modules/mcp/McpApi.ts:240:16 - Identifier "params" should not be abbreviated. Use "parameters" or a more descriptive name. [consistency-no-abbreviated-identifier/noParams]
lint: 1 findings — 22 rules over 3407 files, 2098253 nodes visited, in 1.036s
  note: rule consistency-no-utils-folder listened to no files — it ran, but it never looked
  suppressed: 279 findings silenced by a disable comment, 264 of them without a stated reason
  config: rule consistency-require-type-suffix scoped off for 6 files by the config`

	result, err := ParseVerify(output, ahraTreeRoot)
	if err != nil {
		test.Fatalf("parsing real verify output: %v", err)
	}
	if len(result.Findings) != 1 {
		test.Fatalf("want 1 finding from real output, got %d (unparsed: %v)", len(result.Findings), result.UnparsedLines)
	}
	if len(result.UnparsedLines) != 0 {
		test.Errorf("real output produced unparsed lines, each of which is a finding the harness cannot see: %v", result.UnparsedLines)
	}

	finding := result.Findings[0]
	if finding.File != "modules/mcp/McpApi.ts" {
		test.Errorf("path is %q, want it relative to the tree root — an absolute-versus-relative mismatch makes every finding look one-sided", finding.File)
	}
	if finding.Rule != "consistency-no-abbreviated-identifier" {
		test.Errorf("rule is %q, want the bare name with the message id stripped", finding.Rule)
	}
	if finding.Line != 240 {
		test.Errorf("line is %d, want 240", finding.Line)
	}
}

// TestParsesRealGateOutput uses the gate's own stdout. The parenthesized route-group directories
// are real in this tree and are the shape most likely to break a naive split on punctuation.
func TestParsesRealGateOutput(test *testing.T) {
	output := `app/(os-layout)/finance/_components/FinanceLedgerTable.tsx:49:5: error structure(react-component-no-multiple-primary): File contains a component with 108 lines.
libraries/structure/source/components/files/FileCarousel.tsx:36:8: error structure(react-component-no-multiple-primary): File contains a component with 184 lines.`

	result, err := ParseGate(output, ahraTreeRoot)
	if err != nil {
		test.Fatalf("parsing real gate output: %v", err)
	}
	if len(result.Findings) != 2 {
		test.Fatalf("want 2 findings from real output, got %d (unparsed: %v)", len(result.Findings), result.UnparsedLines)
	}
	if len(result.UnparsedLines) != 0 {
		test.Errorf("real output produced unparsed lines: %v", result.UnparsedLines)
	}
	if result.Findings[0].Rule != "react-component-no-multiple-primary" {
		test.Errorf("rule is %q, want the bare name with the plugin prefix stripped", result.Findings[0].Rule)
	}
	if result.Findings[0].File != "app/(os-layout)/finance/_components/FinanceLedgerTable.tsx" {
		test.Errorf("relative path mangled to %q", result.Findings[0].File)
	}
}

// TestTheTwoGatesAgreeOnOneFindingAfterParsing is the end-to-end control for this layer, and the
// one that would have caught a path or name normalization bug on its own.
//
// The same defect is fed in both formats. If parsing is right, the two produce equal keys and the
// comparison reports no difference. If either path or rule normalization is wrong, the keys differ
// and one finding becomes two one-sided differences — which is exactly what a total mismatch looks
// like, and it looks like working output.
func TestTheTwoGatesAgreeOnOneFindingAfterParsing(test *testing.T) {
	verifyResult, err := ParseVerify(
		`/Users/kirkouimet/Projects/ahra/app/Component.tsx:198:1 - File contains a component with 96 lines. [react-component-no-multiple-primary/tooMany]`,
		ahraTreeRoot,
	)
	if err != nil {
		test.Fatalf("parsing verify side: %v", err)
	}
	gateResult, err := ParseGate(
		`app/Component.tsx:198:1: error structure(react-component-no-multiple-primary): File contains a component with 96 lines.`,
		ahraTreeRoot,
	)
	if err != nil {
		test.Fatalf("parsing gate side: %v", err)
	}

	if len(verifyResult.Findings) != 1 || len(gateResult.Findings) != 1 {
		test.Fatalf("setup did not parse: verify %d, gate %d", len(verifyResult.Findings), len(gateResult.Findings))
	}

	if verifyResult.Findings[0].Key() != gateResult.Findings[0].Key() {
		test.Fatalf("the same defect keyed differently across the two formats:\n  verify %q\n  gate   %q\nnormalization is wrong, and this presents as total disagreement rather than as an error",
			verifyResult.Findings[0].Key(), gateResult.Findings[0].Key())
	}

	report := Compare(Inputs{
		VerifyFindings:   verifyResult.Findings,
		GateFindings:     gateResult.Findings,
		VerifyPopulation: Population{Findings: 1, FilesWalked: 3407, Rules: 22},
		GatePopulation:   Population{Findings: 1, FilesWalked: 3407, Rules: 181},
		VerifyRules:      map[string]bool{"react-component-no-multiple-primary": true},
		ConfiguredRules:  map[string]bool{"react-component-no-multiple-primary": true},
	})
	if len(report.Differences) != 0 {
		test.Errorf("one defect reported in both formats produced %d differences, want 0: %+v", len(report.Differences), report.Differences)
	}
}

// TestUnparsedLinesAreSurfacedNotDropped is this layer's own vacuity guard. A format change that
// silently stops matching must not present as a clean run.
func TestUnparsedLinesAreSurfacedNotDropped(test *testing.T) {
	result, err := ParseVerify("this line is neither a finding nor a summary", "")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		test.Fatalf("invented %d findings from an unrecognized line", len(result.Findings))
	}
	if len(result.UnparsedLines) != 1 {
		test.Error("an unrecognized line was dropped silently — a finding the harness cannot read must not look like a finding that is not there")
	}
}

// TestPluginSlashRuleNamesSurviveNormalization pins a real hazard in the shared normalizer.
//
// NormalizeRuleName strips everything after the first slash, to turn verify's
// `rule-name/messageId` into `rule-name`. Oxlint's own catalog writes rules as `plugin/rule-name`
// — `typescript/no-explicit-any` — where the part after the slash is the rule and the part before
// it is the plugin. The two conventions collide, and the collision is silent: the name still
// parses, still compares, and simply refers to the wrong thing.
//
// This test documents the boundary rather than asserting the collision is handled, because it is
// not handled today. The gate's own output wraps the plugin in parentheses, which is why nothing
// has broken yet: the slash form appears in .oxlintrc.json, not in the output this harness parses.
// If a future reader wires rule catalogs from the config into Inputs.ConfiguredRules, that is the
// moment this bites, and this test is where they will find out.
func TestPluginSlashRuleNamesSurviveNormalization(test *testing.T) {
	// The forms that actually appear in gate and verify output today, which must keep working.
	fromOutput := map[string]string{
		"structure(react-component-no-multiple-primary)": "react-component-no-multiple-primary",
		"nexus(consistency-require-constant-casing)":     "consistency-require-constant-casing",
		"consistency-no-abbreviated-identifier/noParams": "consistency-no-abbreviated-identifier",
		"consistency-no-shouting":                        "consistency-no-shouting",
	}
	for raw, want := range fromOutput {
		if got := NormalizeRuleName(raw); got != want {
			test.Errorf("NormalizeRuleName(%q) = %q, want %q", raw, got, want)
		}
	}

	// The config form, which does NOT survive, recorded so the limit is visible rather than
	// discovered later as a wrong answer.
	if got := NormalizeRuleName("typescript/no-explicit-any"); got != "typescript" {
		test.Errorf("NormalizeRuleName(%q) = %q; this test records today's behavior, and a change here means the plugin/rule collision was addressed — update the comment above rather than only this line",
			"typescript/no-explicit-any", got)
	}
}
