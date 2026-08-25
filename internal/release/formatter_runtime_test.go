package release

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/prettier"
)

// TestEmbeddedBundlesMatchTheirOwnDigest is the positive half: the bytes this test binary carries
// digest to a stable value, computed through the same function the build stamps with.
//
// It does not assert against the stamp, because a `go test` binary has none. What it holds is that
// the two paths into the hash agree, which is the property the comparison in VerifyEmbeddedBundles
// depends on.
func TestEmbeddedBundlesMatchTheirOwnDigest(t *testing.T) {
	source, err := prettier.Bundles()
	if err != nil {
		t.Fatalf("reading the embedded bundles: %v", err)
	}
	if source.Origin != prettier.Embedded {
		t.Skipf("NOT MEASURED: this run resolved bundles from %s, not the embedded copy", source.Path)
	}

	first, err := DigestBundleFiles(source.Files)
	if err != nil {
		t.Fatalf("digesting: %v", err)
	}
	if first == "" {
		t.Fatal("the embedded bundles digested to nothing")
	}

	second, err := DigestBundleFiles(source.Files)
	if err != nil {
		t.Fatalf("digesting again: %v", err)
	}
	if first != second {
		t.Fatalf("the digest is not stable: %s then %s", first, second)
	}
}

// TestBothDigestPathsAgree is the load-bearing one. VerifyEmbeddedBundles compares a digest of
// in-memory bytes against a stamp computed from a directory, so the two paths agreeing is what makes
// the comparison mean anything. If they ever diverge the check fails on correct bundles, somebody
// relaxes it, and the stamp stops being worth carrying.
func TestBothDigestPathsAgree(t *testing.T) {
	source, err := ResolveFormatterSource()
	if err != nil {
		t.Skipf("NOT MEASURED: the Prettier fork is unavailable, so the two paths are unverified here: %v", err)
	}

	embedded, err := prettier.Bundles()
	if err != nil {
		t.Fatalf("reading the embedded bundles: %v", err)
	}
	if embedded.Origin != prettier.Embedded {
		t.Skipf("NOT MEASURED: this run resolved bundles from %s", embedded.Path)
	}

	fromMemory, err := DigestBundleFiles(embedded.Files)
	if err != nil {
		t.Fatalf("digesting the embedded bytes: %v", err)
	}

	if fromMemory != source.Digest {
		t.Fatalf(
			"the vendored bundles and the fork's disagree:\n  embedded %s\n  fork     %s\nThe vendored copy is a different build than the fork holds",
			fromMemory, source.Digest,
		)
	}
}

// TestDigestFilesRefusesAnAbsentBundle holds the loud-failure rule on the in-memory path, matching
// what the directory path already does: absence names the file rather than digesting what is there.
func TestDigestFilesRefusesAnAbsentBundle(t *testing.T) {
	source, err := prettier.Bundles()
	if err != nil {
		t.Fatalf("reading the embedded bundles: %v", err)
	}

	short := make(map[string][]byte, len(source.Files))
	for name, content := range source.Files {
		short[name] = content
	}
	missing := FormatterBundleNames[len(FormatterBundleNames)-1]
	delete(short, missing)

	digest, err := DigestBundleFiles(short)
	if err == nil {
		t.Fatalf("a set missing %s digested to %s instead of failing", missing, digest)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("the error does not name the absent bundle: %v", err)
	}
}

// TestDigestFramesEachBundle is the control for why the hash writes a name and a length before each
// bundle rather than concatenating the bytes.
//
// The case it defends is a boundary shift: two bundles holding "AB" and "C" produce the same
// concatenated stream as the same two holding "A" and "BC". A digest over the raw concatenation
// cannot tell those apart, so a vendoring step that truncated one bundle and lengthened the next by
// the same amount would digest identically to a correct set.
//
// Verified as load-bearing by mutation rather than by argument: with the framing removed this test
// fails and the swap and changed-byte tests all still pass, so it is the only one covering it.
func TestDigestFramesEachBundle(t *testing.T) {
	shifted := func(first, second []byte) string {
		t.Helper()
		files := make(map[string][]byte, len(FormatterBundleNames))
		for _, name := range FormatterBundleNames {
			files[name] = []byte{}
		}
		files[FormatterBundleNames[0]] = first
		files[FormatterBundleNames[1]] = second

		digest, err := DigestBundleFiles(files)
		if err != nil {
			t.Fatalf("digesting: %v", err)
		}
		return digest
	}

	if shifted([]byte("AB"), []byte("C")) == shifted([]byte("A"), []byte("BC")) {
		t.Fatal("two bundle sets with the same concatenated bytes and different boundaries digested identically")
	}
}
