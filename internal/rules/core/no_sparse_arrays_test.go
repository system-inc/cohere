package core

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const sparseArrayFile = "/repository/source/Thing.ts"

func TestNoSparseArraysFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"a hole in the middle", "export const Values = [1, , 3];\n"},
		{"a leading hole", "export const Values = [, 2];\n"},
		{"two holes", "export const Values = [1, , , 4];\n"},
		// A hole followed by a trailing comma. This is the case that separates the two commas the
		// rule reports from the one it must not, and reading only the last element would miss it.
		{"a hole with a trailing comma", "export const Values = [1, , 3, ];\n"},
		{"a nested array's hole", "export const Values = [[1, , 3]];\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t, ruletest.Run(t, NoSparseArrays, sparseArrayFile, testCase.sourceText),
				"unexpectedSparseArray")
		})
	}
}

func TestNoSparseArraysStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"a dense array", "export const Values = [1, 2, 3];\n"},
		// The decision boundary. A trailing comma is one comma after the last element and produces
		// no hole; a hole is two commas with nothing between them. Every formatter in this stack
		// emits the trailing form, so a rule that reported it would fight the formatter over syntax
		// that means nothing.
		{"a trailing comma", "export const Values = [1, 2, 3, ];\n"},
		{"a trailing comma on one element", "export const Values = [1, ];\n"},
		{"an explicit undefined", "export const Values = [1, undefined, 3];\n"},
		{"an empty array", "export const Values = [];\n"},
		{"a spread", "declare const other: number[];\nexport const Values = [1, ...other, 3];\n"},
		{"no array at all", "export const Value = 1;\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.Run(t, NoSparseArrays, sparseArrayFile, testCase.sourceText))
		})
	}
}

// One finding per array rather than one per hole, so a two-hole array reports once. Reported on the
// literal rather than the hole because the hole has no text of its own to point at.
func TestNoSparseArraysReportsOncePerArray(t *testing.T) {
	result := ruletest.Run(t, NoSparseArrays, sparseArrayFile, "export const Values = [1, , , 4];\n")
	if len(result.Diagnostics) != 1 {
		t.Fatalf("want exactly one finding for a two-hole array, got %d", len(result.Diagnostics))
	}
}
