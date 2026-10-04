package formatoptions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
 * and Prettier config left behind in its old place, whatever it says. The only way to get Prettier's
 * defaults is to configure nothing anywhere, which is the honest meaning of a default. A refusal says
 * which file and why.
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

	// Old config is refused wherever the walk up meets it, up to and including the directory whose
	// settings win: cohere never reads it, so keeping it is a second statement of the options that
	// nothing checks.
	for current := absolute; ; current = filepath.Dir(current) {
		leftover, found, err := prettierConfigIn(current)
		if err != nil {
			return Resolution{}, err
		}
		if found {
			return Resolution{}, fmt.Errorf("%s: %w; delete it, since cohere formats with the format block in the Nexus tier (%s)",
				leftover, ErrPrettierConfigRemains, NexusTierFileName)
		}

		path := filepath.Join(current, SettingsFileName)
		if _, err := os.Stat(path); err == nil {
			return resolveChain(path)
		}

		if parent := filepath.Dir(current); parent == current {
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
	options, err := applyFormatBlock(nexusTier, block, PrettierDefaults())
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

// prettierConfigIn names the Prettier config in a directory, if any: a package.json carrying a
// "prettier" key, or one of Prettier's config files.
func prettierConfigIn(directory string) (string, bool, error) {
	manifestPath := filepath.Join(directory, "package.json")
	if contents, err := os.ReadFile(manifestPath); err == nil {
		var manifest map[string]json.RawMessage
		if err := json.Unmarshal(contents, &manifest); err != nil {
			return "", false, fmt.Errorf("%s is not valid JSON: %w", manifestPath, err)
		}
		if _, present := manifest["prettier"]; present {
			return manifestPath + " (its \"prettier\" key)", true, nil
		}
	}
	for _, candidate := range prettierConfigFiles {
		path := filepath.Join(directory, candidate)
		if _, err := os.Stat(path); err == nil {
			return path, true, nil
		}
	}
	return "", false, nil
}

// BlockKey is one key the format block accepts. The table is the decoder itself, and the settings
// schema (internal/settingsschema) is generated from it, so neither can learn a key the other lacks.
type BlockKey struct {
	Name string

	// Type is the key's JSON type: "integer", "boolean", "string", or "array" of patterns.
	Type string

	// Enum is the values a string key takes, when they are fixed. endOfLine's is enforced here; the
	// others are Prettier's, which the printers implement and nothing else.
	Enum []string

	// Option points at the field the value decodes into, nil for a key resolveChain reads itself.
	Option func(options *Options) any
}

// BlockKeys is every key of the format block, in the order the reference documents them.
var BlockKeys = []BlockKey{
	{Name: "printWidth", Type: "integer", Option: func(options *Options) any { return &options.PrintWidth }},
	{Name: "tabWidth", Type: "integer", Option: func(options *Options) any { return &options.TabWidth }},
	{Name: "useTabs", Type: "boolean", Option: func(options *Options) any { return &options.UseTabs }},
	{Name: "semi", Type: "boolean", Option: func(options *Options) any { return &options.Semi }},
	{Name: "singleQuote", Type: "boolean", Option: func(options *Options) any { return &options.SingleQuote }},
	{Name: "trailingComma", Type: "string", Enum: []string{"all", "es5", "none"}, Option: func(options *Options) any { return &options.TrailingComma }},
	{Name: "bracketSpacing", Type: "boolean", Option: func(options *Options) any { return &options.BracketSpacing }},
	{Name: "bracketSameLine", Type: "boolean", Option: func(options *Options) any { return &options.BracketSameLine }},
	{Name: "arrowParens", Type: "string", Enum: []string{"always", "avoid"}, Option: func(options *Options) any { return &options.ArrowParens }},
	{Name: "endOfLine", Type: "string", Enum: []string{"lf"}, Option: func(options *Options) any { return &options.EndOfLine }},
	{Name: "ignore", Type: "array"},
}

func blockKeyNamed(name string) (BlockKey, bool) {
	for _, key := range BlockKeys {
		if key.Name == name {
			return key, true
		}
	}
	return BlockKey{}, false
}

// applyFormatBlock decodes one format block over the options given, refusing any key it would have to
// ignore.
func applyFormatBlock(path string, raw json.RawMessage, over Options) (Options, error) {
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
		blockKey, known := blockKeyNamed(key)
		if known && blockKey.Option == nil {
			// The house ignore list rides in the format block but is no printing option: resolveChain
			// reads it.
			continue
		}
		if !known {
			return Options{}, fmt.Errorf("%s: format option %q is not one cohere applies; add it to Options rather than formatting without it", path, key)
		}
		if err := json.Unmarshal(value, blockKey.Option(options)); err != nil {
			return Options{}, fmt.Errorf("%s: format option %q: %w", path, key, err)
		}
	}

	if options.EndOfLine != "lf" {
		return Options{}, fmt.Errorf("%s sets endOfLine %q; only \"lf\" is supported", path, options.EndOfLine)
	}
	return applied, nil
}
