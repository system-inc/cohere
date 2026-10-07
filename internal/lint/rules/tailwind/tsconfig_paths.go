package tailwind

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tailwindengine "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
)

// The `tsconfig` option, ported from better-tailwindcss 4.7.0 (#ewss35z).
//
// Upstream finds a tsconfig whether or not the option is set (utils/context.js getTSConfigPath): the
// option's path, then `tsconfig.json`, then `jsconfig.json`, searched from `cwd` by findPathRecursive.
// When it finds one, the resolver every stylesheet `@import` goes through carries
// tsconfig-paths-webpack-plugin 4.2.0 (async-utils/resolvers.js), so a specifier the tsconfig's
// `paths` or `baseUrl` maps resolves there first: `@import '@/styles/tokens.css'` reads the file
// `@/*` names. A relative specifier is never mapped.
//
// The mapping is tsconfig-paths 4.2.0's, ported line for line where it decides an answer: `extends`
// (a string or a list) merged shallowly with `compilerOptions` merged a level down, an extended
// `baseUrl` rebased onto the extending file, the base URL resolved from the tsconfig's directory,
// patterns tried longest prefix first, and a match-all `*` onto the base URL added whenever the
// tsconfig declares none, which the plugin always asks for.
//
// Two boundaries, each stated rather than approximated. The file is read as JSON with comments and
// trailing commas, the form tsconfig files take; one using JSON5's other syntax is refused by name
// rather than misread. And only stylesheets are resolved through it: cohere never loads `@plugin` or
// `@config` modules, which upstream also resolves through the tsconfig.

// tsconfigNames are the files upstream looks for after the option's own path, in order.
var tsconfigNames = []string{"tsconfig.json", "jsconfig.json"}

// tsconfigExtensions are the plugin's default extensions, which better-tailwindcss does not
// override: a mapped path is tried as written, then with each of these.
var tsconfigExtensions = []string{".ts", ".tsx"}

// tsconfigPaths is one tsconfig's path mapping, resolved to absolute paths.
type tsconfigPaths struct {
	// mappings are tried in order: longest pattern prefix first, the match-all last.
	mappings []tsconfigPathMapping
}

// tsconfigPathMapping is one `paths` entry, its substitutions absolute.
type tsconfigPathMapping struct {
	pattern string
	paths   []string
}

// findTsconfig is upstream's getTSConfigPath: the option's path, then each default name, searched from
// cwd. Empty when none is there, which upstream answers by resolving without a tsconfig.
func findTsconfig(cwd string, configured string, fileExists func(path string) bool) string {
	var entries []string
	if configured != "" {
		entries = append(entries, configured)
	}
	return findFirstPathRecursive(cwd, cwd, append(entries, tsconfigNames...), fileExists)
}

// findFirstPathRecursive is upstream's findPathRecursive for several entries (async-utils/fs.js at
// 4.7.0). It is a queue: every entry is tried where it was asked for before any is tried a directory
// up, and an entry stops climbing at the filesystem root or once it was looked for in cwd itself.
func findFirstPathRecursive(cwd string, start string, entries []string, fileExists func(path string) bool) string {
	queue := make([]string, 0, len(entries))
	for _, entry := range entries {
		queue = append(queue, resolvePath(start, entry))
	}
	for len(queue) > 0 {
		candidate := queue[0]
		queue = queue[1:]
		if fileExists(candidate) {
			return candidate
		}
		directory := filepath.Dir(candidate)
		parent := filepath.Dir(directory)
		if parent == directory || directory == cwd {
			continue
		}
		queue = append(queue, filepath.Join(parent, filepath.Base(candidate)))
	}
	return ""
}

