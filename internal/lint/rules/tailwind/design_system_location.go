package tailwind

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/rule"
	tailwindengine "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
)

// TailwindLocationOptions are upstream's options that say where Tailwind and its stylesheet are, and how
// the stylesheet's imports resolve, shared by every better-tailwindcss rule: `entryPoint`,
// `tailwindConfig`, `cwd` and `tsconfig` (see tsconfig_paths.go for the last).
//
// Upstream resolves all three in `getTailwindConfigPath` (utils/context.js at 4.7.0). `cwd` is
// resolved against the directory ESLint runs from and is where `tailwindcss` is resolved from. The
// stylesheet is `entryPoint ?? tailwindConfig`, looked for from `cwd` by findPathRecursive. When it is
// not found, or neither is set, upstream reads Tailwind's own default theme, `tailwindcss/theme.css`.
//
// Here, a rule given none of the three keeps finding the project's stylesheet by FindEntryPoint, as it
// always has, and a rule given any of them follows upstream exactly. On a rule that never reads the
// design system they change nothing, in upstream as here.
type TailwindLocationOptions struct {
	EntryPoint     string `json:"entryPoint"`
	TailwindConfig string `json:"tailwindConfig"`
	Cwd            string `json:"cwd"`
	Tsconfig       string `json:"tsconfig"`

	// anchor is the directory the config was written in, which stands for ESLint's working directory,
	// set by the decoder from rule.OptionsBase.
	anchor string
}

// Location is the design system these options name.
func (options TailwindLocationOptions) Location() DesignSystemLocation {
	configurationPath := options.EntryPoint
	if configurationPath == "" {
		configurationPath = options.TailwindConfig
	}
	return DesignSystemLocation{
		Anchor:     options.anchor,
		Cwd:        options.Cwd,
		ConfigPath: configurationPath,
		Tsconfig:   options.Tsconfig,
	}
}

// anchorAt records where the config was written. Promoted onto every options struct that embeds
// TailwindLocationOptions, so the decoder can set it.
func (options *TailwindLocationOptions) anchorAt(directory string) {
	options.anchor = directory
}

// tailwindSettingsNamespaces are the two keys upstream reads its settings from, the first preferred:
// `eslintContext.settings["eslint-plugin-better-tailwindcss"] ?? settings["better-tailwindcss"]`.
// registry.CheckSettings refuses a config writing both, so at most one is ever here.
var tailwindSettingsNamespaces = []string{"better-tailwindcss", "eslint-plugin-better-tailwindcss"}

