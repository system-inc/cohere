package formatoptions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/lint/configuration"
)

/*
 * Which options a directory is formatted with.
 *
 * They live in one `format` block, in the Nexus tier (NexusCohereSettings.json) that every
 * repository's CohereSettings.json extends, and nowhere else: formatting is unified (Kirk's ruling,
 * 2026-10-03). They used to be read from package.json's `prettier` key, then from each repository's
 * own block, merged over its tiers.
 *
 * So this resolves from the nearest CohereSettings.json walking up from the directory, follows its
 * `extends` chain as the lint loader does, and takes the block from the Nexus tier alone. It refuses
 * every case that would otherwise format with options nobody chose or chose in two places: a `format`
 * key in any other file of the chain, a chain without a Nexus tier, a Nexus tier without the block,
 * and Prettier config left behind in its old place. The only way to get Prettier's defaults is to
 * configure nothing anywhere, which is the honest meaning of a default. A refusal says which file and
 * why.
 */

// SettingsFileName is the file a repository's cohere configuration lives in.
const SettingsFileName = "CohereSettings.json"

// ErrPrettierConfigRemains is a refusal over Prettier config left where cohere no longer reads it. A
// caller walking repositories it was not asked about (a nested one, pinned to its own adoption) can
// tell this apart from a config that is wrong.
var ErrPrettierConfigRemains = errors.New("Prettier config remains where cohere no longer reads it")

// prettierConfigFiles are Prettier's config files, from its config searcher. Any of them, or a
// package.json carrying a "prettier" key, in a directory on the walk is options left behind.
var prettierConfigFiles = []string{
	"package.yaml",
	".prettierrc",
	".prettierrc.json",
	".prettierrc.yaml",
	".prettierrc.yml",
	".prettierrc.json5",
	".prettierrc.js",
	"prettier.config.js",
	".prettierrc.ts",
	"prettier.config.ts",
	".prettierrc.mjs",
	"prettier.config.mjs",
	".prettierrc.mts",
	"prettier.config.mts",
	".prettierrc.cjs",
	"prettier.config.cjs",
	".prettierrc.cts",
	"prettier.config.cts",
	".prettierrc.toml",
}

// Resolution is the options a directory formats with, and where they came from.
type Resolution struct {
	Options Options

	// Source is the CohereSettings.json the options were resolved from, or empty when none was found and
	// Prettier's own defaults apply. The files it extends may have written some of them.
	Source string

	// HouseIgnore is the format block's `ignore` list: paths no repository formats, written once in the
	// Nexus tier beside the options. Each pattern reads as a line of an ignore file does. Declared says
	// the block has the key at all, which is what retires Structure's PrettierIgnoreDefaults and a
	// project's .prettierignore: once the house list exists, the old files may only agree with it.
	HouseIgnore         []string
	HouseIgnoreDeclared bool

	// IgnorePatterns is the chain's `ignorePatterns`, the base's first, the one list lint and the format
	// walk share. Its globs are relative to Source's directory, as lint reads them.
	IgnorePatterns []string
}

// PrettierDefaults are Prettier 3's own defaults, what a directory with no options anywhere formats
// with.
//
// Not Default: those are ahra's choices, and a repository that configures nothing gets tab width 2,
// print width 80 and double quotes, not ahra's 4, 120 and single.
func PrettierDefaults() Options {
	return Options{
		TabWidth:        2,
		UseTabs:         false,
		Semi:            true,
		SingleQuote:     false,
		PrintWidth:      80,
		TrailingComma:   "all",
		BracketSpacing:  true,
		BracketSameLine: false,
		ArrowParens:     "always",
		EndOfLine:       "lf",
	}
}

// Resolve finds the CohereSettings.json governing directory and applies its format block over
// Prettier's defaults.
func Resolve(directory string) (Resolution, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return Resolution{}, err
	}

	// Old config is collected on the way up, in every directory up to and including the one whose
	// settings win, and checked against the options once they are known.
	var leftovers []leftoverConfig
	for current := absolute; ; current = filepath.Dir(current) {
		leftover, found, err := prettierConfigIn(current)
		if err != nil {
			return Resolution{}, err
		}
		if found {
			leftovers = append(leftovers, leftover)
		}

		path := filepath.Join(current, SettingsFileName)
		if _, err := os.Stat(path); err == nil {
			resolution, err := resolveChain(path)
			if err != nil {
				return Resolution{}, err
			}
			for _, leftover := range leftovers {
				if err := leftover.agreesWith(resolution); err != nil {
					return Resolution{}, err
				}
			}
			return resolution, nil
		}

		if parent := filepath.Dir(current); parent == current {
			// Old config with no settings above it means the options were never moved, and formatting
			// with Prettier's defaults would silently be a different width than the repository chose.
			if len(leftovers) > 0 {
				return Resolution{}, fmt.Errorf("%s: %w and no %s with a \"format\" block is above it; move its options there",
					leftovers[0].path, ErrPrettierConfigRemains, SettingsFileName)
			}
			return Resolution{Options: PrettierDefaults()}, nil
		}
	}
}

