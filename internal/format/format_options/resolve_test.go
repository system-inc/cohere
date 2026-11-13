package format_options

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/needs"
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

// houseBlock is the house format as nexus 09b88e6 states it.
const houseBlock = `{"tabWidth": 4, "useTabs": false, "semi": true, "singleQuote": true, "printWidth": 120}`

// project writes a repository whose settings extend a Nexus tier carrying block, the shape every one of
// ours has: the project's own file states rules and no format.
func project(test *testing.T, root string, block string) {
	test.Helper()
	writeFile(test, filepath.Join(root, NexusTierFileName), `{"format": `+block+`}`)
	writeFile(test, filepath.Join(root, SettingsFileName), `{"extends": "./`+NexusTierFileName+`", "rules": {}}`)
}

// TestResolveFindsTheNearestSettingsAbove pins inheritance: a nested library with no settings of its
// own formats with its containing repository's options, as libraries/structure does inside ahra.
func TestResolveFindsTheNearestSettingsAbove(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	project(test, root, `{"tabWidth": 4, "printWidth": 120, "singleQuote": true, "bracketSameLine": true}`)
	writeFile(test, filepath.Join(root, "libraries", "structure", "package.json"), `{"name": "structure"}`)

	resolution, resolveError := Resolve(filepath.Join(root, "libraries", "structure", "source"))
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	if resolution.Source != filepath.Join(root, SettingsFileName) {
		test.Fatalf("resolved from %q, want the root %s", resolution.Source, SettingsFileName)
	}
	if resolution.Options.TabWidth != 4 ||
		resolution.Options.PrintWidth != 120 ||
		!resolution.Options.SingleQuote ||
		!resolution.Options.BracketSameLine {
		test.Fatalf("options %+v did not take the Nexus tier's format block", resolution.Options)
	}
}

// TestTheHouseBlockResolvesThroughTheTiers is every repository's real chain: the project extends a
// Structure (or Base) tier, which extends the Nexus tier, and only the Nexus tier holds the block.
func TestTheHouseBlockResolvesThroughTheTiers(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeFile(test, filepath.Join(root, "nexus", NexusTierFileName), `{"rules": {}, "format": `+houseBlock+`}`)
	writeFile(
		test,
		filepath.Join(root, "structure", "StructureCohereSettings.json"),
		`{"extends": "../nexus/`+NexusTierFileName+`", "rules": {}}`,
	)
	writeFile(
		test,
		filepath.Join(root, SettingsFileName),
		`{"extends": "./structure/StructureCohereSettings.json", "rules": {}}`,
	)

	resolution, resolveError := Resolve(filepath.Join(root, "source"))
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	want := PrettierDefaults()
	want.TabWidth = 4
	want.SingleQuote = true
	want.PrintWidth = 120
	if resolution.Options != want {
		test.Fatalf("options %+v, want the house block %+v", resolution.Options, want)
	}
	if resolution.Source != filepath.Join(root, SettingsFileName) {
		test.Fatalf("resolved from %q, want the project's %s", resolution.Source, SettingsFileName)
	}
}

