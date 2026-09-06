package release

import (
	"errors"
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

// TestStampFollowsTheBytesNotTheFlag pins the defect that the stamp is about what shipped.
//
// The bundles are embedded unconditionally, so a binary built without requiring a fork still
// formats. A stamp gated on that flag produced exactly that binary: it rewrote a file and reported
// no formatter, so a bug report from it could not name which Prettier did the rewriting.
//
// This drives resolveFormatterStamp with a stub rather than calling ResolveFormatterSource, because
// the first version of this test did the latter and passed with the defect reintroduced. That
// function was never broken. The defect was in whether the resolve is reached at all, so a test
// that never exercises the gate cannot see it, however true its assertions are.
func TestStampFollowsTheBytesNotTheFlag(t *testing.T) {
	t.Parallel()

	stamped := FormatterSource{Commit: "abc123", Digest: "def456"}
	resolve := func() (FormatterSource, error) { return stamped, nil }

	for _, requireFork := range []bool{true, false} {
		formatter, err := resolveFormatterStamp(requireFork, resolve)
		if err != nil {
			t.Fatalf("requireFork=%v: %v", requireFork, err)
		}
		if formatter.Commit != stamped.Commit || formatter.Digest != stamped.Digest {
			t.Fatalf(
				"requireFork=%v dropped the stamp: got commit %q digest %q, want %q and %q",
				requireFork, formatter.Commit, formatter.Digest, stamped.Commit, stamped.Digest,
			)
		}
	}
}

// TestStampRequiresAForkOnlyWhenAsked holds the other half of the same decision: what an
// unavailable fork means. Required, it fails the release. Not required, it leaves an empty stamp,
// which says plainly that the binary cannot name its formatter.
func TestStampRequiresAForkOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	resolve := func() (FormatterSource, error) {
		return FormatterSource{}, errors.New("no fork here")
	}

	if _, err := resolveFormatterStamp(true, resolve); err == nil {
		t.Fatal("a required fork that could not be resolved did not fail the release")
	}

	formatter, err := resolveFormatterStamp(false, resolve)
	if err != nil {
		t.Fatalf("an unrequired fork that could not be resolved failed the release: %v", err)
	}
	if formatter.Commit != "" || formatter.Digest != "" {
		t.Fatalf("an unresolvable fork produced a stamp: %+v", formatter)
	}
}
