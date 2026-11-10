// Package settings reads which of the Go engine's rules run, and how strictly, from the `go` block of
// `CohereSettings.json`:
//
//	{ "go": { "rules": { "cohere-go/performance-require-parallel-test": "error" } } }
//
// A value is a severity (`error`, `warn`, `warning` or `off`) or an array whose first element is one and
// whose optional second element is the rule's options object, the shape the Swift and TypeScript configs
// use. Severities and options only.
//
// Beside rules, the block may hold departures, a reason for each rule the project sets looser than the house does:
//
//	{ "go": {
//		"rules": { "cohere-go/consistency-no-long-file": ["error", {"max": 3000}] },
//		"departures": { "cohere-go/consistency-no-long-file": "Our parser tables are one generated unit." }
//	} }
//
// Which options loosen a rule is the rule's to say, so this layer holds a departure to naming a known rule with a
// reason that isn't blank, and the engine refuses a loosening option without one, and one that loosens nothing.
//
// And reasons, why the block turns each of its offs off, in a sentence, as the TypeScript block's `reasons` says it:
//
//	{ "go": {
//		"rules": { "cohere-go/consistency-no-utils-file-name": "off" },
//		"reasons": { "cohere-go/consistency-no-utils-file-name": "The IR's util.go splits in #2xt6v5r's step 3." }
//	} }
//
// Inside our tiers every off owes its reason, and a block that turns a rule off without one is refused at load. A Go
// root is inside them when a module it governs is ours by its path (internal/our_tiers): Go has no set to extend, so
// it is the module that says whose code it is (#v11k0g2). Outside them an off is the project's to make, with or
// without a reason. Either way coverage prints the reason beside the rule, and an entry that explains nothing, blank,
// for a rule the block does not turn off, or beside a departure for the same rule, is refused.
//
// When the root has no config, every house rule runs at error. That is not the permissive default the
// TypeScript loader refuses, because every rule runs, and the note says so on every run, so a missing
// config never reads as a configured clean tree. A config that exists and cannot be read is an error, never
// a fallback to the defaults, and so is a rule name the engine does not know: a misspelled rule would
// configure nothing while the file looked configured.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/our_tiers"
)

// Severity is how strongly a rule's findings count, spelled as the contract prints it.
type Severity string

const (
	// SeverityError reports and fails the run.
	SeverityError Severity = "error"

	// SeverityWarning reports without failing the run.
	SeverityWarning Severity = "warning"

	// SeverityOff means the rule runs on no file.
	SeverityOff Severity = "off"
)

// SeveritySpellings are the severities parseSeverity accepts, exactly as written, for the settings schema
// (internal/settings_schema) to read rather than copy, so the schema never allows a spelling the engine
// refuses. TestTheSeveritySpellingsAreTheLoaders holds the list to parseSeverity.
var SeveritySpellings = []string{"error", "warn", "warning", "off"}

// RuleSetting is one rule's severity and the options the config gave it.
type RuleSetting struct {
	Severity Severity

	// Options is the object the config wrote after the severity, as written, and nil when it wrote none.
	// Which keys a rule reads is the rule's business; this layer only holds the shape to one object.
	Options json.RawMessage
}

// Settings is what every rule the engine knows runs at.
type Settings struct {
	// Rules holds every known rule: what the config says, or error with no options for a rule it does not
	// mention.
	Rules map[string]RuleSetting

	// Departures maps a rule to the reason the project gives for setting it looser than the house does, from the
	// go block's departures, and is empty when it gives none.
	Departures map[string]string

	// OffReasons maps each rule the go block turns off to why, from its reasons, and is empty when it gives none.
	OffReasons map[string]string

	// Targets are the go block's targets, each `GOOS/GOARCH`, which build selection checks beside the platforms the
	// project's files name (#ntamp9e), in the order written, and empty when it gives none.
	Targets []string

	// Note is empty when a go block was read. Otherwise it is the sentence the lint record carries as
	// `configNote`, naming the defaults in force and where a config would go.
	Note string
}

// settingsFileName is the file the engine reads beside go.work or go.mod when no path is named.
const settingsFileName = "CohereSettings.json"