// TestOurTiersRefuseAFormatBlockOutsideTheNexusTier: in a chain that extends a system-inc set,
// formatting is unified, so a format key anywhere but the Nexus tier is a second place the house format
// could drift. It is refused, naming the file to change, whether it restates the house block or differs,
// in the project's own file or in a base of it. The Nexus tier itself is the set cohere carries, which
// always has its block.
func TestOurTiersRefuseAFormatBlockOutsideTheNexusTier(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name    string
		files   map[string]string
		refused string
	}{
		{"the project restating the house block", map[string]string{
			SettingsFileName: `{"extends": "cohere:system-inc/structure", "format": ` + houseBlock + `}`,
		}, SettingsFileName},
		{
			"the project's own empty block",
			map[string]string{SettingsFileName: `{"extends": "cohere:system-inc/base", "format": {}}`},
			SettingsFileName,
		},
		{"a base between the project and the set", map[string]string{
			SettingsFileName:     `{"extends": "./shared/Shared.json"}`,
			"shared/Shared.json": `{"extends": "cohere:system-inc/structure", "format": {"printWidth": 100}}`,
		}, filepath.Join("shared", "Shared.json")},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			for file, contents := range testCase.files {
				writeFile(test, filepath.Join(root, file), contents)
			}
			resolution, resolveError := Resolve(root)
			if resolveError == nil {
				test.Fatalf("a format block in %s resolved to %+v", testCase.refused, resolution.Options)
			}
			if !strings.Contains(resolveError.Error(), filepath.Join(root, testCase.refused)) ||
				!strings.Contains(resolveError.Error(), "only the Nexus tier") {
				test.Fatalf(
					"refusal %v does not name %s as the file holding a format block outside the Nexus tier",
					resolveError,
					testCase.refused,
				)
			}
		})
	}

	root := test.TempDir()
	writeFile(test, filepath.Join(root, SettingsFileName), `{"extends": "cohere:system-inc/structure"}`)
	resolution, resolveError := Resolve(root)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	if resolution.Options.PrintWidth != 120 || resolution.Options.TabWidth != 4 || !resolution.Options.SingleQuote {
		test.Fatalf("our tier without a project block resolved to %+v, want the house block", resolution.Options)
	}
}

// TestZeroConfigIsTheHouseFormat: no settings anywhere, settings that extend nothing, and a chain of the
// project's own files that never says how to format all format the house way, with the house ignore
// list declared, as cohere:typescript carries them (#bfxz13m).
func TestZeroConfigIsTheHouseFormat(test *testing.T) {
	test.Parallel()
	house, houseResolutionError := houseResolution()
	if houseResolutionError != nil {
		test.Fatal(houseResolutionError)
	}
	if house.Options.TabWidth != 4 ||
		house.Options.PrintWidth != 120 ||
		!house.Options.SingleQuote ||
		!house.HouseIgnoreDeclared {
		test.Fatalf("the house resolution %+v is not cohere:typescript's block", house)
	}

	for name, files := range map[string]map[string]string{
		"settings extending nothing": {SettingsFileName: `{"rules": {}}`},
		"a chain of the project's own files": {
			SettingsFileName: `{"extends": "./base/Base.json"}`,
			"base/Base.json": `{"rules": {}}`,
		},
		"a cohere set that is not ours": {SettingsFileName: `{"extends": "cohere:react"}`},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			for file, contents := range files {
				writeFile(test, filepath.Join(root, file), contents)
			}
			resolution, resolveError := Resolve(root)
			if resolveError != nil {
				test.Fatal(resolveError)
			}
			if resolution.Options != house.Options ||
				strings.Join(resolution.HouseIgnore, ",") != strings.Join(house.HouseIgnore, ",") ||
				!resolution.HouseIgnoreDeclared {
				test.Fatalf("resolved %+v, want the house format and list", resolution)
			}
			if resolution.Source != filepath.Join(root, SettingsFileName) {
				test.Fatalf("resolved from %q, want the project's settings", resolution.Source)
			}
		})
	}
}

