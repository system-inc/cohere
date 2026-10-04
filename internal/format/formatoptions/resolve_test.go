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

// houseBlock is the house format as nexus 09b88e6 states it.
const houseBlock = `{"tabWidth": 4, "useTabs": false, "semi": true, "singleQuote": true, "printWidth": 120}`

// project writes a repository whose settings extend a Nexus tier carrying block, the shape every one of
// ours has: the project's own file states rules and no format.
func project(t *testing.T, root string, block string) {
	t.Helper()
	writeFile(t, filepath.Join(root, NexusTierFileName), `{"format": `+block+`}`)
	writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./`+NexusTierFileName+`", "rules": {}}`)
}

// TestResolveFindsTheNearestSettingsAbove pins inheritance: a nested library with no settings of its
// own formats with its containing repository's options, as libraries/structure does inside ahra.
func TestResolveFindsTheNearestSettingsAbove(t *testing.T) {
	root := t.TempDir()
	project(t, root, `{"tabWidth": 4, "printWidth": 120, "singleQuote": true, "bracketSameLine": true}`)
	writeFile(t, filepath.Join(root, "libraries", "structure", "package.json"), `{"name": "structure"}`)

	resolution, err := Resolve(filepath.Join(root, "libraries", "structure", "source"))
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Source != filepath.Join(root, SettingsFileName) {
		t.Fatalf("resolved from %q, want the root %s", resolution.Source, SettingsFileName)
	}
	if resolution.Options.TabWidth != 4 || resolution.Options.PrintWidth != 120 || !resolution.Options.SingleQuote || !resolution.Options.BracketSameLine {
		t.Fatalf("options %+v did not take the Nexus tier's format block", resolution.Options)
	}
}

// TestTheHouseBlockResolvesThroughTheTiers is every repository's real chain: the project extends a
// Structure (or Base) tier, which extends the Nexus tier, and only the Nexus tier holds the block.
func TestTheHouseBlockResolvesThroughTheTiers(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "nexus", NexusTierFileName), `{"rules": {}, "format": `+houseBlock+`}`)
	writeFile(t, filepath.Join(root, "structure", "StructureCohereSettings.json"), `{"extends": "../nexus/`+NexusTierFileName+`", "rules": {}}`)
	writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./structure/StructureCohereSettings.json", "rules": {}}`)

	resolution, err := Resolve(filepath.Join(root, "source"))
	if err != nil {
		t.Fatal(err)
	}
	want := PrettierDefaults()
	want.TabWidth = 4
	want.SingleQuote = true
	want.PrintWidth = 120
	if resolution.Options != want {
		t.Fatalf("options %+v, want the house block %+v", resolution.Options, want)
	}
	if resolution.Source != filepath.Join(root, SettingsFileName) {
		t.Fatalf("resolved from %q, want the project's %s", resolution.Source, SettingsFileName)
	}
}

// TestAFormatBlockOutsideTheNexusTierIsRefused: formatting is unified, so a format key anywhere but
// the Nexus tier is a second place the house format could drift, refused naming the file to change,
// whether it restates the house block or differs from it.
func TestAFormatBlockOutsideTheNexusTierIsRefused(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		structure string
		project   string
		refused   string
	}{
		{"the project's own file", `{"extends": "../nexus/` + NexusTierFileName + `"}`, `{"extends": "./structure/StructureCohereSettings.json", "format": ` + houseBlock + `}`, SettingsFileName},
		{"a project block that differs", `{"extends": "../nexus/` + NexusTierFileName + `"}`, `{"extends": "./structure/StructureCohereSettings.json", "format": {"printWidth": 80}}`, SettingsFileName},
		{"a Structure or Base tier", `{"extends": "../nexus/` + NexusTierFileName + `", "format": {"printWidth": 100}}`, `{"extends": "./structure/StructureCohereSettings.json"}`, filepath.Join("structure", "StructureCohereSettings.json")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "nexus", NexusTierFileName), `{"format": `+houseBlock+`}`)
			writeFile(t, filepath.Join(root, "structure", "StructureCohereSettings.json"), testCase.structure)
			writeFile(t, filepath.Join(root, SettingsFileName), testCase.project)

			resolution, err := Resolve(root)
			if err == nil {
				t.Fatalf("a format block in %s resolved to %+v", testCase.refused, resolution.Options)
			}
			if !strings.Contains(err.Error(), filepath.Join(root, testCase.refused)) || !strings.Contains(err.Error(), "only the Nexus tier") {
				t.Fatalf("refusal %v does not name %s as the file holding a format block outside the Nexus tier", err, testCase.refused)
			}
		})
	}
}

