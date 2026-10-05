package release

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The expression is spelled out here rather than read from LicenseExpression, so a change to the constant
// fails rather than passing on whatever it became. A lowercase or reads to npm and every license scanner as
// one unknown license instead of a choice of two, and message tooling lowercased it once already.
const expectedLicenseExpression = "MIT OR Apache-2.0"

func TestEveryPackageDeclaresTheDualLicense(t *testing.T) {
	t.Parallel()

	if LicenseExpression != expectedLicenseExpression {
		t.Fatalf("the license expression is %q, wanted exactly %q", LicenseExpression, expectedLicenseExpression)
	}

	manifests := map[string]map[string]any{
		DispatcherPackageName: decodeManifest(t, mustDispatcherManifest(t, "1.2.3")),
	}
	for _, target := range Targets {
		manifests[target.PackageName()] = decodeManifest(t, mustPlatformManifest(t, target, "1.2.3"))
	}
	for name, manifest := range manifests {
		if manifest["license"] != expectedLicenseExpression {
			t.Errorf("%s declares license %v, wanted exactly %q", name, manifest["license"], expectedLicenseExpression)
		}
		// Listed in files, or npm publish drops the texts the license field promises.
		files, _ := manifest["files"].([]any)
		for _, license := range LicenseFileNames {
			if !slices.Contains(files, any(license)) {
				t.Errorf("%s's files are %v, so npm publish would leave %s out", name, files, license)
			}
		}
	}

	// The VS Code extension's manifest is a file in the repository rather than generated, so it is read.
	contents, err := os.ReadFile(filepath.Join(moduleRoot(t), "editors", "vscode", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var extension struct {
		License string `json:"license"`
	}
	if err := json.Unmarshal(contents, &extension); err != nil {
		t.Fatal(err)
	}
	if extension.License != expectedLicenseExpression {
		t.Errorf("the VS Code extension declares license %q, wanted exactly %q", extension.License, expectedLicenseExpression)
	}
}

func TestPackagesShipBothLicenseTextsAsCommitted(t *testing.T) {
	t.Parallel()

	module := moduleRoot(t)
	output := t.TempDir()
	dispatcher, err := buildDispatcherPackage(Options{ModuleDirectory: module, OutputDirectory: output, Version: "1.0.0"}, placeholderChecksums)
	if err != nil {
		t.Fatal(err)
	}
	platform := filepath.Join(output, "cohere-linux-x64")
	if err := os.MkdirAll(platform, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := stageLicenses(module, platform); err != nil {
		t.Fatal(err)
	}

	for _, directory := range []string{dispatcher.Directory, platform} {
		for _, name := range LicenseFileNames {
			committed, err := os.ReadFile(filepath.Join(module, name))
			if err != nil {
				t.Fatal(err)
			}
			shipped, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatalf("%s does not ship %s: %v", directory, name, err)
			}
			if !bytes.Equal(shipped, committed) {
				t.Fatalf("%s ships a %s that differs from the committed one", directory, name)
			}
		}
	}

	empty := t.TempDir()
	for _, name := range LicenseFileNames {
		writeFile(t, filepath.Join(empty, name), "\n")
	}
	if err := stageLicenses(empty, t.TempDir()); err == nil {
		t.Fatalf("an empty license was staged, so a package would ship terms with nothing in them")
	}
}

func TestTheLicenseTextsAreTheStandardOnes(t *testing.T) {
	t.Parallel()

	module := moduleRoot(t)

	// The Apache text verbatim from apache.org/licenses/LICENSE-2.0.txt, whose SHA-256 this is. A hand edit
	// to a license is a change of terms, so any byte that moves fails here.
	apache, err := os.ReadFile(filepath.Join(module, "LICENSE-APACHE"))
	if err != nil {
		t.Fatal(err)
	}
	if got := sha256Hex(apache); got != "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30" {
		t.Errorf("LICENSE-APACHE hashes to %s, not apache.org's Apache License 2.0 text", got)
	}

	// MIT's text is the same everywhere but its copyright line, so the rest is compared against the
	// tsgolint license this repository already carries, which is the standard text.
	mit, err := os.ReadFile(filepath.Join(module, "LICENSE-MIT"))
	if err != nil {
		t.Fatal(err)
	}
	reference, err := os.ReadFile(filepath.Join(module, "internal", "lint", "checking", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if mitTerms(mit) != mitTerms(reference) || mitTerms(mit) == "" {
		t.Errorf("LICENSE-MIT's terms differ from the standard MIT text:\n%s", mit)
	}
	if !strings.HasPrefix(string(mit), "MIT License\n\nCopyright (c) 2026 ") {
		t.Errorf("LICENSE-MIT does not open with its title and a 2026 copyright line:\n%s", mit)
	}
}

func TestReleaseRefusesAPlaceholderCopyrightHolderUnlessItIsADryRun(t *testing.T) {
	t.Parallel()

	// The placeholder stays in the repository until Kirk confirms the legal name, and the release is what
	// refuses it, so a contributor's tests pass meanwhile and nothing can publish it. A dry run stages
	// with it, so the packaging is proven before the name lands, and publishes nothing.
	pending := t.TempDir()
	writeFile(t, filepath.Join(pending, "LICENSE-APACHE"), "terms\n")
	writeFile(t, filepath.Join(pending, "LICENSE-MIT"), "MIT License\n\nCopyright (c) 2026 "+copyrightHolderPlaceholder+": the legal name]]\n")
	if err := requireConfirmedCopyright(pending, false); err == nil || !strings.Contains(err.Error(), "LICENSE-MIT") {
		t.Fatalf("a placeholder copyright holder was not refused by name: %v", err)
	}
	if err := requireConfirmedCopyright(pending, true); err != nil {
		t.Fatalf("a dry run was refused the placeholder it is allowed: %v", err)
	}

	confirmed := t.TempDir()
	writeFile(t, filepath.Join(confirmed, "LICENSE-APACHE"), "terms\n")
	writeFile(t, filepath.Join(confirmed, "LICENSE-MIT"), "MIT License\n\nCopyright (c) 2026 Example, Inc.\n")
	if err := requireConfirmedCopyright(confirmed, false); err != nil {
		t.Fatalf("a named copyright holder was refused: %v", err)
	}

	// A dry run is excused the name, not the licenses.
	if err := requireConfirmedCopyright(t.TempDir(), true); err == nil {
		t.Fatalf("a dry run staged with no license at all")
	}

	// And Build reaches the check, before it reads the compiler pin or compiles anything: a publish is
	// refused there, and a dry run passes it and is refused later, by the pin this fixture lacks.
	_, err := Build(Options{ModuleDirectory: pending, OutputDirectory: t.TempDir(), Version: "1.0.0", SwiftScratchDirectory: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("Build did not refuse a placeholder copyright holder for a publish: %v", err)
	}
	_, err = Build(Options{ModuleDirectory: pending, OutputDirectory: t.TempDir(), Version: "1.0.0", SwiftScratchDirectory: t.TempDir(), DryRun: true})
	if err == nil || strings.Contains(err.Error(), "placeholder") || !strings.Contains(err.Error(), "compiler") {
		t.Fatalf("a dry run did not get past the copyright check to the compiler pin: %v", err)
	}
}

func TestTheWorkflowDryRunsExactlyWhenItDoesNotPublish(t *testing.T) {
	t.Parallel()

	// release.yml decides --dry-run in shell, twice: for the npm staging and for the extension. Each
	// decision is run here for both inputs, so a publish that would pass --dry-run, and slip the
	// placeholder past the release, fails here rather than on the registry.
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash runs the workflow's steps, and it is not installed: %v", err)
	}
	workflow, err := os.ReadFile(filepath.Join(moduleRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	decisions := dryRunDecisions(string(workflow))
	if len(decisions) != 2 {
		t.Fatalf("release.yml decides --dry-run %d times, wanted twice (staging and the extension)", len(decisions))
	}
	for index, decision := range decisions {
		for publish, wanted := range map[string]string{"true": "", "false": "--dry-run"} {
			script := strings.ReplaceAll(decision, "${{ inputs.publish }}", publish) + "\nprintf '%s' \"${dry_run[*]}\"\n"
			output, err := exec.Command(bash, "-c", script).CombinedOutput()
			if err != nil {
				t.Fatalf("decision %d with publish=%s did not run: %v\n%s", index, publish, err, output)
			}
			if string(output) != wanted {
				t.Errorf("decision %d with publish=%s passes %q, wanted %q", index, publish, output, wanted)
			}
		}
	}
}

// dryRunDecisions are release.yml's blocks that set dry_run, from `dry_run=()` to the `fi` closing them.
func dryRunDecisions(workflow string) []string {
	var decisions []string
	for {
		start := strings.Index(workflow, "dry_run=()")
		if start < 0 {
			return decisions
		}
		closing := strings.Index(workflow[start:], "fi\n")
		if closing < 0 {
			return decisions
		}
		decisions = append(decisions, dedent(workflow[start:start+closing+2]))
		workflow = workflow[start+closing+2:]
	}
}

// dedent strips each line's leading spaces, which YAML indents a run block by and bash does not need.
func dedent(block string) string {
	lines := strings.Split(block, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimLeft(line, " ")
	}
	return strings.Join(lines, "\n")
}

// mitTerms is an MIT license from its first paragraph on, past the title and the copyright line.
func mitTerms(license []byte) string {
	_, terms, found := strings.Cut(string(license), "Permission is hereby granted")
	if !found {
		return ""
	}
	return terms
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	module, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return module
}