// Path is the settings file Load reads for the engine rooted at root: explicitPath when the front door named one
// with `--lint-config`, or else the CohereSettings.json beside go.work or go.mod, there or not. The engine's
// inputs record names it, so a replay sees the file appear, change or go.
func Path(root string, explicitPath string) string {
	if explicitPath != "" {
		return explicitPath
	}
	return filepath.Join(root, settingsFileName)
}

// Load reads the settings for the engine rooted at root, from the file Path names. knownRules is every rule name
// the engine runs, and a key outside it is refused. modulePaths are the modules the root governs
// (project.ModulePaths): when one is ours, every off the block makes must say why.
func Load(root string, explicitPath string, knownRules []string, modulePaths []string) (Settings, error) {
	path := Path(root, explicitPath)
	contents, readError := os.ReadFile(path)
	if errors.Is(readError, fs.ErrNotExist) {
		if explicitPath != "" {
			return Settings{}, fmt.Errorf("reading %s: no such file, and a config named by flag is never replaced "+
				"by the defaults", path)
		}
		return defaults(knownRules, fmt.Sprintf("config: no %s beside %s, so every house rule ran at error; "+
			"put one at %s to change that", settingsFileName, rootManifest(root), path)), nil
	}
	if readError != nil {
		return Settings{}, fmt.Errorf("reading %s: %w", path, readError)
	}
	return parse(contents, path, knownRules, modulePaths)
}

// rootManifest is the file that makes root a Go root, so the note says where a config would sit: go.work for
// a workspace, and go.mod for a module on its own.
func rootManifest(root string) string {
	if _, statError := os.Stat(filepath.Join(root, "go.work")); statError == nil {
		return "go.work"
	}
	return "go.mod"
}

// defaults runs every known rule at error, with the note that says so.
func defaults(knownRules []string, note string) Settings {
	rules := make(map[string]RuleSetting, len(knownRules))
	for _, name := range knownRules {
		rules[name] = RuleSetting{Severity: SeverityError}
	}
	return Settings{Rules: rules, Note: note}
}