// loadTsconfigPaths reads a tsconfig and everything it extends into its path mapping.
func loadTsconfigPaths(configPath string, readFile func(path string) (string, bool), fileExists func(path string) bool) (*tsconfigPaths, error) {
	config, err := loadTsconfig(configPath, readFile, fileExists, map[string]bool{})
	if err != nil {
		return nil, err
	}
	compilerOptions := config.compilerOptions()
	var baseUrl string
	if raw, isWritten := compilerOptions["baseUrl"]; isWritten {
		_ = json.Unmarshal(raw, &baseUrl)
	}
	absoluteBaseUrl := resolvePath(filepath.Dir(configPath), baseUrl)

	var paths map[string][]string
	if raw, isWritten := compilerOptions["paths"]; isWritten {
		if err := json.Unmarshal(raw, &paths); err != nil {
			return nil, fmt.Errorf("%s: compilerOptions.paths is not an object of string lists: %w", configPath, err)
		}
	}

	patterns := make([]string, 0, len(paths))
	for pattern := range paths {
		patterns = append(patterns, pattern)
	}
	// Longest prefix first, as tsconfig-paths sorts. JavaScript's sort is stable and object keys keep
	// their written order, which a Go map does not, so ties are broken by the pattern for a reproducible
	// answer; two patterns with one prefix length and one specifier that both match are not a case
	// tsconfig-paths orders deliberately either.
	sort.SliceStable(patterns, func(left, right int) bool {
		leftLength, rightLength := tsconfigPrefixLength(patterns[left]), tsconfigPrefixLength(patterns[right])
		if leftLength != rightLength {
			return leftLength > rightLength
		}
		return patterns[left] < patterns[right]
	})

	mapping := &tsconfigPaths{}
	for _, pattern := range patterns {
		absolute := make([]string, 0, len(paths[pattern]))
		for _, substitution := range paths[pattern] {
			absolute = append(absolute, resolvePath(absoluteBaseUrl, substitution))
		}
		mapping.mappings = append(mapping.mappings, tsconfigPathMapping{pattern: pattern, paths: absolute})
	}
	if _, hasMatchAll := paths["*"]; !hasMatchAll {
		mapping.mappings = append(mapping.mappings, tsconfigPathMapping{
			pattern: "*",
			paths:   []string{strings.TrimSuffix(absoluteBaseUrl, "/") + "/*"},
		})
	}
	return mapping, nil
}

// tsconfigPrefixLength is the length of a pattern before its `*`, tsconfig-paths' getPrefixLength. A
// pattern with no star is its own prefix in JavaScript's `substr(0, -1)`, which is the empty string.
func tsconfigPrefixLength(pattern string) int {
	star := strings.IndexByte(pattern, '*')
	if star < 0 {
		return 0
	}
	return star
}

// tsconfigFile is one tsconfig read as JSON, its top-level keys kept raw.
type tsconfigFile map[string]json.RawMessage

func (config tsconfigFile) compilerOptions() map[string]json.RawMessage {
	options := map[string]json.RawMessage{}
	_ = json.Unmarshal(config["compilerOptions"], &options)
	return options
}

// loadTsconfig is tsconfig-paths' loadTsconfig: the file, with whatever it extends merged under it.
func loadTsconfig(configPath string, readFile func(path string) (string, bool), fileExists func(path string) bool, visiting map[string]bool) (tsconfigFile, error) {
	if visiting[configPath] {
		return nil, fmt.Errorf("%s extends itself", configPath)
	}
	visiting[configPath] = true
	defer delete(visiting, configPath)

	text, isRead := readFile(configPath)
	if !isRead {
		return nil, nil
	}
	config := tsconfigFile{}
	if err := json.Unmarshal([]byte(stripJsonComments(strings.TrimPrefix(text, "\ufeff"))), &config); err != nil {
		return nil, fmt.Errorf("%s is malformed: %w; cohere reads a tsconfig as JSON with comments and trailing commas", configPath, err)
	}

	raw, extends := config["extends"]
	if !extends {
		return config, nil
	}
	var extended []string
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single == "" {
			return config, nil
		}
		extended = []string{single}
	} else if err := json.Unmarshal(raw, &extended); err != nil {
		return nil, fmt.Errorf("%s: extends is neither a string nor a list of strings", configPath)
	}

	base := tsconfigFile{}
	for _, value := range extended {
		loaded, err := loadTsconfigFromExtends(configPath, value, readFile, fileExists, visiting)
		if err != nil {
			return nil, err
		}
		base = mergeTsconfigs(base, loaded)
	}
	return mergeTsconfigs(base, config), nil
}

