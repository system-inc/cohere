package release

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/prettier"
)

// TestEmbeddedBundlesMatchTheirOwnDigest is the positive half: the bytes this test binary carries
// digest to a stable value, computed through the same function the build stamps with.
//
// It does not assert against the stamp, because a `go test` binary has none. What it holds is that
// the two paths into the hash agree, which is the property the comparison in CohereEmbeddedBundles
// depends on.
func TestEmbeddedBundlesMatchTheirOwnDigest(t *testing.T) {
	t.Parallel()

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

// TestBothDigestPathsAgree is the load-bearing one. CohereEmbeddedBundles compares a digest of
// in-memory bytes against a stamp computed from a directory, so the two paths agreeing is what makes
// the comparison mean anything. If they ever diverge the check fails on correct bundles, somebody
// relaxes it, and the stamp stops being worth carrying.
func TestBothDigestPathsAgree(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

// TestDigestKeysByName covers the half of the framing the length does not.
//
// Length framing is injective over a fixed ordered list, so it catches boundary shifts on its own
// and the name looks redundant. The case the name defends is a rename: a bundle moving from
// `plugins/yaml.js` to `plugins/yml.js` with identical bytes at the same position digests
// identically under length framing alone.
//
// That is not hypothetical. BundleFiles is a hand-edited list and a rename is an ordinary edit to
// it. Without the name, a binary built before such a rename and one built after carry the same
// digest while loading differently-named files.
//
// It goes through digestNamedBundles rather than re-implementing the hash, because the first
// version of this test hashed inline and passed while a mutation dropping the name from the real
// function went undetected -- a test of its own arithmetic rather than of the code.
func TestDigestKeysByName(t *testing.T) {
	t.Parallel()

	content := []byte("the same bytes under either name")

	before, err := digestNamedBundles([]string{"plugins/yaml.js"}, map[string][]byte{"plugins/yaml.js": content})
	if err != nil {
		t.Fatalf("digesting under the first name: %v", err)
	}

	after, err := digestNamedBundles([]string{"plugins/yml.js"}, map[string][]byte{"plugins/yml.js": content})
	if err != nil {
		t.Fatalf("digesting under the second name: %v", err)
	}

	if before == after {
		t.Fatal("the same bytes under two different names digested identically, so the name is not in the hash")
	}
}
