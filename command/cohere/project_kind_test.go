package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// A kind's value is what `COHERE_PROJECT_KIND` and the run cache's key spell, and its label is what a reader sees:
// the language for a program, package or module, and Formats for the root's format-only project (#9gjmwgs).
func TestAProjectKindIsSpelledForScriptsAndLabeledForReaders(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		kind  projectKind
		value string
		label string
	}{
		{projectKindTypeScriptProgram, "TypeScriptProgram", "TypeScript"},
		{projectKindSwiftPackage, "SwiftPackage", "Swift"},
		{projectKindGoModule, "GoModule", "Go"},
		{projectKindFolder, "Folder", "Formats"},
	} {
		if string(each.kind) != each.value || each.kind.label() != each.label {
			test.Errorf("%s is labeled %q, want the value %q labeled %q", each.kind, each.kind.label(), each.value,
				each.label)
		}
	}
}

// The project's heading, its label beside another project in its directory, the summary's counts and the overall
// footer all say the label, never the value.
func TestTheHeadingsAndTheSummariesSayTheKindsLabel(test *testing.T) {
	test.Parallel()
	module := discoveredProject{Directory: ".", Kind: projectKindGoModule}
	folder := discoveredProject{Directory: ".", Kind: projectKindFolder}
	if heading := sectionHeading(module); heading != "== . (Go) ==" {
		test.Errorf("the module's heading is %q, want == . (Go) ==", heading)
	}
	if heading := sectionHeading(folder); heading != "== . (Formats) ==" {
		test.Errorf("the folder's heading is %q, want == . (Formats) ==", heading)
	}
	labels := runLabels([]discoveredProject{module, folder})
	if !slices.Equal(labels, []string{".", ". (Formats)"}) {
		test.Errorf("the two projects at the root are labeled %q, want . and . (Formats)", labels)
	}
	runs := []projectRun{{project: module, label: labels[0]}, {project: folder, label: labels[1]}}
	if line := summaryLine(discovery{}, runs); !strings.Contains(line, "2 checked (1 Go, 1 Formats)") {
		test.Errorf("the summary line is %q, want it to count 1 Go, 1 Formats", line)
	}
	if footer := overallFooter(overallFactsOf(runs, time.Second), plain); !strings.Contains(footer,
		"2 projects: 1 Formats, 1 Go") {
		test.Errorf("the overall footer is %q, want it to count 1 Formats, 1 Go", footer)
	}
}