// loadTsconfigFromExtends is tsconfig-paths' loadTsconfigFromExtends, quirks included: `.json` is
// appended when the value names no `.json` anywhere, a value holding both a `/` and a `.` that is not
// beside the file is looked for under its `node_modules`, and an extended `baseUrl` is rebased by the
// value's own directory so it reads relative to the extending file.
func loadTsconfigFromExtends(configPath, value string, readFile func(path string) (string, bool), fileExists func(path string) bool, visiting map[string]bool) (tsconfigFile, error) {
	if !strings.Contains(value, ".json") {
		value += ".json"
	}
	directory := filepath.Dir(configPath)
	extendedPath := filepath.Join(directory, value)
	if strings.Contains(value, "/") && strings.Contains(value, ".") && !fileExists(extendedPath) {
		extendedPath = filepath.Join(directory, "node_modules", value)
	}
	config, err := loadTsconfig(extendedPath, readFile, fileExists, visiting)
	if err != nil || config == nil {
		return tsconfigFile{}, err
	}
	options := config.compilerOptions()
	var baseUrl string
	if raw, isWritten := options["baseUrl"]; isWritten && json.Unmarshal(raw, &baseUrl) == nil && baseUrl != "" {
		options["baseUrl"], _ = json.Marshal(filepath.Join(filepath.Dir(value), baseUrl))
		config["compilerOptions"], _ = json.Marshal(options)
	}
	return config, nil
}

// mergeTsconfigs is tsconfig-paths' mergeTsconfigs: the extending file's keys over the base's, and
// its compilerOptions over the base's compilerOptions, each replaced whole.
func mergeTsconfigs(base, config tsconfigFile) tsconfigFile {
	merged := tsconfigFile{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range config {
		merged[key] = value
	}
	options := base.compilerOptions()
	for key, value := range config.compilerOptions() {
		options[key] = value
	}
	merged["compilerOptions"], _ = json.Marshal(options)
	return merged
}

// stripJsonComments removes `//` and `/* */` comments outside strings, and a comma that is followed
// only by whitespace and comments before a closing bracket or brace.
func stripJsonComments(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	inString, escaped := false, false
	for index := 0; index < len(text); index++ {
		character := text[index]
		if inString {
			builder.WriteByte(character)
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == '"':
				inString = false
			}
			continue
		}
		switch {
		case character == '"':
			inString = true
			builder.WriteByte(character)
		case character == '/' && index+1 < len(text) && text[index+1] == '/':
			for index < len(text) && text[index] != '\n' {
				index++
			}
			builder.WriteByte('\n')
		case character == '/' && index+1 < len(text) && text[index+1] == '*':
			end := strings.Index(text[index+2:], "*/")
			if end < 0 {
				index = len(text)
				continue
			}
			index += end + 3
		case character == ',':
			if next := nextSignificant(text, index+1); next == ']' || next == '}' {
				continue
			}
			builder.WriteByte(character)
		default:
			builder.WriteByte(character)
		}
	}
	return builder.String()
}

// nextSignificant is the first byte after start that is neither whitespace nor inside a comment.
func nextSignificant(text string, start int) byte {
	for index := start; index < len(text); index++ {
		switch character := text[index]; {
		case character == ' ' || character == '\t' || character == '\n' || character == '\r':
		case character == '/' && index+1 < len(text) && text[index+1] == '/':
			for index < len(text) && text[index] != '\n' {
				index++
			}
		case character == '/' && index+1 < len(text) && text[index+1] == '*':
			end := strings.Index(text[index+2:], "*/")
			if end < 0 {
				return 0
			}
			index += end + 3
		default:
			return character
		}
	}
	return 0
}

