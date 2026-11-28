package check

import (
	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_language/generated_markers"
)

// staticcheck's generated.Analyzer judges every file of a package it runs on by reading it line by line for a
// "Code generated" marker, and its reader's ReadBytes copies each line into a slice of its own: one allocation for
// every line of every file checked, only to compare it and drop it. generated_markers.ReadGeneratedMarkers judges the
// same lines in place, in the reader's own buffer, and readGeneratedMarkers replaces the analyzer's Run as the package
// initializes, in place as wrapRuns wraps it and for the same reason: every pass reads what it requires keyed by the
// analyzer's own pointer, so a copy standing in for it would be read by nobody. The judgment is staticcheck's, line for
// line (generated_markers.ScanGeneratedMarker), at the version go.mod links, v0.8.1; its result is the same type, so
// every reader of pass.ResultOf[generated.Analyzer] reads what it read before. The replacement lives here, not in
// generated_markers, so that package's tests still hold staticcheck's own Run to compare the scanner with.

// generatedMarkersReplaced is whether generated.Analyzer runs readGeneratedMarkers, which it does from the package's
// initialization, before any pass could run it and before wrapRuns wraps it.
var generatedMarkersReplaced = replaceGeneratedRun()

// replaceGeneratedRun makes readGeneratedMarkers generated.Analyzer's Run. Only the package's initialization calls it.
func replaceGeneratedRun() bool {
	generated.Analyzer.Run = readGeneratedMarkers
	return true
}

// readGeneratedMarkers is generated.Analyzer's Run: generated_markers.ReadGeneratedMarkers over the pass, which never
// fails.
func readGeneratedMarkers(pass *analysis.Pass) (any, error) {
	return generated_markers.ReadGeneratedMarkers(pass), nil
}