// parse reads a settings file's go block. Every other top-level key, `format` among them, belongs to the
// TypeScript engine in a repository that holds both, so it is left alone here.
//
// Each level decodes into a map of raw values first, the way the TypeScript loader checks its keys, because
// `encoding/json` drops a key a struct does not name without a word, and a dropped key is a setting that
// looks chosen and does nothing.
func parse(contents []byte, path string, knownRules []string, modulePaths []string) (Settings, error) {
	var document map[string]json.RawMessage
	if decodeError := json.Unmarshal(contents, &document); decodeError != nil {
		var typeError *json.UnmarshalTypeError
		if errors.As(decodeError, &typeError) {
			return Settings{}, fmt.Errorf("reading %s: the top level is not an object", path)
		}
		return Settings{}, fmt.Errorf("parsing %s: %w", path, decodeError)
	}
	// A top level of `null` decodes into a nil map with no error.
	if document == nil {
		return Settings{}, fmt.Errorf("reading %s: the top level is not an object", path)
	}

	rawBlock, hasBlock := document["go"]
	if !hasBlock {
		return defaults(knownRules, fmt.Sprintf("config: %s has no go block, so every house rule ran at error",
			path)), nil
	}
	block, isObject := decodeObject(rawBlock)
	if !isObject {
		return Settings{}, fmt.Errorf("reading %s: the go block is not an object", path)
	}
	if _, hasFormat := block["format"]; hasFormat {
		return Settings{}, fmt.Errorf("reading %s: the go block's \"format\" configures formatting, and Go's house "+
			"format is gofmt, which no project's settings change; remove the key", path)
	}
	var unknownKeys []string
	for key := range block {
		if key != "rules" && key != "departures" && key != "reasons" && key != "targets" {
			unknownKeys = append(unknownKeys, fmt.Sprintf("%q", key))
		}
	}
	if len(unknownKeys) > 0 {
		sort.Strings(unknownKeys)
		return Settings{}, fmt.Errorf("reading %s: the go block declares %s, which the Go engine does not read, so "+
			"the key would configure nothing. The go block takes \"rules\", \"departures\", \"reasons\" and "+
			"\"targets\"", path,
			strings.Join(unknownKeys, ", "))
	}
	rawRules, hasRules := block["rules"]
	if !hasRules {
		return Settings{}, fmt.Errorf("reading %s: the go block has no rules object", path)
	}
	rules, isObject := decodeObject(rawRules)
	if !isObject {
		return Settings{}, fmt.Errorf("reading %s: the go block's rules is not an object", path)
	}

	known := make(map[string]bool, len(knownRules))
	for _, name := range knownRules {
		known[name] = true
	}
	// Sorted, so a file with two bad entries names the same one on every run.
	names := make([]string, 0, len(rules))
	for name := range rules {
		names = append(names, name)
	}
	sort.Strings(names)

	settings := defaults(knownRules, "")
	for _, name := range names {
		if !known[name] {
			return Settings{}, fmt.Errorf("reading %s: the go block names %q, which is not a rule the Go engine "+
				"knows, so the entry would configure nothing", path, name)
		}
		setting, settingError := parseRuleSetting(rules[name])
		if settingError != nil {
			return Settings{}, fmt.Errorf("reading %s: rule %q %w", path, name, settingError)
		}
		settings.Rules[name] = setting
	}

	if rawDepartures, hasDepartures := block["departures"]; hasDepartures {
		departures, departuresError := parseDepartures(rawDepartures, known)
		if departuresError != nil {
			return Settings{}, fmt.Errorf("reading %s: %w", path, departuresError)
		}
		settings.Departures = departures
	}
	if rawReasons, hasReasons := block["reasons"]; hasReasons {
		offReasons, reasonsError := parseOffReasons(rawReasons, known, settings)
		if reasonsError != nil {
			return Settings{}, fmt.Errorf("reading %s: %w", path, reasonsError)
		}
		settings.OffReasons = offReasons
	}
	if rawTargets, hasTargets := block["targets"]; hasTargets {
		targets, targetsError := parseTargets(rawTargets)
		if targetsError != nil {
			return Settings{}, fmt.Errorf("reading %s: %w", path, targetsError)
		}
		settings.Targets = targets
	}
	if unreasonedError := refuseUnreasonedOffs(settings, names, modulePaths); unreasonedError != nil {
		return Settings{}, fmt.Errorf("reading %s: %w", path, unreasonedError)
	}
	return settings, nil
}

// parseOffReasons reads the go block's reasons, each a rule the block turns off and why, in a sentence. An entry that
// explains nothing is refused, so the list cannot rot: one for a rule the engine does not know, one whose reason is
// blank or not a string, one for a rule the block does not turn off in its own rules, and one beside a departure for
// the same rule, which already says why. settings holds the block's rules and departures, read already.
func parseOffReasons(value json.RawMessage, known map[string]bool, settings Settings) (map[string]string, error) {
	entries, isObject := decodeObject(value)
	if !isObject {
		return nil, fmt.Errorf("the go block's reasons is %s, which is not an object of each off's reason", value)
	}
	// Sorted, so a file with two bad entries names the same one on every run.
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	offReasons := make(map[string]string, len(entries))
	for _, name := range names {
		if !known[name] {
			return nil, fmt.Errorf("the go block's reasons name %q, which is not a rule the Go engine knows, so the "+
				"reason would explain nothing", name)
		}
		var reason string
		if jsonKind(entries[name]) != '"' || json.Unmarshal(entries[name], &reason) != nil {
			return nil, fmt.Errorf("the reason for %q is %s, which is not a sentence written as a string", name,
				entries[name])
		}
		if strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("the go block's reasons name %q and give no reason: say why the rule is off, in "+
				"a sentence", name)
		}
		if settings.Rules[name].Severity != SeverityOff {
			return nil, fmt.Errorf("the go block's reasons name %q, which its rules do not turn off: a reason "+
				"belongs to the off it explains, so remove the entry", name)
		}
		if _, departs := settings.Departures[name]; departs {
			return nil, fmt.Errorf("the go block names %q under both \"reasons\" and \"departures\": the "+
				"departure's reason already says why, so remove the \"reasons\" entry", name)
		}
		offReasons[name] = reason
	}
	return offReasons, nil
}

