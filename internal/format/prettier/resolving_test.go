package prettier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(test *testing.T, path string, contents string) {
	test.Helper()
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}

// TestResolvingFormatsEachFileWithItsOwnConfig is the defect the format phase had, run through the
// replacement: one formatter, two repositories, two configs, and each file gets its own.
//
// The JSX is long enough to break, so bracketSameLine decides where `>` lands. If Resolving applied one
// config to both, the two outputs would put it in the same place.
func TestResolvingFormatsEachFileWithItsOwnConfig(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	apart := filepath.Join(root, "apart")
	together := filepath.Join(root, "together")
	// Each repository's options live in its own Nexus tier, the one file a format block may live in.
	writeFile(
		test,
		filepath.Join(apart, "NexusCohereSettings.json"),
		`{"format": {"printWidth": 60, "tabWidth": 4, "singleQuote": true}}`,
	)
	writeFile(
		test,
		filepath.Join(together, "NexusCohereSettings.json"),
		`{"format": {"printWidth": 60, "tabWidth": 4, "singleQuote": true, "bracketSameLine": true}}`,
	)
	writeFile(test, filepath.Join(apart, "CohereSettings.json"), `{"extends": "./NexusCohereSettings.json"}`)
	writeFile(test, filepath.Join(together, "CohereSettings.json"), `{"extends": "./NexusCohereSettings.json"}`)

	resolving, newResolvingError := NewResolving(apart)
	if newResolvingError != nil {
		test.Fatal(newResolvingError)
	}
	source := "const element = <Component firstAttribute=\"a long value here\" secondAttribute=\"another long value\" third=\"x\">child</Component>;\n"

	formattedApart, formatError := resolving.Format(filepath.Join(apart, "source", "Probe.tsx"), source)
	if formatError != nil {
		test.Fatal(formatError)
	}
	formattedTogether, formatError := resolving.Format(filepath.Join(together, "source", "Probe.tsx"), source)
	if formatError != nil {
		test.Fatal(formatError)
	}

	// JSX attributes keep double quotes under singleQuote (that is jsxSingleQuote's job), so the probe
	// is `"x">`. An earlier draft looked for `'x'>`, which no output contains, so the first check passed
	// vacuously and only the second one's failure exposed it.
	if strings.Contains(formattedApart, `"x">`) {
		test.Errorf("the repository without bracketSameLine put `>` on the attribute line:\n%s", formattedApart)
	}
	if !strings.Contains(formattedTogether, `"x">`) {
		test.Errorf("the repository with bracketSameLine did not put `>` on the attribute line:\n%s", formattedTogether)
	}
	if len(resolving.engines) != 2 {
		test.Errorf("built %d engines for two distinct configs, want 2", len(resolving.engines))
	}
}

// TestResolvingRefusesAtConstructionNotMidRun pins why the first resolution is eager.
func TestResolvingRefusesAtConstructionNotMidRun(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFile(test, filepath.Join(root, "prettier.config.js"), "module.exports = {}")
	if _, newResolvingError := NewResolving(root); newResolvingError == nil {
		test.Fatal("a directory with a config cohere cannot read built a formatter, so the refusal would land mid-run")
	}
}