// decodeTailwindOptionsAt decodes a better-tailwindcss rule's options and anchors their paths at the
// config's directory. A bare severity decodes to the defaults, still anchored.
//
// The project's settings["better-tailwindcss"] sit between the defaults and the rule's own options,
// as upstream's createRule merges them (utils/rule.js at 4.7.0): defaults, then settings, then
// options, each key replacing the one before it whole. The rule's own element is decoded strictly
// first, so its errors are its own, and the settings are merged after: the keys of them this rule
// declares, overlaid by every key the element wrote. registry.CheckSettings has already refused a
// settings key no rule reads and a value a rule would refuse, before any rule decodes.
func decodeTailwindOptionsAt[Options any, Settled interface {
	*Options
	anchorAt(directory string)
	compileClassLiterals() error
}]() func(raw []byte, base rule.OptionsBase) (any, error) {
	var declared Options
	keys := map[string]bool{}
	for _, key := range rule.OptionKeys(&declared) {
		keys[key] = true
	}
	return func(raw []byte, base rule.OptionsBase) (any, error) {
		var decoded Options
		merged := map[string]json.RawMessage{}
		if settings := tailwindSettings(base.Settings); len(settings) > 0 {
			var written map[string]json.RawMessage
			if decodeError := json.Unmarshal(settings, &written); decodeError != nil {
				return decoded, fmt.Errorf("decoding settings[%q]: %w", tailwindSettingsNamespaces[0], decodeError)
			}
			for key, value := range written {
				if keys[key] {
					merged[key] = value
				}
			}
		}
		if len(raw) > 0 {
			if unmarshalOptionsError := rule.UnmarshalOptions(raw, &decoded); unmarshalOptionsError != nil {
				return decoded, fmt.Errorf("decoding %T: %w", decoded, unmarshalOptionsError)
			}
			var own map[string]json.RawMessage
			if decodeError := json.Unmarshal(raw, &own); decodeError != nil {
				return decoded, fmt.Errorf("decoding %T: %w", decoded, decodeError)
			}
			maps.Copy(merged, own)
		}
		if len(merged) > 0 {
			encoded, encodeError := json.Marshal(merged)
			if encodeError != nil {
				return decoded, fmt.Errorf("merging settings into %T: %w", decoded, encodeError)
			}
			decoded = *new(Options)
			if unmarshalOptionsError := rule.UnmarshalOptions(encoded, &decoded); unmarshalOptionsError != nil {
				return decoded, fmt.Errorf("decoding %T with settings merged: %w", decoded, unmarshalOptionsError)
			}
		}
		anchor := base.ConfigDirectory
		if anchor == "" {
			anchor = base.ProjectRoot
		}
		Settled(&decoded).anchorAt(anchor)
		// Checked and compiled here, once per configuration selection, rather than by the rule on every
		// file.
		if compileClassLiteralsError := Settled(&decoded).compileClassLiterals(); compileClassLiteralsError != nil {
			return decoded, fmt.Errorf("decoding %T: %w", decoded, compileClassLiteralsError)
		}
		return decoded, nil
	}
}

// tailwindSettings is the project's better-tailwindcss settings, under whichever spelling it wrote.
func tailwindSettings(settings map[string]json.RawMessage) json.RawMessage {
	for _, namespace := range tailwindSettingsNamespaces {
		if written, isWritten := settings[namespace]; isWritten {
			return written
		}
	}
	return nil
}

// tailwindOptionKeys is the keys each better-tailwindcss rule's options declare, by rule name, filled
// at registration.
var tailwindOptionKeys = map[string]map[string]bool{}

// splitTailwindSettings hands each better-tailwindcss rule the keys of the settings it declares, and
// refuses every key none of them declares. An upstream option not ported yet is one of those: it is
// refused here exactly as it is in a rule's own element, until its port lands.
func splitTailwindSettings(raw []byte) (map[string]json.RawMessage, error) {
	var written map[string]json.RawMessage
	if decodeError := json.Unmarshal(raw, &written); decodeError != nil {
		return nil, fmt.Errorf("expected an object of better-tailwindcss options: %w", decodeError)
	}
	parts := map[string]map[string]json.RawMessage{}
	read := map[string]bool{}
	for ruleName, keys := range tailwindOptionKeys {
		for key, value := range written {
			if keys[key] {
				if parts[ruleName] == nil {
					parts[ruleName] = map[string]json.RawMessage{}
				}
				parts[ruleName][key] = value
				read[key] = true
			}
		}
	}
	var unread []string
	for key := range written {
		if !read[key] {
			unread = append(unread, strconv.Quote(key))
		}
	}
	if len(unread) > 0 {
		sort.Strings(unread)
		return nil, fmt.Errorf("no better-tailwindcss rule cohere runs reads %s, so it would be ignored "+
			"silently. An upstream option cohere has not ported yet is refused until it is, as it is in a "+
			"rule's own options", strings.Join(unread, ", "))
	}
	encoded := make(map[string]json.RawMessage, len(parts))
	for ruleName, part := range parts {
		bytes, encodeError := json.Marshal(part)
		if encodeError != nil {
			return nil, encodeError
		}
		encoded[ruleName] = bytes
	}
	return encoded, nil
}

// tailwindRegistration registers one better-tailwindcss rule with its options, recording the keys
// those options declare for splitTailwindSettings.
func tailwindRegistration[Options any, Settled interface {
	*Options
	anchorAt(directory string)
	compileClassLiterals() error
}](tailwindRule rule.Rule) rule.Registration {
	var declared Options
	keys := map[string]bool{}
	for _, key := range rule.OptionKeys(&declared) {
		keys[key] = true
	}
	tailwindOptionKeys[tailwindRule.Name] = keys
	return rule.Registration{Rule: tailwindRule, DecodeAt: decodeTailwindOptionsAt[Options, Settled]()}
}