// TestAChainThatDoesNotSayHowToFormatIsRefused: a Nexus tier without the block, and a chain with no
// Nexus tier at all, each refused rather than formatted with Prettier's defaults.
func TestAChainThatDoesNotSayHowToFormatIsRefused(t *testing.T) {
	t.Run("a Nexus tier without the block", func(t *testing.T) {
		root := t.TempDir()
		nexusTier := filepath.Join(root, "nexus", NexusTierFileName)
		writeFile(t, nexusTier, `{"rules": {}}`)
		writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./nexus/`+NexusTierFileName+`"}`)
		_, err := Resolve(root)
		if err == nil || !strings.Contains(err.Error(), nexusTier) || !strings.Contains(err.Error(), "has no \"format\" block") {
			t.Fatalf("refusal %v does not name the Nexus tier %s as missing the block", err, nexusTier)
		}
	})

	t.Run("no Nexus tier in the chain", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "structure", "StructureCohereSettings.json"), `{"rules": {}}`)
		writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./structure/StructureCohereSettings.json"}`)
		_, err := Resolve(root)
		if err == nil || !strings.Contains(err.Error(), "does not extend the Nexus tier") {
			t.Fatalf("refusal %v, want one saying the chain has no Nexus tier", err)
		}
	})

	t.Run("settings extending nothing", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, SettingsFileName), `{"rules": {}}`)
		_, err := Resolve(root)
		if err == nil || !strings.Contains(err.Error(), "does not extend the Nexus tier") {
			t.Fatalf("refusal %v, want one saying the chain has no Nexus tier", err)
		}
	})
}

// TestAChainRefusesWhatOneFileWouldBeRefusedFor: a bad key in the Nexus tier is refused naming the
// tier, which is the file to change, and a missing base is refused.
func TestAChainRefusesWhatOneFileWouldBeRefusedFor(t *testing.T) {
	t.Run("an unknown option in the Nexus tier", func(t *testing.T) {
		root := t.TempDir()
		nexusTier := filepath.Join(root, "tiers", NexusTierFileName)
		writeFile(t, nexusTier, `{"format": {"quoteProps": "consistent"}}`)
		writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/`+NexusTierFileName+`"}`)
		_, err := Resolve(root)
		if err == nil || !strings.Contains(err.Error(), nexusTier) {
			t.Fatalf("refusal %v does not name the Nexus tier %s", err, nexusTier)
		}
	})

	t.Run("a base that is missing", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/`+NexusTierFileName+`"}`)
		if resolution, err := Resolve(root); err == nil {
			t.Fatalf("a chain naming a missing base resolved to %+v", resolution.Options)
		}
	})
}

// TestALeftoverIsRefusedEvenWhenItAgrees: old Prettier config is refused whatever it says. Agreeing
// with the Nexus tier today is no reason to keep a second statement of the options that nothing reads,
// since nothing would notice the day it stopped agreeing. ahra's move was the last one the tolerance
// served (#dv5ng7g).
func TestALeftoverIsRefusedEvenWhenItAgrees(t *testing.T) {
	for name, leftover := range map[string]struct{ file, contents string }{
		"package.json, the house options":        {"package.json", `{"name": "x", "prettier": {"tabWidth": 4, "singleQuote": true, "printWidth": 120}}`},
		"package.json, with the Tailwind plugin": {"package.json", `{"name": "x", "prettier": {"plugins": ["prettier-plugin-tailwindcss"], "tabWidth": 4, "singleQuote": true, "printWidth": 120, "tailwindFunctions": ["mergeClassNames"]}}`},
		".prettierrc, the house options":         {".prettierrc", `{"tabWidth": 4, "singleQuote": true, "printWidth": 120}`},
		"package.json, an empty prettier key":    {"package.json", `{"name": "x", "prettier": {}}`},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			project(t, root, `{"tabWidth": 4, "singleQuote": true, "printWidth": 120}`)
			writeFile(t, filepath.Join(root, leftover.file), leftover.contents)
			resolution, err := Resolve(root)
			if !errors.Is(err, ErrPrettierConfigRemains) {
				t.Fatalf("a leftover %s resolved to %+v, %v; want it refused", leftover.file, resolution.Options, err)
			}
			if !strings.Contains(err.Error(), filepath.Join(root, leftover.file)) || !strings.Contains(err.Error(), "delete it") {
				t.Fatalf("refusal %v does not name %s and say to delete it", err, leftover.file)
			}
		})
	}
}

