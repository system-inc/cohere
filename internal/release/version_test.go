package release

import (
	"strings"
	"testing"
)

// The defect these guard is a label that outlives what it describes. The vendored compiler moved
// from `microsoft/typescript-go`, which is archived, to `microsoft/TypeScript`, and a `--version`
// line that still said "typescript-go" would have kept printing a commit against a repository it
// does not exist in. That is a true fact wearing a wrong name: the reader checks the hash, finds it
// well-formed, and has no way to learn it resolves nowhere.

func TestVersionNamesTheRepositoryAlongsideTheCommit(t *testing.T) {
	provenance := Provenance{
		Version:          "1.2.3",
		CompilerCommit:   "d6c4afddb2c55f4a9dea7b59293a99a8fdea1799",
		CompilerUpstream: "microsoft/TypeScript",
		GoToolchain:      "go1.27.0",
		Platform:         "darwin/arm64",
	}

	rendered := provenance.String()

	// A commit hash is not lookup-able without the repository it lives in, and two pins from either
	// side of the migration are both forty hex characters.
	if !strings.Contains(rendered, "microsoft/TypeScript@d6c4afddb2c55f4a9dea7b59293a99a8fdea1799") {
		t.Errorf("the compiler line does not pair the repository with the commit:\n%s", rendered)
	}

	// The old label must not survive anywhere, because it is now false rather than merely stale.
	if strings.Contains(rendered, "typescript-go") {
		t.Errorf("the output still says typescript-go, which is the archived upstream:\n%s", rendered)
	}
}

func TestVersionDegradesToTheBareCommitWhenTheUpstreamIsUnknown(t *testing.T) {
	// A submodule with no configured origin still yields the commit, which is the fact a bug report
	// needs most. What it must not do is print a dangling "@" or invent a repository.
	for _, upstream := range []string{"", "unknown"} {
		provenance := Provenance{
			Version:          "1.2.3",
			CompilerCommit:   "abc123",
			CompilerUpstream: upstream,
			GoToolchain:      "go1.27.0",
			Platform:         "darwin/arm64",
		}

		rendered := provenance.String()
		if strings.Contains(rendered, "@") {
			t.Errorf("upstream %q produced a dangling separator:\n%s", upstream, rendered)
		}
		if !strings.Contains(rendered, "abc123") {
			t.Errorf("upstream %q lost the commit entirely:\n%s", upstream, rendered)
		}
	}
}

func TestVersionSaysWhenItIsALocalBuild(t *testing.T) {
	// An unstamped build's version number identifies nothing, and a reader who treats "dev" as a
	// release would go looking for rules that were never published.
	local := Provenance{Version: "dev", CompilerCommit: "unknown", GoToolchain: "go1.27.0", Platform: "darwin/arm64"}
	if !local.IsDevelopment() {
		t.Fatalf("a dev build did not identify as one")
	}
	if !strings.Contains(local.String(), "local build") {
		t.Errorf("a dev build does not say so:\n%s", local.String())
	}

	released := Provenance{Version: "1.2.3", CompilerCommit: "abc123", GoToolchain: "go1.27.0", Platform: "darwin/arm64"}
	if released.IsDevelopment() {
		t.Fatalf("a released build identified as development")
	}
	if strings.Contains(released.String(), "local build") {
		t.Errorf("a released build claims to be local:\n%s", released.String())
	}
}

func TestCompilerUpstreamNormalizesBothUrlShapes(t *testing.T) {
	// git accepts ssh and https remotes, and a build machine's choice of clone should not change
	// what a release reports. Otherwise two reports of the same pin look different, and a reader
	// comparing them sees a discrepancy that is not one.
	cases := map[string]string{
		"git@github.com:microsoft/TypeScript.git":     "microsoft/TypeScript",
		"https://github.com/microsoft/TypeScript.git": "microsoft/TypeScript",
		"https://github.com/microsoft/TypeScript":     "microsoft/TypeScript",
		"git@github.com:microsoft/typescript-go.git":  "microsoft/typescript-go",
	}
	for remote, expected := range cases {
		if got := normalizeUpstream(remote); got != expected {
			t.Errorf("%s normalized to %q, wanted %q", remote, got, expected)
		}
	}

	// Anything that is not a recognizable owner/name pair degrades rather than inventing a label.
	for _, unusable := range []string{"", "   ", "not-a-url"} {
		if got := normalizeUpstream(unusable); got != "unknown" {
			t.Errorf("%q normalized to %q rather than unknown", unusable, got)
		}
	}
}