// NexusTierFileName is the one file in a chain that may hold the format block: the Nexus tier, which
// every repository's chain ends at (ahra and www-phi-health through Structure, api-phi-health
// through Base).
const NexusTierFileName = "NexusCohereSettings.json"

// NexusTierSetName is the Nexus tier as the rule set cohere carries, which a chain names in `extends`.
const NexusTierSetName = configuration.SetPrefix + "typescript"

// resolveChain reads the format block from the Nexus tier of path's `extends` chain, and only from
// there.
//
// Formatting is unified (Kirk's ruling, 2026-10-03): every repository formats the same way, so the
// block is written once, in the Nexus tier, and a `format` key anywhere else in the chain is refused,
// naming the file, rather than merged over it. A project or a Structure or Base tier that restated
// the block would be a second place the house format could drift. A chain with no Nexus tier, or a
// Nexus tier without the block, does not say how to format, and is refused rather than formatted with
// Prettier's defaults.
//
// The chain is read by the lint loader's own SourcesOf rather than by a second walk of `extends` here,
// so the two readers cannot disagree about which files a configuration is made of.
func resolveChain(path string) (Resolution, error) {
	sources, err := configuration.SourcesOf(path)
	if err != nil {
		return Resolution{}, err
	}

	var nexusTier string
	var block json.RawMessage
	for _, source := range sources {
		contents, err := configuration.SourceContents(source)
		if err != nil {
			return Resolution{}, err
		}
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(contents, &settings); err != nil {
			return Resolution{}, fmt.Errorf("%s is not valid JSON: %w", source, err)
		}
		sourceBlock, present := settings["format"]
		if source != NexusTierSetName && filepath.Base(source) != NexusTierFileName {
			if present {
				return Resolution{}, fmt.Errorf("%s has a \"format\" block; formatting is unified, and only the Nexus tier (%s) holds the format block, so remove it here", source, NexusTierFileName)
			}
			continue
		}
		nexusTier = source
		if present {
			block = sourceBlock
		}
	}

	if nexusTier == "" {
		return Resolution{}, fmt.Errorf("%s does not extend the Nexus tier (%s), which holds the format block, so it does not say how to format", path, NexusTierFileName)
	}
	if block == nil {
		return Resolution{}, fmt.Errorf("%s, the Nexus tier %s extends, has no \"format\" block, so the chain does not say how to format", nexusTier, path)
	}
	options, err := applyFormatBlock(nexusTier, block, PrettierDefaults(), false)
	if err != nil {
		return Resolution{}, err
	}
	resolution := Resolution{Options: options, Source: path}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(block, &keys); err != nil {
		return Resolution{}, fmt.Errorf("%s: the \"format\" block is not a JSON object: %w", nexusTier, err)
	}
	if raw, present := keys["ignore"]; present {
		if err := json.Unmarshal(raw, &resolution.HouseIgnore); err != nil {
			return Resolution{}, fmt.Errorf("%s: the format block's \"ignore\" is not a list of patterns: %w", nexusTier, err)
		}
		resolution.HouseIgnoreDeclared = true
	}

	resolution.IgnorePatterns, err = configuration.IgnorePatternsOf(path)
	if err != nil {
		return Resolution{}, err
	}
	return resolution, nil
}

// leftoverConfig is Prettier config found where cohere no longer reads it.
//
// It is tolerated only while it agrees with the format block, key for key, so a repository mid-move
// keeps formatting and nothing can drift: the old copy is never read for options, only compared. The
// tolerance exists for ahra's move, which waits on its editor (#3w83j3k); once that lands, any leftover
// is refused outright.
type leftoverConfig struct {
	path string

	// raw is the options as JSON, or nil for a form cohere cannot read (a JavaScript config), which can
	// never be shown to agree.
	raw json.RawMessage
}

