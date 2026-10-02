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

// TestResolveFollowsTheExtendsChain pins how tiers combine: each file's block applies over the one it
// extends, the outermost base first, so the nearest file to write a key wins and a key nobody nearer
// writes is inherited. Every key here is written at a different depth, so reading the chain in the
// wrong order, or reading only one end of it, changes at least one of them.
//
// The project's `bracketSameLine: false` and `tabWidth: 2` are Prettier's own defaults written over a
// base's other value: a merge that skipped a key equal to the default, or let true win over false,
// would keep the base's.
func TestResolveFollowsTheExtendsChain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tiers", "nexus.json"), `{"format": {"tabWidth": 8, "printWidth": 100, "singleQuote": true, "bracketSameLine": true}}`)
	writeFile(t, filepath.Join(root, "tiers", "structure.json"), `{"extends": "./nexus.json", "format": {"printWidth": 120, "arrowParens": "avoid"}}`)
	writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/structure.json", "format": {"tabWidth": 2, "bracketSameLine": false}}`)

	resolution, err := Resolve(filepath.Join(root, "source"))
	if err != nil {
		t.Fatal(err)
	}
	want := PrettierDefaults()
	want.TabWidth = 2            // the project over nexus's 8
	want.PrintWidth = 120        // structure over nexus's 100
	want.SingleQuote = true      // nexus alone
	want.BracketSameLine = false // the project over nexus's true
	want.ArrowParens = "avoid"   // structure alone
	if resolution.Options != want {
		t.Fatalf("options %+v, want %+v", resolution.Options, want)
	}
	if resolution.Source != filepath.Join(root, SettingsFileName) {
		t.Fatalf("resolved from %q, want the project's %s", resolution.Source, SettingsFileName)
	}
}

// TestAProjectWithoutAFormatBlockInheritsItsBases: a project that states no format of its own formats
// with the house's, which is the point of putting the house format in a tier.
func TestAProjectWithoutAFormatBlockInheritsItsBases(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tiers", "nexus.json"), `{"format": {"tabWidth": 4, "printWidth": 120}}`)
	writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/nexus.json", "rules": {}}`)

	resolution, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Options.TabWidth != 4 || resolution.Options.PrintWidth != 120 {
		t.Fatalf("options %+v, want the base's tab width 4 and print width 120", resolution.Options)
	}
}

// TestAChainRefusesWhatOneFileWouldBeRefusedFor: no block anywhere in the chain is the old "settings
// without a format" refusal, and a bad key in a base is refused naming the base, which is the file to
// change.
func TestAChainRefusesWhatOneFileWouldBeRefusedFor(t *testing.T) {
	t.Run("no format block in any file", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "tiers", "nexus.json"), `{"rules": {}}`)
		writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/nexus.json", "rules": {}}`)
		_, err := Resolve(root)
		if err == nil || !strings.Contains(err.Error(), "neither does any file it extends") {
			t.Fatalf("refusal %v, want one saying no file in the chain has a format block", err)
		}
	})

	t.Run("an unknown option in a base", func(t *testing.T) {
		root := t.TempDir()
		base := filepath.Join(root, "tiers", "nexus.json")
		writeFile(t, base, `{"format": {"quoteProps": "consistent"}}`)
		writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/nexus.json", "format": {}}`)
		_, err := Resolve(root)
		if err == nil || !strings.Contains(err.Error(), base) {
			t.Fatalf("refusal %v does not name the base %s", err, base)
		}
	})

	t.Run("a base that is missing", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/nexus.json", "format": {}}`)
		if resolution, err := Resolve(root); err == nil {
			t.Fatalf("a chain naming a missing base resolved to %+v", resolution.Options)
		}
	})
}

// TestALeftoverIsComparedWithTheWholeChain: old Prettier config agrees or disagrees with what the chain
// resolves to, not with the project's own block alone, which here says nothing about print width.
func TestALeftoverIsComparedWithTheWholeChain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tiers", "nexus.json"), `{"format": {"printWidth": 120}}`)
	writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/nexus.json", "format": {"tabWidth": 4}}`)

	writeFile(t, filepath.Join(root, "package.json"), `{"name": "x", "prettier": {"tabWidth": 4, "printWidth": 120}}`)
	if _, err := Resolve(root); err != nil {
		t.Fatalf("a leftover agreeing with the chain was refused: %v", err)
	}

	writeFile(t, filepath.Join(root, "package.json"), `{"name": "x", "prettier": {"tabWidth": 4}}`)
	if _, err := Resolve(root); !errors.Is(err, ErrPrettierConfigRemains) {
		t.Fatalf("a leftover missing the inherited print width was not refused as disagreeing: %v", err)
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
