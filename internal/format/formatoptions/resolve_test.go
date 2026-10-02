package formatoptions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolveFindsTheNearestSettingsAbove pins inheritance: a nested library with no settings of its
// own formats with its containing repository's options, as libraries/structure does inside ahra.
func TestResolveFindsTheNearestSettingsAbove(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, SettingsFileName), `{"rules": {}, "format": {"tabWidth": 4, "printWidth": 120, "singleQuote": true, "bracketSameLine": true}}`)
	writeFile(t, filepath.Join(root, "libraries", "structure", "package.json"), `{"name": "structure"}`)

	resolution, err := Resolve(filepath.Join(root, "libraries", "structure", "source"))
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Source != filepath.Join(root, SettingsFileName) {
		t.Fatalf("resolved from %q, want the root %s", resolution.Source, SettingsFileName)
	}
	if resolution.Options.TabWidth != 4 || resolution.Options.PrintWidth != 120 || !resolution.Options.SingleQuote || !resolution.Options.BracketSameLine {
		t.Fatalf("options %+v did not take the format block", resolution.Options)
	}
}

// TestResolveWithNothingConfiguredIsPrettierNotAhra holds the distinction a fallback would erase.
func TestResolveWithNothingConfiguredIsPrettierNotAhra(t *testing.T) {
	resolution, err := Resolve(t.TempDir())
	if err != nil {
		t.Skipf("something above the temp directory configures formatting (%v), so this machine cannot test the empty case", err)
	}
	if resolution.Source != "" {
		t.Skipf("settings exist above the temp directory (%s), so this machine cannot test the empty case", resolution.Source)
	}
	if resolution.Options != PrettierDefaults() || resolution.Options.TabWidth != 2 || resolution.Options.PrintWidth != 80 {
		t.Fatalf("nothing configured resolved to %+v, want Prettier's own defaults", resolution.Options)
	}
}

// TestResolveRefusesEveryOptionNobodyChose covers each refusal, because each one replaces a format run
// with options nobody chose: Prettier's width 80 where the repository meant 120.
func TestResolveRefusesEveryOptionNobodyChose(t *testing.T) {
	for name, testCase := range map[string]struct {
		files    map[string]string
		leftover bool
	}{
		"unknown option":             {files: map[string]string{SettingsFileName: `{"format": {"quoteProps": "consistent"}}`}},
		"plugin key in the block":    {files: map[string]string{SettingsFileName: `{"format": {"plugins": ["prettier-plugin-tailwindcss"]}}`}},
		"crlf":                       {files: map[string]string{SettingsFileName: `{"format": {"endOfLine": "crlf"}}`}},
		"settings without a format":  {files: map[string]string{SettingsFileName: `{"rules": {}}`}},
		"disagreeing package.json":   {files: map[string]string{SettingsFileName: `{"format": {"tabWidth": 4}}`, "package.json": `{"prettier": {"tabWidth": 2}}`}, leftover: true},
		"disagreeing .prettierrc":    {files: map[string]string{SettingsFileName: `{"format": {"tabWidth": 4}}`, ".prettierrc": `{"printWidth": 100, "tabWidth": 4}`}, leftover: true},
		"old config and no settings": {files: map[string]string{"package.json": `{"prettier": {"tabWidth": 4}}`}, leftover: true},
		"javascript config":          {files: map[string]string{SettingsFileName: `{"format": {}}`, "prettier.config.js": `module.exports = {}`}, leftover: true},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for file, contents := range testCase.files {
				writeFile(t, filepath.Join(root, file), contents)
			}
			resolution, err := Resolve(root)
			if err == nil {
				t.Fatalf("resolved %+v from %s, want a refusal", resolution.Options, resolution.Source)
			}
			if errors.Is(err, ErrPrettierConfigRemains) != testCase.leftover {
				t.Fatalf("refusal %v: leftover-config sentinel %v, want %v", err, errors.Is(err, ErrPrettierConfigRemains), testCase.leftover)
			}
		})
	}
}

// TestALeftoverIsNamedWithItsFile: a refusal a reader can act on says which file to change.
func TestALeftoverIsNamedWithItsFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, SettingsFileName), `{"format": {"tabWidth": 4}}`)
	writeFile(t, filepath.Join(root, "package.json"), `{"name": "x", "prettier": {"tabWidth": 2}}`)
	_, err := Resolve(root)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(root, "package.json")) {
		t.Fatalf("refusal %v does not name the package.json carrying the old options", err)
	}
}

// TestAPackageJSONWithoutPrettierIsNotALeftover: almost every repository has a package.json, and only
// its "prettier" key is old config.
func TestAPackageJSONWithoutPrettierIsNotALeftover(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, SettingsFileName), `{"format": {"printWidth": 120}}`)
	writeFile(t, filepath.Join(root, "package.json"), `{"name": "x", "devDependencies": {}}`)
	resolution, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Options.PrintWidth != 120 {
		t.Fatalf("print width %d, want the format block's 120", resolution.Options.PrintWidth)
	}
}

// TestALeftoverThatAgreesIsTolerated: ahra keeps package.json's prettier block until its editor moves
// (#3w83j3k), with the same options as its format block plus the Tailwind plugin's keys. It keeps
// formatting, from the format block, and the old copy is only compared.
func TestALeftoverThatAgreesIsTolerated(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, SettingsFileName), `{"format": {"tabWidth": 4, "singleQuote": true, "printWidth": 120}}`)
	writeFile(t, filepath.Join(root, "package.json"), `{"name": "x", "prettier": {"plugins": ["prettier-plugin-tailwindcss"], "tabWidth": 4, "singleQuote": true, "printWidth": 120, "tailwindFunctions": ["mergeClassNames"]}}`)
	resolution, err := Resolve(root)
	if err != nil {
		t.Fatalf("an agreeing leftover was refused: %v", err)
	}
	if resolution.Source != filepath.Join(root, SettingsFileName) || resolution.Options.PrintWidth != 120 {
		t.Fatalf("resolved %+v from %s, want the format block's", resolution.Options, resolution.Source)
	}
}

// TestADroppedKeyCanNeverReachPrettiersDefaults: the move's failure is a repository whose prettier key
// went and whose options never arrived, formatting at width 80 with nothing said. Both ways it could
// happen are refusals: settings without a format block, and old config with no settings above it.
func TestADroppedKeyCanNeverReachPrettiersDefaults(t *testing.T) {
	withoutBlock := t.TempDir()
	writeFile(t, filepath.Join(withoutBlock, SettingsFileName), `{"rules": {"no-debugger": "error"}}`)
	writeFile(t, filepath.Join(withoutBlock, "package.json"), `{"name": "x"}`)
	if resolution, err := Resolve(withoutBlock); err == nil {
		t.Fatalf("settings with no format block resolved to %+v", resolution.Options)
	}

	neverMoved := t.TempDir()
	writeFile(t, filepath.Join(neverMoved, "package.json"), `{"name": "x", "prettier": {"tabWidth": 4, "printWidth": 120}}`)
	resolution, err := Resolve(neverMoved)
	if err == nil {
		t.Fatalf("old config with no settings resolved to %+v", resolution.Options)
	}
	if resolution.Options == PrettierDefaults() {
		t.Fatal("old config with no settings reached Prettier's defaults")
	}
}