// agreesWith refuses the leftover unless its options decode to exactly the format block's.
func (leftover leftoverConfig) agreesWith(resolution Resolution) error {
	if leftover.raw == nil {
		return fmt.Errorf("%s: %w in a form cohere cannot compare with %s; delete it",
			leftover.path, ErrPrettierConfigRemains, resolution.Source)
	}
	old, err := applyFormatBlock(leftover.path, leftover.raw, PrettierDefaults(), true)
	if err != nil {
		return fmt.Errorf("%s: %w and cannot be read: %v", leftover.path, ErrPrettierConfigRemains, err)
	}
	if old != resolution.Options {
		return fmt.Errorf("%s: %w and disagrees with %s (%+v against %+v); delete it, since cohere formats with the format block",
			leftover.path, ErrPrettierConfigRemains, resolution.Source, old, resolution.Options)
	}
	return nil
}

// prettierConfigIn finds the Prettier config in a directory, if any.
func prettierConfigIn(directory string) (leftoverConfig, bool, error) {
	manifestPath := filepath.Join(directory, "package.json")
	if contents, err := os.ReadFile(manifestPath); err == nil {
		var manifest map[string]json.RawMessage
		if err := json.Unmarshal(contents, &manifest); err != nil {
			return leftoverConfig{}, false, fmt.Errorf("%s is not valid JSON: %w", manifestPath, err)
		}
		if value, present := manifest["prettier"]; present {
			return leftoverConfig{path: manifestPath + " (its \"prettier\" key)", raw: value}, true, nil
		}
	}
	for _, candidate := range prettierConfigFiles {
		path := filepath.Join(directory, candidate)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		leftover := leftoverConfig{path: path}
		if candidate == ".prettierrc" || candidate == ".prettierrc.json" {
			if contents, err := os.ReadFile(path); err == nil && json.Valid(contents) {
				leftover.raw = contents
			}
		}
		return leftover, true, nil
	}
	return leftoverConfig{}, false, nil
}

// applyFormatBlock decodes one format block over the options given, refusing any key it would have to
// ignore. ignorePluginKeys lets a leftover Prettier config through with its Tailwind plugin settings
// (plugins, tailwind*), which were never format options, so it can be compared with the format block;
// a format block itself must not carry them.
func applyFormatBlock(path string, raw json.RawMessage, over Options, ignorePluginKeys bool) (Options, error) {
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil {
		return Options{}, fmt.Errorf("%s: the \"format\" block is not a JSON object: %w", path, err)
	}

	applied := over
	options := &applied

	keys := make([]string, 0, len(block))
	for key := range block {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := block[key]
		var err error
		switch key {
		case "tabWidth":
			err = json.Unmarshal(value, &options.TabWidth)
		case "useTabs":
			err = json.Unmarshal(value, &options.UseTabs)
		case "semi":
			err = json.Unmarshal(value, &options.Semi)
		case "singleQuote":
			err = json.Unmarshal(value, &options.SingleQuote)
		case "printWidth":
			err = json.Unmarshal(value, &options.PrintWidth)
		case "trailingComma":
			err = json.Unmarshal(value, &options.TrailingComma)
		case "bracketSpacing":
			err = json.Unmarshal(value, &options.BracketSpacing)
		case "bracketSameLine":
			err = json.Unmarshal(value, &options.BracketSameLine)
		case "arrowParens":
			err = json.Unmarshal(value, &options.ArrowParens)
		case "endOfLine":
			err = json.Unmarshal(value, &options.EndOfLine)
		case "ignore":
			// The house ignore list rides in the format block but is no printing option: resolveChain
			// reads it. Prettier config never had the key, so a leftover carrying it is refused.
			if ignorePluginKeys {
				return Options{}, fmt.Errorf("%s: format option %q is not one cohere applies; add it to Options rather than formatting without it", path, key)
			}
			continue
		default:
			if ignorePluginKeys && (key == "plugins" || strings.HasPrefix(key, "tailwind")) {
				continue
			}
			return Options{}, fmt.Errorf("%s: format option %q is not one cohere applies; add it to Options rather than formatting without it", path, key)
		}
		if err != nil {
			return Options{}, fmt.Errorf("%s: format option %q: %w", path, key, err)
		}
	}

	if options.EndOfLine != "lf" {
		return Options{}, fmt.Errorf("%s sets endOfLine %q; only \"lf\" is supported", path, options.EndOfLine)
	}
	return applied, nil
}