// TestAnOutsidersOwnBlockIsAppliedOverPrettiersDefaults: outside our tiers the project's own block
// decides, over Prettier's defaults rather than over the house, so `{}` is exactly Prettier's defaults.
// The most derived file with a block wins, and its `ignore` replaces the house list only when it has one.
func TestAnOutsidersOwnBlockIsAppliedOverPrettiersDefaults(test *testing.T) {
	test.Parallel()
	house, houseResolutionError := houseResolution()
	if houseResolutionError != nil {
		test.Fatal(houseResolutionError)
	}
	for _, testCase := range []struct {
		name   string
		files  map[string]string
		want   func(options *Options)
		ignore string
	}{
		{
			"an empty block",
			map[string]string{SettingsFileName: `{"extends": "cohere:typescript", "format": {}}`},
			func(*Options) {
				// An empty block changes nothing, so the want is Prettier's defaults as they are.
			},
			strings.Join(house.HouseIgnore, ","),
		},
		{
			"a block of its own",
			map[string]string{SettingsFileName: `{"format": {"printWidth": 100, "ignore": ["generated/"]}}`},
			func(options *Options) { options.PrintWidth = 100 },
			"generated/",
		},
		{"a base's block", map[string]string{
			SettingsFileName: `{"extends": "./base/Base.json"}`,
			"base/Base.json": `{"format": {"semi": false}}`,
		}, func(options *Options) { options.Semi = false }, strings.Join(house.HouseIgnore, ",")},
		{"the project's block over its base's", map[string]string{
			SettingsFileName: `{"extends": "./base/Base.json", "format": {"tabWidth": 8}}`,
			"base/Base.json": `{"format": {"semi": false}}`,
		}, func(options *Options) { options.TabWidth = 8 }, strings.Join(house.HouseIgnore, ",")},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			for file, contents := range testCase.files {
				writeFile(test, filepath.Join(root, file), contents)
			}
			resolution, resolveError := Resolve(root)
			if resolveError != nil {
				test.Fatal(resolveError)
			}
			want := PrettierDefaults()
			testCase.want(&want)
			if resolution.Options != want {
				test.Fatalf("options %+v, want %+v", resolution.Options, want)
			}
			if strings.Join(resolution.HouseIgnore, ",") != testCase.ignore || !resolution.HouseIgnoreDeclared {
				test.Fatalf("ignore %v, want %s", resolution.HouseIgnore, testCase.ignore)
			}
		})
	}

	test.Run("an unknown option in an outsider's block", func(test *testing.T) {
		test.Parallel()
		root := test.TempDir()
		settings := filepath.Join(root, SettingsFileName)
		writeFile(test, settings, `{"format": {"quoteProps": "consistent"}}`)
		if _, resolveError := Resolve(root); resolveError == nil || !strings.Contains(resolveError.Error(), settings) {
			test.Fatalf("refusal %v does not name %s", resolveError, settings)
		}
	})
}

// TestAChainRefusesWhatOneFileWouldBeRefusedFor: a bad key in the Nexus tier is refused naming the
// tier, which is the file to change, and a missing base is refused.
func TestAChainRefusesWhatOneFileWouldBeRefusedFor(test *testing.T) {
	test.Parallel()
	test.Run("an unknown option in the Nexus tier", func(test *testing.T) {
		test.Parallel()
		root := test.TempDir()
		nexusTier := filepath.Join(root, "tiers", NexusTierFileName)
		writeFile(test, nexusTier, `{"format": {"quoteProps": "consistent"}}`)
		writeFile(test, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/`+NexusTierFileName+`"}`)
		_, resolveError := Resolve(root)
		if resolveError == nil || !strings.Contains(resolveError.Error(), nexusTier) {
			test.Fatalf("refusal %v does not name the Nexus tier %s", resolveError, nexusTier)
		}
	})

	test.Run("a base that is missing", func(test *testing.T) {
		test.Parallel()
		root := test.TempDir()
		writeFile(test, filepath.Join(root, SettingsFileName), `{"extends": "./tiers/`+NexusTierFileName+`"}`)
		if resolution, resolveError := Resolve(root); resolveError == nil {
			test.Fatalf("a chain naming a missing base resolved to %+v", resolution.Options)
		}
	})
}

// TestALeftoverIsRefusedEvenWhenItAgrees: old Prettier config is refused whatever it says. Agreeing
// with the Nexus tier today is no reason to keep a second statement of the options that nothing reads,
// since nothing would notice the day it stopped agreeing. ahra's move was the last one the tolerance
// served (#dv5ng7g).
func TestALeftoverIsRefusedEvenWhenItAgrees(test *testing.T) {
	test.Parallel()
	for name, leftover := range map[string]struct{ file, contents string }{
		"package.json, the house options": {
			"package.json",
			`{"name": "x", "prettier": {"tabWidth": 4, "singleQuote": true, "printWidth": 120}}`,
		},
		"package.json, with the Tailwind plugin": {
			"package.json",
			`{"name": "x", "prettier": {"plugins": ["prettier-plugin-tailwindcss"], "tabWidth": 4, "singleQuote": true, "printWidth": 120, "tailwindFunctions": ["mergeClassNames"]}}`,
		},
		".prettierrc, the house options": {
			".prettierrc",
			`{"tabWidth": 4, "singleQuote": true, "printWidth": 120}`,
		},
		"package.json, an empty prettier key": {"package.json", `{"name": "x", "prettier": {}}`},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			project(test, root, `{"tabWidth": 4, "singleQuote": true, "printWidth": 120}`)
			writeFile(test, filepath.Join(root, leftover.file), leftover.contents)
			resolution, resolveError := Resolve(root)
			if !errors.Is(resolveError, ErrPrettierConfigRemains) {
				test.Fatalf(
					"a leftover %s resolved to %+v, %v; want it refused",
					leftover.file,
					resolution.Options,
					resolveError,
				)
			}
			if !strings.Contains(resolveError.Error(), filepath.Join(root, leftover.file)) ||
				!strings.Contains(resolveError.Error(), "delete it") {
				test.Fatalf("refusal %v does not name %s and say to delete it", resolveError, leftover.file)
			}
		})
	}
}

