// Package generated_markers judges which Go files carry a "Code generated" marker exactly as staticcheck's
// generated.Analyzer does, line for line at the version go.mod links, v0.8.1, but in the reader's own buffer, with no
// allocation for each line. ReadGeneratedMarkers answers what the analyzer's Run answers, in its concrete type.
//
// It changes nothing as it initializes: generated.Analyzer.Run stays staticcheck's own until check, as it initializes,
// replaces it with a Run that answers ReadGeneratedMarkers (check/generated_markers.go says why). So this package's
// tests, which never link check, compare the scanner with staticcheck's Run on the same files, and the comparison over
// every Go file of the repository lives in generated_markers_repository, whose test alone reruns on every change.
package generated_markers