// refuseUnreasonedOffs refuses a block that turns a rule off with no reason, when a module the root governs is ours
// (internal/our_tiers). An off with no reason reads as a decision while recording none, and inside our tiers every
// off owes its why, as the TypeScript loader holds it (#v11k0g2). written are the rules the block names, sorted.
func refuseUnreasonedOffs(settings Settings, written []string, modulePaths []string) error {
	ours := ""
	for _, modulePath := range modulePaths {
		if our_tiers.OwnsModule(modulePath) {
			ours = modulePath
			break
		}
	}
	if ours == "" {
		return nil
	}
	var unreasoned []string
	for _, name := range written {
		if settings.Rules[name].Severity != SeverityOff {
			continue
		}
		if _, reasoned := settings.OffReasons[name]; !reasoned {
			unreasoned = append(unreasoned, fmt.Sprintf("%q", name))
		}
	}
	if len(unreasoned) == 0 {
		return nil
	}
	return fmt.Errorf("the go block turns off %s with no reason, and %s is one of our modules (%s), where every off "+
		"says why: add a sentence for each under the go block's \"reasons\"", strings.Join(unreasoned, ", "), ours,
		our_tiers.ModulePrefix)
}

// parseDepartures reads the go block's departures, each a known rule and the reason the project loosens it. A
// departure for a rule the engine does not know would exempt nothing while the file looked reasoned, and a blank
// reason is a departure that says nothing, so both are refused. Whether the rule's options loosen anything is the
// engine's to check, since only the rule knows.
func parseDepartures(value json.RawMessage, known map[string]bool) (map[string]string, error) {
	departures, isObject := decodeObject(value)
	if !isObject {
		return nil, fmt.Errorf("the go block's departures is %s, which is not an object of each rule's reason", value)
	}
	// Sorted, so a file with two bad departures names the same one on every run.
	names := make([]string, 0, len(departures))
	for name := range departures {
		names = append(names, name)
	}
	sort.Strings(names)
	reasons := make(map[string]string, len(departures))
	for _, name := range names {
		if !known[name] {
			return nil, fmt.Errorf("the go block's departures name %q, which is not a rule the Go engine knows, so "+
				"the departure would exempt nothing", name)
		}
		var reason string
		if jsonKind(departures[name]) != '"' || json.Unmarshal(departures[name], &reason) != nil {
			return nil, fmt.Errorf("the departure for %q is %s, which is not a reason written as a string", name,
				departures[name])
		}
		if strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("the departure for %q gives a blank reason, and a departure must say why the "+
				"project loosens the rule", name)
		}
		reasons[name] = reason
	}
	return reasons, nil
}

// parseRuleSetting reads a rule's value: a bare severity, or `[severity]`, or `[severity, {options}]`.
//
// The value's kind is read from its first byte rather than by trying a decode into each shape, because
// `null` decodes into a Go string as the empty string with no error, and would then be reported as a
// severity of "" rather than as the null it is.
func parseRuleSetting(value json.RawMessage) (RuleSetting, error) {
	kind := jsonKind(value)
	if kind == '"' {
		severity, severityError := parseSeverity(value)
		return RuleSetting{Severity: severity}, severityError
	}
	if kind != '[' {
		return RuleSetting{}, fmt.Errorf("is %s, which is neither a severity nor a [severity, options] array", value)
	}

	var elements []json.RawMessage
	if decodeError := json.Unmarshal(value, &elements); decodeError != nil {
		return RuleSetting{}, fmt.Errorf("decoding its array: %w", decodeError)
	}
	if len(elements) == 0 {
		return RuleSetting{}, errors.New("is an empty array, and the first element must be its severity")
	}
	if len(elements) > 2 {
		return RuleSetting{}, fmt.Errorf("has %d elements, and a rule takes a severity and at most one options "+
			"object, so the rest would never be read", len(elements))
	}
	severity, severityError := parseSeverity(elements[0])
	if severityError != nil {
		return RuleSetting{}, severityError
	}
	setting := RuleSetting{Severity: severity}
	if len(elements) == 2 {
		if jsonKind(elements[1]) != '{' {
			return RuleSetting{}, fmt.Errorf("has options %s, which is not an object", elements[1])
		}
		setting.Options = elements[1]
	}
	return setting, nil
}