// TestALeftoverBelowTheSettingsIsRefused: the walk up refuses old config in every directory it passes,
// not only beside the settings, so a package in a workspace cannot keep its own.
func TestALeftoverBelowTheSettingsIsRefused(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	project(test, root, houseBlock)
	writeFile(
		test,
		filepath.Join(root, "packages", "inner", "package.json"),
		`{"name": "inner", "prettier": {"printWidth": 120}}`,
	)
	if _, resolveError := Resolve(
		filepath.Join(root, "packages", "inner", "source"),
	); !errors.Is(resolveError, ErrPrettierConfigRemains) {
		test.Fatalf("a leftover below the settings was not refused: %v", resolveError)
	}
}

// TestNothingConfiguredIsTheHouseNotPrettier: a tree with no settings anywhere above it is zero config,
// which formats the house way, not with Prettier's defaults.
func TestNothingConfiguredIsTheHouseNotPrettier(test *testing.T) {
	test.Parallel()
	resolution, resolveError := Resolve(test.TempDir())
	if resolveError != nil {
		test.Skipf(
			"something above the temp directory configures formatting (%v), so this machine cannot test the empty case",
			resolveError,
		)
	}
	if resolution.Source != "" {
		test.Skipf(
			"settings exist above the temp directory (%s), so this machine cannot test the empty case",
			resolution.Source,
		)
	}
	if resolution.Options.TabWidth != 4 || resolution.Options.PrintWidth != 120 || !resolution.HouseIgnoreDeclared {
		test.Fatalf("nothing configured resolved to %+v, want the house format and list", resolution)
	}
}

// TestResolveRefusesEveryOptionNobodyChose covers each refusal, because each one replaces a format run
// with options nobody chose: Prettier's width 80 where the repository meant 120.
func TestResolveRefusesEveryOptionNobodyChose(test *testing.T) {
	test.Parallel()
	extendsNexus := `{"extends": "./` + NexusTierFileName + `"}`
	for name, testCase := range map[string]struct {
		files    map[string]string
		leftover bool
	}{
		"unknown option": {files: map[string]string{
			SettingsFileName:  extendsNexus,
			NexusTierFileName: `{"format": {"quoteProps": "consistent"}}`,
		}},
		"plugin key in the block": {files: map[string]string{
			SettingsFileName:  extendsNexus,
			NexusTierFileName: `{"format": {"plugins": ["prettier-plugin-tailwindcss"]}}`,
		}},
		"crlf": {files: map[string]string{
			SettingsFileName:  extendsNexus,
			NexusTierFileName: `{"format": {"endOfLine": "crlf"}}`,
		}},
		"package.json": {files: map[string]string{
			SettingsFileName:  extendsNexus,
			NexusTierFileName: `{"format": {"tabWidth": 4}}`,
			"package.json":    `{"prettier": {"tabWidth": 2}}`,
		}, leftover: true},
		".prettierrc": {files: map[string]string{
			SettingsFileName:  extendsNexus,
			NexusTierFileName: `{"format": {"tabWidth": 4}}`,
			".prettierrc":     `{"printWidth": 100, "tabWidth": 4}`,
		}, leftover: true},
		"old config and no settings": {
			files:    map[string]string{"package.json": `{"prettier": {"tabWidth": 4}}`},
			leftover: true,
		},
		"javascript config": {files: map[string]string{
			SettingsFileName:     extendsNexus,
			NexusTierFileName:    `{"format": {}}`,
			"prettier.config.js": `module.exports = {}`,
		}, leftover: true},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			for file, contents := range testCase.files {
				writeFile(test, filepath.Join(root, file), contents)
			}
			resolution, resolveError := Resolve(root)
			if resolveError == nil {
				test.Fatalf("resolved %+v from %s, want a refusal", resolution.Options, resolution.Source)
			}
			if errors.Is(resolveError, ErrPrettierConfigRemains) != testCase.leftover {
				test.Fatalf(
					"refusal %v: leftover-config sentinel %v, want %v",
					resolveError,
					errors.Is(resolveError, ErrPrettierConfigRemains),
					testCase.leftover,
				)
			}
		})
	}
}

