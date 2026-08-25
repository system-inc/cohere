package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stagedBundles writes the eight bundles the engine loads into a temporary directory.
//
// It builds the fixture from the real fork rather than from invented bytes, because a digest over
// content nobody ships proves the hash function works and nothing about whether this function reads
// the right files.
func stagedBundles(t *testing.T) string {
	t.Helper()

	source, err := ResolveFormatterSource()
	if err != nil {
		t.Skipf("NOT MEASURED: the Prettier fork is unavailable, so the digest is unverified here: %v", err)
	}

	directory := t.TempDir()
	for _, name := range FormatterBundleNames {
		content, err := os.ReadFile(filepath.Join(source.Directory, name))
		if err != nil {
			t.Fatalf("reading %s from the fork: %v", name, err)
		}
		staged := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
			t.Fatalf("staging %s: %v", name, err)
		}
		if err := os.WriteFile(staged, content, 0o644); err != nil {
			t.Fatalf("staging %s: %v", name, err)
		}
	}
	return directory
}

// TestDigestMatchesAFaithfulCopy is the positive half: the same bytes in a different directory
// digest the same. Without it, a digest that hashed the path rather than the content would pass the
// mutation test below and still be wrong.
func TestDigestMatchesAFaithfulCopy(t *testing.T) {
	staged := stagedBundles(t)

	source, err := ResolveFormatterSource()
	if err != nil {
		t.Fatalf("resolving the fork: %v", err)
	}

	copied, err := digestBundles(staged)
	if err != nil {
		t.Fatalf("digesting the copy: %v", err)
	}

	if copied != source.Digest {
		t.Fatalf("a faithful copy digested differently:\n  fork %s\n  copy %s", source.Digest, copied)
	}
}

// TestDigestDetectsOneChangedByte is the known-dirty control. A digest that has never returned a
// different value for different bytes has not been shown to detect anything.
func TestDigestDetectsOneChangedByte(t *testing.T) {
	staged := stagedBundles(t)

	before, err := digestBundles(staged)
	if err != nil {
		t.Fatalf("digesting before: %v", err)
	}

	// The last bundle in the list, so a digest that stopped reading after the first would pass the
	// faithful-copy test and fail here.
	target := filepath.Join(staged, FormatterBundleNames[len(FormatterBundleNames)-1])
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading the bundle to mutate: %v", err)
	}
	content[0] ^= 0x01
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatalf("writing the mutated bundle: %v", err)
	}

	after, err := digestBundles(staged)
	if err != nil {
		t.Fatalf("digesting after: %v", err)
	}

	if after == before {
		t.Fatal("one flipped byte in the last bundle produced the same digest, so the digest does not cover it")
	}
}

// TestDigestRefusesAMissingBundle holds the loud-failure rule: absence is an error naming the file,
// never a digest over the seven that were there.
func TestDigestRefusesAMissingBundle(t *testing.T) {
	staged := stagedBundles(t)

	if err := os.Remove(filepath.Join(staged, FormatterBundleNames[0])); err != nil {
		t.Fatalf("removing a bundle: %v", err)
	}

	digest, err := digestBundles(staged)
	if err == nil {
		t.Fatalf("a missing bundle digested to %s instead of failing", digest)
	}
	if !strings.Contains(err.Error(), FormatterBundleNames[0]) {
		t.Fatalf("the error does not name the missing bundle: %v", err)
	}
}
