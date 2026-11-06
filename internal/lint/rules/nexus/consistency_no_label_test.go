package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const labelFile = "/project/source/Thing.ts"

// labelWant is one finding a case expects: the label, and which occurrence of its name in the source the finding
// covers, counted from 1, so a jump naming the label later in the source is told apart from the label itself.
type labelWant struct {
	label      string
	occurrence int
}

// expectLabels requires exactly the wanted findings, in order, each covering its label's name at its occurrence and
// naming it in the message. A count alone would pass a rule that reports the whole labeled statement, which is what
// ESLint's `no-labels` did.
func expectLabels(test *testing.T, sourceText string, wants []labelWant) {
	test.Helper()
	result := rule_testing.Run(test, ConsistencyNoLabel, labelFile, sourceText)
	if len(wants) == 0 {
		rule_testing.ExpectClean(test, result)
		return
	}
	wantIds := make([]string, 0, len(wants))
	for range wants {
		wantIds = append(wantIds, "label")
	}
	rule_testing.ExpectFindings(test, result, wantIds...)
	for index, want := range wants {
		start := nthIndex(sourceText, want.label, want.occurrence)
		if start < 0 {
			test.Fatalf("the source holds no occurrence %d of %q", want.occurrence, want.label)
		}
		diagnostic := result.Diagnostics[index]
		if diagnostic.Range.Pos() != start || diagnostic.Range.End() != start+len(want.label) {
			test.Errorf("finding %d covers %q at %d, want %q at %d", index,
				sourceText[diagnostic.Range.Pos():diagnostic.Range.End()], diagnostic.Range.Pos(), want.label, start)
		}
		if !strings.Contains(diagnostic.Message.Description, "`"+want.label+"` is a label") {
			test.Errorf("finding %d's message does not name %q: %s", index, want.label, diagnostic.Message.Description)
		}
	}
}

// Every labeled statement is reported once, on its label, whatever it labels and wherever it sits (Kirk, 2026-11-06,
// #4b337jk). The jumps naming a label are not findings of their own, which is where ESLint's `no-labels` differed.
func TestConsistencyNoLabelFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wants      []labelWant
	}{
		{"a label no jump names", "export function sum(values: number[]): number {\n  let total = 0\n" +
			"  outer: for (const value of values) {\n    total += value\n  }\n  return total\n}\n",
			[]labelWant{{"outer", 1}}},
		{"a label a continue and a break name, once", "export function find(grid: number[][]): void {\n" +
			"  outer: for (const row of grid) {\n    for (const cell of row) {\n" +
			"      if (cell < 0) {\n        continue outer\n      }\n" +
			"      if (cell > 9) {\n        break outer\n      }\n    }\n  }\n}\n",
			[]labelWant{{"outer", 1}}},
		{"a while", "export function spin(): void {\n  waiting: while (true) {\n    break waiting\n  }\n}\n",
			[]labelWant{{"waiting", 1}}},
		{"a block a break leaves", "export function leave(): void {\n  block: {\n    break block\n  }\n}\n",
			[]labelWant{{"block", 1}}},
		{"a switch", "export function choose(value: number): void {\n" +
			"  choice: switch (value) {\n    case 0:\n      break choice\n  }\n}\n",
			[]labelWant{{"choice", 1}}},
		{"a plain statement", "export function assign(): void {\n  marker: void 0\n}\n",
			[]labelWant{{"marker", 1}}},
		{"a label stacked on another", "export function stack(): void {\n  first: second: for (;;) {\n" +
			"    break second\n  }\n}\n",
			[]labelWant{{"first", 1}, {"second", 1}}},
		{"labels nested in each other", "export function nest(grid: number[][]): void {\n" +
			"  rows: for (const row of grid) {\n    cells: for (const cell of row) {\n" +
			"      if (cell) {\n        continue rows\n      }\n      break cells\n    }\n  }\n}\n",
			[]labelWant{{"rows", 1}, {"cells", 1}}},
		{"a label inside an arrow function", "export const run = (values: number[]): void => {\n" +
			"  scan: for (const value of values) {\n    if (value) {\n      break scan\n    }\n  }\n}\n",
			[]labelWant{{"scan", 1}}},
		{"a label inside a class method", "export class Walker {\n  walk(values: number[]): void {\n" +
			"    steps: for (const value of values) {\n      if (value) {\n        continue steps\n      }\n    }\n" +
			"  }\n}\n",
			[]labelWant{{"steps", 1}}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			expectLabels(test, testCase.sourceText, testCase.wants)
		})
	}
}

// The jumps without a label, and the colons that are not labels: an object key, a case clause, a ternary, a type
// annotation, and a label's shape inside a comment or a string.
func TestConsistencyNoLabelStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"a break and a continue with no label", "export function walk(values: number[]): void {\n" +
			"  for (const value of values) {\n    if (value < 0) {\n      continue\n    }\n" +
			"    if (value > 9) {\n      break\n    }\n  }\n}\n"},
		{"an object key", "export const point = { outer: 1, label: 2 }\n"},
		{"a case clause", "export function choose(value: number): number {\n  switch (value) {\n" +
			"    case 0:\n      return 1\n    default:\n      return 2\n  }\n}\n"},
		{"a ternary", "export const pick = (flag: boolean): number => (flag ? 1 : 2)\n"},
		{"a type annotation", "export const outer: number = 1\n"},
		{"a label's shape in a comment", "// outer: for (const row of grid) { continue outer }\n" +
			"export const Value = 1\n"},
		{"a label's shape in a string", "export const Text = 'outer: for (;;) { break outer }'\n"},
		{"a function that returns in place of a jump", "export function find(grid: number[][]): number {\n" +
			"  for (const row of grid) {\n    for (const cell of row) {\n      if (cell > 9) {\n" +
			"        return cell\n      }\n    }\n  }\n  return 0\n}\n"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			expectLabels(test, testCase.sourceText, nil)
		})
	}
}
