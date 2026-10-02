package formatoptions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

/*
 * Which options a directory is formatted with.
 *
 * They live in the `format` block of the repository's CohereSettings.json, beside its rules, and
 * nowhere else. They used to be read from package.json's `prettier` key, and before that every caller
 * passed Default, which is ahra's block, to every repository. That was measured wrong on the first
 * differential run: api-phi-health sets `bracketSameLine: true`, the oracle did not, and its JSX
 * numbers described a formatter that repository does not use.
 *
 * So this resolves from the nearest CohereSettings.json walking up from the directory, and refuses
 * every case that would otherwise format with options nobody chose: a CohereSettings.json without a
 * `format` block, and Prettier config left behind in its old place. The only way to get Prettier's
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

	// Source is the CohereSettings.json the options came from, or empty when none was found and
	// Prettier's own defaults apply.
	Source string
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
		contents, err := os.ReadFile(path)
		if err == nil {
			var settings map[string]json.RawMessage
			if err := json.Unmarshal(contents, &settings); err != nil {
				return Resolution{}, fmt.Errorf("%s is not valid JSON: %w", path, err)
			}
			block, present := settings["format"]
			if !present {
				return Resolution{}, fmt.Errorf("%s has no \"format\" block, so it does not say how to format; add one rather than formatting with Prettier's defaults", path)
			}
			resolution, err := applyFormatBlock(path, block, false)
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
	old, err := applyFormatBlock(leftover.path, leftover.raw, true)
	if err != nil {
		return fmt.Errorf("%s: %w and cannot be read: %v", leftover.path, ErrPrettierConfigRemains, err)
	}
	if old.Options != resolution.Options {
		return fmt.Errorf("%s: %w and disagrees with %s (%+v against %+v); delete it, since cohere formats with the format block",
			leftover.path, ErrPrettierConfigRemains, resolution.Source, old.Options, resolution.Options)
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

// applyFormatBlock decodes one format block over Prettier's defaults, refusing any key it would have to
// ignore. ignorePluginKeys lets a leftover Prettier config through with its Tailwind plugin settings
// (plugins, tailwind*), which were never format options, so it can be compared with the format block;
// a format block itself must not carry them.
func applyFormatBlock(path string, raw json.RawMessage, ignorePluginKeys bool) (Resolution, error) {
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil {
		return Resolution{}, fmt.Errorf("%s: the \"format\" block is not a JSON object: %w", path, err)
	}

	resolution := Resolution{Options: PrettierDefaults(), Source: path}
	options := &resolution.Options

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
		default:
			if ignorePluginKeys && (key == "plugins" || strings.HasPrefix(key, "tailwind")) {
				continue
			}
			return Resolution{}, fmt.Errorf("%s: format option %q is not one cohere applies; add it to Options rather than formatting without it", path, key)
		}
		if err != nil {
			return Resolution{}, fmt.Errorf("%s: format option %q: %w", path, key, err)
		}
	}

	if options.EndOfLine != "lf" {
		return Resolution{}, fmt.Errorf("%s sets endOfLine %q; only \"lf\" is supported", path, options.EndOfLine)
	}
	return resolution, nil
}