// TestALeftoverIsNamedWithItsFile: a refusal a reader can act on says which file to change.
func TestALeftoverIsNamedWithItsFile(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	project(test, root, `{"tabWidth": 4}`)
	writeFile(test, filepath.Join(root, "package.json"), `{"name": "x", "prettier": {"tabWidth": 2}}`)
	_, resolveError := Resolve(root)
	if resolveError == nil || !strings.Contains(resolveError.Error(), filepath.Join(root, "package.json")) {
		test.Fatalf("refusal %v does not name the package.json carrying the old options", resolveError)
	}
}

// TestAPackageJsonWithoutPrettierIsNotALeftover: almost every repository has a package.json, and only
// its "prettier" key is old config.
func TestAPackageJsonWithoutPrettierIsNotALeftover(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	project(test, root, `{"printWidth": 120}`)
	writeFile(test, filepath.Join(root, "package.json"), `{"name": "x", "devDependencies": {}}`)
	resolution, resolveError := Resolve(root)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	if resolution.Options.PrintWidth != 120 {
		test.Fatalf("print width %d, want the Nexus tier's 120", resolution.Options.PrintWidth)
	}
}

// TestTheHouseIgnoreListRidesInTheFormatBlock: the block's `ignore` is the house list, not a printing
// option. It is read beside the options, declared only when written, and a value that is not a list of
// patterns is refused naming the Nexus tier.
func TestTheHouseIgnoreListRidesInTheFormatBlock(test *testing.T) {
	test.Parallel()
	declared := test.TempDir()
	project(test, declared, `{"tabWidth": 4, "ignore": ["pnpm-lock.yaml", "*.sqlite"]}`)
	resolution, resolveError := Resolve(declared)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	if !resolution.HouseIgnoreDeclared ||
		strings.Join(resolution.HouseIgnore, ",") != "pnpm-lock.yaml,*.sqlite" ||
		resolution.Options.TabWidth != 4 {
		test.Fatalf("resolved %+v, want the house list beside tab width 4", resolution)
	}

	// An outsider's block without the key keeps the house list, cohere:typescript's.
	undeclared := test.TempDir()
	project(test, undeclared, `{"tabWidth": 4}`)
	house, houseResolutionError := houseResolution()
	if houseResolutionError != nil {
		test.Fatal(houseResolutionError)
	}
	if resolution, resolveError := Resolve(undeclared); resolveError != nil ||
		!resolution.HouseIgnoreDeclared ||
		strings.Join(resolution.HouseIgnore, ",") != strings.Join(house.HouseIgnore, ",") {
		test.Fatalf("a block without the key did not keep the house list: %+v, %v", resolution, resolveError)
	}

	malformed := test.TempDir()
	project(test, malformed, `{"tabWidth": 4, "ignore": "pnpm-lock.yaml"}`)
	if _, resolveError := Resolve(
		malformed,
	); resolveError == nil || !strings.Contains(resolveError.Error(), NexusTierFileName) {
		test.Fatalf("an ignore that is not a list was accepted or not named: %v", resolveError)
	}

}