// ErrTailwindNotInstalled is returned when no `tailwindcss` resolves from a configured `cwd`. Upstream
// disables the rule with a warning there, so a rule holding it skips the file rather than declining
// loudly.
var ErrTailwindNotInstalled = errors.New("tailwindcss does not resolve from the configured cwd")

// skipWithoutTailwind skips the file when err is a missing Tailwind, and says whether it did. A
// design system that failed to build is not a skip, and the rule declines it loudly instead. Each
// reason is written at its Skip, where TestEverySkipSaysWhichKindItIs reads it.
func skipWithoutTailwind(ruleContext rule.Context, candidateError error) bool {
	switch {
	case errors.Is(candidateError, ErrNoTailwindEntryPoint):
		ruleContext.Skip("no Tailwind entry point is configured")
	case errors.Is(candidateError, ErrTailwindNotInstalled):
		ruleContext.Skip("tailwindcss does not resolve from the configured cwd")
	default:
		return false
	}
	return true
}

// ConfiguredEntryPoint is the stylesheet a named location resolves to, and the installed tailwindcss
// it reads with, as upstream's getTailwindConfigPath resolves them. Exported for zero config's
// detection of cohere:tailwind (internal/lint/housesets), so a project whose settings name its
// stylesheet is found by the same search its rules then make.
func ConfiguredEntryPoint(
	projectRoot string,
	location DesignSystemLocation,
	fileExists func(path string) bool,
) (string, string, error) {
	anchor := location.Anchor
	if anchor == "" {
		anchor = projectRoot
	}
	cwd := anchor
	if location.Cwd != "" {
		cwd = resolvePath(anchor, location.Cwd)
	}

	packageRoot := findTailwindPackageRoot(cwd, fileExists)
	if packageRoot == "" {
		return "", "", fmt.Errorf("%w: %s", ErrTailwindNotInstalled, cwd)
	}

	// Not found falls back to the default theme, as upstream does, which then warns in every message;
	// the warning is not ported, since these rules' messages are their own.
	entryPoint := ""
	if location.ConfigPath != "" {
		entryPoint = findPathRecursive(cwd, cwd, location.ConfigPath, fileExists)
	}
	if entryPoint == "" {
		entryPoint = filepath.Join(packageRoot, "theme.css")
		if !fileExists(entryPoint) {
			return "", "", fmt.Errorf("no default Tailwind theme at %s", entryPoint)
		}
	}
	return entryPoint, packageRoot, nil
}

// LocationInSettings is the location the project's settings["better-tailwindcss"] name, anchored at
// the config's directory, and whether they name one at all: entryPoint, tailwindConfig or cwd.
// Only those three keys are read, each as a string, from a block that carries the rules' other keys
// too; registry.CheckSettings refuses a malformed value by name.
func LocationInSettings(settings map[string]json.RawMessage, anchor string) (DesignSystemLocation, bool) {
	var written map[string]json.RawMessage
	if decodeError := json.Unmarshal(tailwindSettings(settings), &written); decodeError != nil || len(written) == 0 {
		return DesignSystemLocation{}, false
	}
	read := func(key string) string {
		var value string
		// Ignored: an absent key reads as "", naming no location, which is what it means. A value of the
		// wrong type reads as "" too, and registry.CheckSettings refuses it by name for the run.
		_ = json.Unmarshal(written[key], &value)
		return value
	}
	location := TailwindLocationOptions{
		EntryPoint:     read("entryPoint"),
		TailwindConfig: read("tailwindConfig"),
		Cwd:            read("cwd"),
		Tsconfig:       read("tsconfig"),
	}
	location.anchorAt(anchor)
	named := location.Location()
	return named, !named.isDefault()
}

