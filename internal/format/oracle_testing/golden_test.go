package oracle_testing

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGolden(test *testing.T, contents record) string {
	test.Helper()
	path := filepath.Join(test.TempDir(), "golden.json.gz")
	file, createError := os.Create(path)
	if createError != nil {
		test.Fatal(createError)
	}
	zipped := gzip.NewWriter(file)
	if encodeError := json.NewEncoder(zipped).Encode(contents); encodeError != nil {
		test.Fatal(encodeError)
	}
	if closeError := zipped.Close(); closeError != nil {
		test.Fatal(closeError)
	}
	if closeError := file.Close(); closeError != nil {
		test.Fatal(closeError)
	}
	return path
}

// TestAGoldenFromOtherBundlesIsRefused is the property the golden exists to keep: answers recorded from
// bundles this build does not embed are never compared against.
func TestAGoldenFromOtherBundlesIsRefused(test *testing.T) {
	test.Parallel()
	path := writeGolden(test, record{Digest: "recorded", Answers: map[string]Answer{"k": {Output: "x"}}})
	if _, readError := read(
		path,
		"embedded",
	); readError == nil || !strings.Contains(readError.Error(), "recorded from Prettier bundles recorded") {
		test.Fatalf("a golden from other bundles was read: %v", readError)
	}
	// The control: the same file, with the digest it was recorded under, reads.
	answers, readError := read(path, "recorded")
	if readError != nil || answers["k"].Output != "x" {
		test.Fatalf("a golden from these bundles was refused: %v, %v", answers, readError)
	}
}

// TestAMissingGoldenIsRefused: no golden is a failure that says how to record one, never an empty pass.
func TestAMissingGoldenIsRefused(test *testing.T) {
	test.Parallel()
	if _, readError := read(filepath.Join(test.TempDir(), "absent.json.gz"), "embedded"); readError == nil {
		test.Fatal("a missing golden read as empty")
	}
}

// TestKeysDoNotRunTogether: inputs that concatenate to the same text are different keys.
func TestKeysDoNotRunTogether(test *testing.T) {
	test.Parallel()
	if Key("ab", "c") == Key("a", "bc") {
		test.Fatal("two different inputs share a key")
	}
	// Two calls, so the comparison is of two keys computed apart rather than one expression with itself.
	first := Key("a", "b")
	second := Key("a", "b")
	if first != second {
		test.Fatal("one input has two keys")
	}
}

// TestTheBundlesHaveADigest: the digest every golden is stamped with is readable from this build.
func TestTheBundlesHaveADigest(test *testing.T) {
	test.Parallel()
	digest, bundlesDigestError := BundlesDigest()
	if bundlesDigestError != nil || len(digest) != 64 {
		test.Fatalf("no digest for the embedded bundles: %q, %v", digest, bundlesDigestError)
	}
}