// TestIgnorePatternsResolveWithTheOptions: the walk takes the chain's ignorePatterns from the same
// resolution as its options, base first, so it reads the list lint reads.
func TestIgnorePatternsResolveWithTheOptions(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeFile(
		test,
		filepath.Join(root, "nexus", NexusTierFileName),
		`{"rules": {}, "ignorePatterns": ["**/generated/**"], "format": `+houseBlock+`}`,
	)
	writeFile(
		test,
		filepath.Join(root, SettingsFileName),
		`{"extends": "./nexus/`+NexusTierFileName+`", "rules": {}, "ignorePatterns": ["data/**"]}`,
	)
	resolution, resolveError := Resolve(root)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	if strings.Join(resolution.IgnorePatterns, ",") != "**/generated/**,data/**" {
		test.Fatalf("ignorePatterns %v, want the base's first", resolution.IgnorePatterns)
	}
}

// TestADroppedKeyCanNeverReachPrettiersDefaults: the move's failure is a repository whose prettier key
// went and whose options never arrived, formatting at width 80 with nothing said. Settings that say
// nothing about formatting format the house way, and old config with no settings above it is refused.
func TestADroppedKeyCanNeverReachPrettiersDefaults(test *testing.T) {
	test.Parallel()
	withoutBlock := test.TempDir()
	writeFile(test, filepath.Join(withoutBlock, SettingsFileName), `{"rules": {"no-debugger": "error"}}`)
	writeFile(test, filepath.Join(withoutBlock, "package.json"), `{"name": "x"}`)
	resolution, resolveError := Resolve(withoutBlock)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	if resolution.Options == PrettierDefaults() || resolution.Options.PrintWidth != 120 {
		test.Fatalf(
			"settings that say nothing about formatting resolved to %+v, want the house format",
			resolution.Options,
		)
	}

	neverMoved := test.TempDir()
	writeFile(
		test,
		filepath.Join(neverMoved, "package.json"),
		`{"name": "x", "prettier": {"tabWidth": 4, "printWidth": 120}}`,
	)
	if resolution, resolveError := Resolve(neverMoved); resolveError == nil {
		test.Fatalf("old config with no settings resolved to %+v", resolution.Options)
	}
}

// resolveUnremembered is Resolve as it was before the Resolver: every directory walks to its settings
// and reads the chain afresh. The reference the remembered path is held to.
func resolveUnremembered(directory string) (Resolution, error) {
	for current := directory; ; current = filepath.Dir(current) {
		leftover, found, prettierConfigurationInError := prettierConfigIn(current)
		if prettierConfigurationInError != nil {
			return Resolution{}, prettierConfigurationInError
		}
		if found {
			return Resolution{}, fmt.Errorf(
				"%s: %w; delete it, since cohere reads format options only from a \"format\" block in %s",
				leftover,
				ErrPrettierConfigRemains,
				SettingsFileName,
			)
		}
		path := filepath.Join(current, SettingsFileName)
		if _, statError := os.Stat(path); statError == nil {
			return resolveChain(path)
		}
		if parent := filepath.Dir(current); parent == current {
			return houseResolution()
		}
	}
}

