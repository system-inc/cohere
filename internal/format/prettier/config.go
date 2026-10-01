package prettier

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

/*
 * Which Prettier options a directory is formatted with.
 *
 * The engine takes its options from the caller and says so, because resolving a config is Node's job
 * and goja has no filesystem. Until this file every caller passed DefaultOptions, which is ahra's
 * block, to every repository. That was measured wrong on the first differential run: api-phi-health
 * sets `bracketSameLine: true`, the oracle did not, and its JSX numbers described a formatter that
 * repository does not use.
 *
 * So this resolves the way Prettier does, nearest config walking up from the directory, and refuses
 * every form it does not parse rather than falling back to a default that formats the tree differently.
 * A refusal says which file and why. A silent default is the defect this replaces.
 */

// configCandidates are Prettier's search order within one directory, from its config searcher. The
// first one present wins, and package.json counts only when it carries a "prettier" key.
var configCandidates = []string{
	"package.json",
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

// pluginOwnedKeys belong to prettier-plugin-tailwindcss. The engine runs core Prettier without that
// plugin by design, because class order is cohere's lint fixer's job, so these are read, named and
// not applied.
func isPluginOwnedKey(key string) bool {
	return key == "plugins" || strings.HasPrefix(key, "tailwind")
}

// Resolution is the options a directory formats with, and where they came from.
type Resolution struct {
	Options Options

	// Source is the config file the options came from, or empty when none was found and Prettier's
	// own defaults apply.
	Source string

	// NotApplied are the plugin-owned keys the config sets that the engine does not run.
	NotApplied []string
}

// PrettierDefaults are Prettier 3's own defaults, what a directory with no config formats with.
//
// Not DefaultOptions: those are ahra's choices, and a repository that configures nothing gets tab
// width 2, print width 80 and double quotes, not ahra's 4, 120 and single.
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

// ResolveOptions finds the config governing directory and applies it over Prettier's defaults.
func ResolveOptions(directory string) (Resolution, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return Resolution{}, err
	}

	for current := absolute; ; current = filepath.Dir(current) {
		for _, candidate := range configCandidates {
			path := filepath.Join(current, candidate)
			contents, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			var raw json.RawMessage
			switch candidate {
			case "package.json":
				var manifest map[string]json.RawMessage
				if err := json.Unmarshal(contents, &manifest); err != nil {
					return Resolution{}, fmt.Errorf("%s is not valid JSON: %w", path, err)
				}
				value, present := manifest["prettier"]
				if !present {
					continue
				}
				raw = value
			case ".prettierrc", ".prettierrc.json":
				raw = contents
			default:
				return Resolution{}, fmt.Errorf("%s configures Prettier in a form cohere does not read; convert it to a package.json \"prettier\" key or JSON .prettierrc", path)
			}

			return applyConfig(path, raw)
		}

		if parent := filepath.Dir(current); parent == current {
			return Resolution{Options: PrettierDefaults()}, nil
		}
	}
}

// applyConfig decodes one config over Prettier's defaults, refusing any key it would have to ignore.
func applyConfig(path string, raw json.RawMessage) (Resolution, error) {
	var shared string
	if json.Unmarshal(raw, &shared) == nil {
		return Resolution{}, fmt.Errorf("%s names a shared config %q, which cohere does not resolve", path, shared)
	}

	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return Resolution{}, fmt.Errorf("%s: the Prettier config is not a JSON object: %w", path, err)
	}

	resolution := Resolution{Options: PrettierDefaults(), Source: path}
	options := &resolution.Options

	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := config[key]
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
			if isPluginOwnedKey(key) {
				resolution.NotApplied = append(resolution.NotApplied, key)
				continue
			}
			return Resolution{}, fmt.Errorf("%s sets Prettier option %q, which cohere does not apply; add it to Options rather than formatting without it", path, key)
		}
		if err != nil {
			return Resolution{}, fmt.Errorf("%s: option %q: %w", path, key, err)
		}
	}

	if options.EndOfLine != "lf" {
		return Resolution{}, fmt.Errorf("%s sets endOfLine %q; only \"lf\" is supported", path, options.EndOfLine)
	}
	return resolution, nil
}
