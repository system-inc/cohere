package check

import "testing"

// generated.Analyzer runs readGeneratedMarkers from the package's initialization on, before any pass. The scanner's
// judgment is held to staticcheck's in generated_markers' own tests.
func TestGeneratedAnalyzerIsReplacedAsThePackageInitializes(test *testing.T) {
	test.Parallel()
	if !generatedMarkersReplaced {
		test.Fatal("generated.Analyzer's Run was not replaced as the package initialized")
	}
}