// resolve answers a stylesheet specifier the way the plugin does before any other resolution, or
// reports that no mapping found an existing file, which hands the specifier to ordinary resolution.
//
// Each mapping that matches offers its substitutions; each substitution is tried as written, then with
// each of the plugin's extensions, then as a package whose package.json names a `style`, then as a
// directory index with each extension. A hit on the path as written is the file. A hit with an
// extension or as an index stands for the path without it, which enhanced-resolve's stylesheet resolver
// then reads as a stylesheet: as written, with `.css`, or as a package or an index.
func (mapping *tsconfigPaths) resolve(specifier string, readFile func(path string) (string, bool), fileExists func(path string) bool) (string, bool) {
	if mapping == nil || specifier == "" || specifier[0] == '.' {
		return "", false
	}
	for _, entry := range mapping.mappings {
		starMatch, matches := "", entry.pattern == specifier
		if !matches {
			starMatch, matches = tsconfigMatchStar(entry.pattern, specifier)
		}
		if !matches {
			continue
		}
		for _, substitution := range entry.paths {
			physical := strings.Replace(substitution, "*", starMatch, 1)
			if fileExists(physical) {
				return physical, true
			}
			for _, extension := range tsconfigExtensions {
				if fileExists(physical + extension) {
					return resolveStylesheetPath(physical, readFile, fileExists)
				}
			}
			if main, found := packageStyle(physical, readFile, fileExists); found {
				return main, true
			}
			index := filepath.Join(physical, "index")
			for _, extension := range tsconfigExtensions {
				if fileExists(index + extension) {
					return resolveStylesheetPath(index, readFile, fileExists)
				}
			}
		}
	}
	return "", false
}

// tsconfigMatchStar is tsconfig-paths' matchStar, including its `substr(start, length)`: the text
// after the prefix is taken with the suffix's start as its length, which for a pattern ending in its
// star is everything after the prefix.
func tsconfigMatchStar(pattern, search string) (string, bool) {
	if len(search) < len(pattern) {
		return "", false
	}
	if pattern == "*" {
		return search, true
	}
	star := strings.IndexByte(pattern, '*')
	if star < 0 {
		return "", false
	}
	prefix, suffix := pattern[:star], pattern[star+1:]
	if search[:star] != prefix || search[len(search)-len(suffix):] != suffix {
		return "", false
	}
	end := min(star+len(search)-len(suffix), len(search))
	return search[star:end], true
}

// packageStyle is the file a directory's package.json names in `style`, the one main field the
// plugin is given for stylesheets.
func packageStyle(directory string, readFile func(path string) (string, bool), fileExists func(path string) bool) (string, bool) {
	text, isRead := readFile(filepath.Join(directory, "package.json"))
	if !isRead {
		return "", false
	}
	// A map rather than a struct: a struct field matches `Style` and `STYLE` too, and the plugin reads
	// the key `style` exactly.
	var manifest map[string]json.RawMessage
	var style string
	if json.Unmarshal([]byte(text), &manifest) != nil || json.Unmarshal(manifest["style"], &style) != nil || style == "" {
		return "", false
	}
	main := filepath.Join(directory, style)
	if fileExists(main) {
		return main, true
	}
	return "", false
}

// resolveStylesheetPath is enhanced-resolve's stylesheet resolver on an absolute path: the file, the
// file with `.css`, the directory's package `style`, or its `index.css`.
func resolveStylesheetPath(path string, readFile func(path string) (string, bool), fileExists func(path string) bool) (string, bool) {
	for _, candidate := range []string{path, path + ".css"} {
		if fileExists(candidate) {
			return candidate, true
		}
	}
	if main, found := packageStyle(path, readFile, fileExists); found {
		return main, true
	}
	if index := filepath.Join(path, "index.css"); fileExists(index) {
		return index, true
	}
	return "", false
}

// tsconfigStylesheetResolver puts a tsconfig's mapping in front of a resolver, as the plugin sits in
// front of enhanced-resolve's own lookup.
func tsconfigStylesheetResolver(mapping *tsconfigPaths, next tailwindengine.StylesheetResolver, readFile func(path string) (string, bool), fileExists func(path string) bool) tailwindengine.StylesheetResolver {
	if mapping == nil {
		return next
	}
	return func(specifier, base string) (string, error) {
		if resolved, found := mapping.resolve(specifier, readFile, fileExists); found {
			return resolved, nil
		}
		return next(specifier, base)
	}
}