// parseSeverity accepts the four spellings the TypeScript loader shares with this one, exactly, and refuses
// the rest by name: a severity read as some default would turn a rule off, or on, with nothing reporting it.
func parseSeverity(value json.RawMessage) (Severity, error) {
	var spelled string
	if jsonKind(value) == '"' && json.Unmarshal(value, &spelled) == nil {
		switch spelled {
		case "error":
			return SeverityError, nil
		case "warn", "warning":
			return SeverityWarning, nil
		case "off":
			return SeverityOff, nil
		}
	}
	return "", fmt.Errorf("has severity %s, which is not error, warn, warning or off", value)
}

// decodeObject decodes a raw value that must be a JSON object, reporting false for any other kind, `null`
// included.
func decodeObject(value json.RawMessage) (map[string]json.RawMessage, bool) {
	if jsonKind(value) != '{' {
		return nil, false
	}
	var object map[string]json.RawMessage
	if decodeError := json.Unmarshal(value, &object); decodeError != nil {
		return nil, false
	}
	return object, true
}

// jsonKind is the first byte of a raw value already known to be valid JSON, which names its kind: '{', '[',
// '"', or the first letter or digit of a literal. Zero for an empty value.
func jsonKind(value json.RawMessage) byte {
	trimmed := bytes.TrimLeft(value, " \t\r\n")
	if len(trimmed) == 0 {
		return 0
	}
	return trimmed[0]
}

// TargetPattern is a target as go tool dist list spells one, `GOOS/GOARCH`, each a word of lowercase letters and
// digits, for the settings schema (internal/settings_schema) to read rather than copy.
const TargetPattern = `^[a-z0-9]+/[a-z0-9]+$`

// targetPattern is TargetPattern, compiled.
var targetPattern = regexp.MustCompile(TargetPattern)

// parseTargets reads the go block's targets (#ntamp9e, Kirk's ruling, Nov 10): the platforms, `GOOS/GOARCH`, the
// project is checked under beside the ones its files name. A target only ever adds to them, so an entry that reads
// as taking one away, `!windows/arm64` or `-windows/arm64`, is refused, and so is one not spelled as a pair, a
// duplicate, and an entry that is not a string. Whether the toolchain targets the pair is the engine's to check,
// against go tool dist list, when it selects the builds.
func parseTargets(value json.RawMessage) ([]string, error) {
	var entries []json.RawMessage
	if jsonKind(value) != '[' || json.Unmarshal(value, &entries) != nil {
		return nil, fmt.Errorf("the go block's targets is %s, which is not a list of `GOOS/GOARCH` pairs", value)
	}
	targets := make([]string, 0, len(entries))
	for _, entry := range entries {
		var target string
		if jsonKind(entry) != '"' || json.Unmarshal(entry, &target) != nil {
			return nil, fmt.Errorf("the go block's targets hold %s, which is not a `GOOS/GOARCH` pair written as a "+
				"string", entry)
		}
		if strings.HasPrefix(target, "!") || strings.HasPrefix(target, "-") {
			return nil, fmt.Errorf("the go block's targets hold %q, which reads as taking a platform away: targets "+
				"only add to the platforms the files name, so remove it", target)
		}
		if !targetPattern.MatchString(target) {
			return nil, fmt.Errorf("the go block's targets hold %q, which is not a `GOOS/GOARCH` pair as go tool "+
				"dist list spells one, such as \"wasip1/wasm\"", target)
		}
		if slices.Contains(targets, target) {
			return nil, fmt.Errorf("the go block's targets name %q twice", target)
		}
		targets = append(targets, target)
	}
	return targets, nil
}