// TestAResolverAnswersAsResolveDidOnOurTrees: one Resolver, shared across every directory of a real
// tree as a format run shares it, answers each directory exactly as the unremembered walk does:
// options, source, both ignore lists, and every refusal word for word (ahra's projects/www-ahra-ai,
// with its prettier key, is one). Reads files as text only.
//
// Off unless COHERE_RESOLVE_TREES lists the trees, separated as PATH is: the reference walks every
// directory afresh, and on 2026-10-04 ahra's 90,798 directories, www's 1,075 and api's 835 took 317s,
// all identical, 437 of them refusals.
func TestAResolverAnswersAsResolveDidOnOurTrees(test *testing.T) {
	test.Parallel()
	trees := filepath.SplitList(needs.OptIn(test, "COHERE_RESOLVE_TREES",
		"a differential of one shared resolver against a fresh walk over every directory of the trees it lists"))
	skipped := map[string]bool{
		".git":         true,
		"node_modules": true,
		".cache":       true,
		".next":        true,
		"data":         true,
		"dist":         true,
	}
	for _, root := range trees {
		test.Run(root, func(test *testing.T) {
			test.Parallel()
			resolver := NewResolver()
			directories, refusals := 0, 0
			walkError := filepath.WalkDir(root, func(path string, entry os.DirEntry, pathError error) error {
				if pathError != nil || !entry.IsDir() {
					return nil
				}
				if skipped[entry.Name()] {
					return filepath.SkipDir
				}
				directories++
				remembered, rememberedError := resolver.Resolve(path)
				reference, referenceError := resolveUnremembered(path)
				if fmt.Sprint(rememberedError) != fmt.Sprint(referenceError) {
					test.Fatalf("%s: the Resolver answered %v, the walk %v", path, rememberedError, referenceError)
				}
				if referenceError != nil {
					refusals++
					return nil
				}
				if fmt.Sprintf("%+v", remembered) != fmt.Sprintf("%+v", reference) {
					test.Fatalf("%s: the Resolver answered %+v, the walk %+v", path, remembered, reference)
				}
				return nil
			})
			if walkError != nil {
				test.Fatal(walkError)
			}
			if directories < 50 {
				test.Fatalf(
					"compared only %d directories under %s, so the comparison measured nothing",
					directories,
					root,
				)
			}
			test.Logf("%d directories identical, %d of them refusals", directories, refusals)
		})
	}
}

// TestAnEditAnywhereInTheChainIsSeen: a remembered chain is used only while its files read what they
// did, so a later run sees an edit to the Nexus tier and to the project's own `extends`, even one that
// leaves the file's size unchanged.
func TestAnEditAnywhereInTheChainIsSeen(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	nexusTier := filepath.Join(root, "nexus", NexusTierFileName)
	writeFile(test, nexusTier, `{"format": {"printWidth": 120}}`)
	writeFile(test, filepath.Join(root, "other", NexusTierFileName), `{"format": {"printWidth": 90}}`)
	settings := filepath.Join(root, SettingsFileName)
	writeFile(test, settings, `{"extends": "./nexus/`+NexusTierFileName+`"}`)

	width := func() int {
		test.Helper()
		resolution, resolveError := NewResolver().Resolve(filepath.Join(root, "source"))
		if resolveError != nil {
			test.Fatal(resolveError)
		}
		return resolution.Options.PrintWidth
	}
	if got := width(); got != 120 {
		test.Fatalf("print width %d, want the Nexus tier's 120", got)
	}

	writeFile(test, nexusTier, `{"format": {"printWidth": 100}}`)
	if got := width(); got != 100 {
		test.Fatalf("after the Nexus tier changed to 100 at the same size, a run read %d", got)
	}

	writeFile(test, settings, `{"extends": "./other/`+NexusTierFileName+`"}`)
	if got := width(); got != 90 {
		test.Fatalf("after the project's extends moved to another tier, a run read %d, want 90", got)
	}
}

// TestARememberedParentDoesNotAnswerForAChildWithLeftoverConfiguration: a Resolver that already knows a
// directory's options still walks a child that holds old Prettier config, and refuses it.
func TestARememberedParentDoesNotAnswerForAChildWithLeftoverConfiguration(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	project(test, root, houseBlock)
	writeFile(test, filepath.Join(root, "packages", "inner", ".prettierrc"), `{"printWidth": 120}`)

	resolver := NewResolver()
	if _, resolveError := resolver.Resolve(filepath.Join(root, "packages")); resolveError != nil {
		test.Fatal(resolveError)
	}
	if _, resolveError := resolver.Resolve(
		filepath.Join(root, "packages", "inner", "source"),
	); !errors.Is(resolveError, ErrPrettierConfigRemains) {
		test.Fatalf("a child with a leftover resolved through its remembered parent: %v", resolveError)
	}
}