// TestALeftoverBelowTheSettingsIsRefused: the walk up refuses old config in every directory it passes,
// not only beside the settings, so a package in a workspace cannot keep its own.
func TestALeftoverBelowTheSettingsIsRefused(t *testing.T) {
	root := t.TempDir()
	project(t, root, houseBlock)
	writeFile(t, filepath.Join(root, "packages", "inner", "package.json"), `{"name": "inner", "prettier": {"printWidth": 120}}`)
	if _, err := Resolve(filepath.Join(root, "packages", "inner", "source")); !errors.Is(err, ErrPrettierConfigRemains) {
		t.Fatalf("a leftover below the settings was not refused: %v", err)
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
	extendsNexus := `{"extends": "./` + NexusTierFileName + `"}`
	for name, testCase := range map[string]struct {
		files    map[string]string
		leftover bool
	}{
		"unknown option":             {files: map[string]string{SettingsFileName: extendsNexus, NexusTierFileName: `{"format": {"quoteProps": "consistent"}}`}},
		"plugin key in the block":    {files: map[string]string{SettingsFileName: extendsNexus, NexusTierFileName: `{"format": {"plugins": ["prettier-plugin-tailwindcss"]}}`}},
		"crlf":                       {files: map[string]string{SettingsFileName: extendsNexus, NexusTierFileName: `{"format": {"endOfLine": "crlf"}}`}},
		"settings without a format":  {files: map[string]string{SettingsFileName: extendsNexus, NexusTierFileName: `{"rules": {}}`}},
		"package.json":               {files: map[string]string{SettingsFileName: extendsNexus, NexusTierFileName: `{"format": {"tabWidth": 4}}`, "package.json": `{"prettier": {"tabWidth": 2}}`}, leftover: true},
		".prettierrc":                {files: map[string]string{SettingsFileName: extendsNexus, NexusTierFileName: `{"format": {"tabWidth": 4}}`, ".prettierrc": `{"printWidth": 100, "tabWidth": 4}`}, leftover: true},
		"old config and no settings": {files: map[string]string{"package.json": `{"prettier": {"tabWidth": 4}}`}, leftover: true},
		"javascript config":          {files: map[string]string{SettingsFileName: extendsNexus, NexusTierFileName: `{"format": {}}`, "prettier.config.js": `module.exports = {}`}, leftover: true},
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
	project(t, root, `{"tabWidth": 4}`)
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
	project(t, root, `{"printWidth": 120}`)
	writeFile(t, filepath.Join(root, "package.json"), `{"name": "x", "devDependencies": {}}`)
	resolution, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Options.PrintWidth != 120 {
		t.Fatalf("print width %d, want the Nexus tier's 120", resolution.Options.PrintWidth)
	}
}

// TestTheHouseIgnoreListRidesInTheFormatBlock: the block's `ignore` is the house list, not a printing
// option. It is read beside the options, declared only when written, and a value that is not a list of
// patterns is refused naming the Nexus tier.
func TestTheHouseIgnoreListRidesInTheFormatBlock(t *testing.T) {
	declared := t.TempDir()
	project(t, declared, `{"tabWidth": 4, "ignore": ["pnpm-lock.yaml", "*.sqlite"]}`)
	resolution, err := Resolve(declared)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.HouseIgnoreDeclared || strings.Join(resolution.HouseIgnore, ",") != "pnpm-lock.yaml,*.sqlite" || resolution.Options.TabWidth != 4 {
		t.Fatalf("resolved %+v, want the house list beside tab width 4", resolution)
	}

	undeclared := t.TempDir()
	project(t, undeclared, `{"tabWidth": 4}`)
	if resolution, err := Resolve(undeclared); err != nil || resolution.HouseIgnoreDeclared {
		t.Fatalf("a block without the key declared a house list: %+v, %v", resolution, err)
	}

	malformed := t.TempDir()
	project(t, malformed, `{"tabWidth": 4, "ignore": "pnpm-lock.yaml"}`)
	if _, err := Resolve(malformed); err == nil || !strings.Contains(err.Error(), NexusTierFileName) {
		t.Fatalf("an ignore that is not a list was accepted or not named: %v", err)
	}

}

// TestIgnorePatternsResolveWithTheOptions: the walk takes the chain's ignorePatterns from the same
// resolution as its options, base first, so it reads the list lint reads.
func TestIgnorePatternsResolveWithTheOptions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "nexus", NexusTierFileName), `{"rules": {}, "ignorePatterns": ["**/generated/**"], "format": `+houseBlock+`}`)
	writeFile(t, filepath.Join(root, SettingsFileName), `{"extends": "./nexus/`+NexusTierFileName+`", "rules": {}, "ignorePatterns": ["data/**"]}`)
	resolution, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(resolution.IgnorePatterns, ",") != "**/generated/**,data/**" {
		t.Fatalf("ignorePatterns %v, want the base's first", resolution.IgnorePatterns)
	}
}

// TestADroppedKeyCanNeverReachPrettiersDefaults: the move's failure is a repository whose prettier key
// went and whose options never arrived, formatting at width 80 with nothing said. Both ways it could
// happen are refusals: settings that do not say how to format, and old config with no settings above it.
func TestADroppedKeyCanNeverReachPrettiersDefaults(t *testing.T) {
	withoutBlock := t.TempDir()
	writeFile(t, filepath.Join(withoutBlock, SettingsFileName), `{"rules": {"no-debugger": "error"}}`)
	writeFile(t, filepath.Join(withoutBlock, "package.json"), `{"name": "x"}`)
	if resolution, err := Resolve(withoutBlock); err == nil {
		t.Fatalf("settings that do not say how to format resolved to %+v", resolution.Options)
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