// WritesSettings reports whether the project writes settings for better-tailwindcss, under either name.
func WritesSettings(settings map[string]json.RawMessage) bool {
	return tailwindSettings(settings) != nil
}

// loadConfiguredDesignSystem is upstream's resolution for a rule that named a location.
func loadConfiguredDesignSystem(
	projectRoot string,
	fileSystem *rule.RecordingFS,
	location DesignSystemLocation,
) DesignSystemResult {
	entryPoint, packageRoot, configuredEntryPointError := ConfiguredEntryPoint(
		projectRoot,
		location,
		func(path string) bool { return fileSystem.FileExists(tspath.RootedFilePath(path)) },
	)
	if configuredEntryPointError != nil {
		return DesignSystemResult{Error: configuredEntryPointError}
	}

	resolve, stylesheetResolverAtError := stylesheetResolverAt(
		location.cwdFrom(projectRoot),
		location.Tsconfig,
		packageRoot,
		fileSystem,
	)
	if stylesheetResolverAtError != nil {
		return DesignSystemResult{EntryPoint: entryPoint, Error: stylesheetResolverAtError}
	}
	system, loadDesignSystemError := tailwindengine.LoadDesignSystem(tailwindengine.LoadOptions{
		EntryPoint:          entryPoint,
		TailwindPackageRoot: packageRoot,
		Resolve:             resolve,
	})
	if loadDesignSystemError != nil {
		return DesignSystemResult{EntryPoint: entryPoint, Error: loadDesignSystemError}
	}
	for _, stylesheet := range system.Stylesheets {
		fileSystem.Stat(tspath.RootedPath(stylesheet))
	}
	return loadedDesignSystemResult(system, entryPoint)
}

// cwdFrom is upstream's `ctx.cwd` for this location: the anchor, or the project root when there is none,
// with `cwd` resolved against it when written.
func (location DesignSystemLocation) cwdFrom(projectRoot string) string {
	anchor := location.Anchor
	if anchor == "" {
		anchor = projectRoot
	}
	if location.Cwd != "" {
		return resolvePath(anchor, location.Cwd)
	}
	return anchor
}

// stylesheetResolverAt is the resolver a design system's `@import`s go through: the installed
// tailwindcss's, with the tsconfig upstream would find from cwd in front of it. Every question about
// the disk goes through fileSystem, so the tsconfig, everything it extends and every path it maps are
// inputs the run cache signs.
func stylesheetResolverAt(
	cwd string,
	configured string,
	packageRoot string,
	fileSystem *rule.RecordingFS,
) (tailwindengine.StylesheetResolver, error) {
	fileExists := func(path string) bool { return fileSystem.FileExists(tspath.RootedFilePath(path)) }
	readFile := func(path string) (string, bool) { return fileSystem.ReadFile(tspath.RootedFilePath(path)) }
	next := tailwindengine.NodeStylesheetResolver(packageRoot)
	configurationPath := findTsconfig(cwd, configured, fileExists)
	if configurationPath == "" {
		return next, nil
	}
	mapping, loadTsconfigPathsError := loadTsconfigPaths(configurationPath, readFile, fileExists)
	if loadTsconfigPathsError != nil {
		return nil, loadTsconfigPathsError
	}
	return tsconfigStylesheetResolver(mapping, next, readFile, fileExists), nil
}

// findPathRecursive is upstream's search (async-utils/fs.js at 4.7.0) for one path: the path resolved
// against start, and if it is not there, the same file name one directory up, and so on, stopping at
// the filesystem root or once the directory searched is cwd itself.
func findPathRecursive(cwd string, start string, entry string, fileExists func(path string) bool) string {
	for candidate := resolvePath(start, entry); ; {
		if fileExists(candidate) {
			return candidate
		}
		directory := filepath.Dir(candidate)
		parent := filepath.Dir(directory)
		if parent == directory || directory == cwd {
			return ""
		}
		candidate = filepath.Join(parent, filepath.Base(candidate))
	}
}

// resolvePath is Node's path.resolve for two segments: an absolute path stands, a relative one joins.
func resolvePath(base string, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
