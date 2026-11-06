package configuration

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
)

// Configuration is a resolved lint configuration: what runs everywhere, what never runs, and what changes
// per path.
type Configuration struct {
	// Rules is the base configuration, applying to every file not excluded.
	Rules map[string]RuleSetting

	// IgnorePatterns are paths no rule runs on at all.
	IgnorePatterns []string

	// Overrides apply in order, later blocks winning over earlier ones, which is how both ESLint and
	// oxlint resolve them. Order is preserved rather than sorted for exactly this reason.
	Overrides []Override

	// Plugins are the namespaces the config declares, which contribute their own rules without any
	// rules block naming them. See PluginDefaultRules.
	Plugins []string

	// Root is the directory glob patterns resolve against: the directory of the file cohere was
	// pointed at, whichever file in its `extends` chain a pattern was written in.
	Root string

	// Sources is every file the configuration was read from, the one cohere was pointed at first and
	// then each file it extends, in order. Anything keyed on the configuration's bytes (the findings
	// cache, the run cache, the scoped-run widening guard) must read all of them: a base edited alone
	// changes what runs exactly as an edit to the project's own file does.
	Sources []string

	// Departures are the rules a file in the chain sets differently from a file it extends, each with
	// the reason that file gave, keyed by rule name. Coverage reports them, so a departure is always
	// visible and never becomes a quiet allowance.
	Departures map[string]Departure

	// OffReasons are why each rule the chain turns off at top level is off, keyed by rule name: the
	// sentence a file gave under `reasons`, or its departure's reason when the off departs from an
	// inherited ruling. Coverage prints each beside the rule. An off with no entry here is an allowance.
	OffReasons map[string]OffReason

	// CohereVersion is the range of cohere releases the project's own file accepts, or nil when it
	// pins none. Only the project's own file may pin: a set is read by every project that extends it,
	// and a range there would decide for all of them which release they run.
	CohereVersion *VersionRange

	// Settings is the project's own `settings`, keyed by plugin namespace, each value as written. Only
	// the project's own file may write it, for the reason only it may pin a release: a value in a set
	// would decide for every project extending it. Which namespaces a run reads, and which keys under
	// each, is the rule packages' to say (rule.RegisterSettings), checked before any rule decodes.
	Settings map[string]json.RawMessage

	// ruleWriters is the source that turned each rule key on or set it: the file or set that wrote it at
	// top level, the one that declared the plugin for a plugin default, or the one whose override names it.
	ruleWriters map[string]string

	// house is the per-file dispatch of a zero-config configuration, nil for one that names its sets.
	house *houseSets

	// resolutionMemo is Resolve's results by match, created on first use. See resolutionMemo.
	resolutionMemo atomic.Pointer[resolutionMemo]

	// compiledGlobs is IgnorePatterns and each override's Files, compiled on first use. See Config.globs.
	compiledGlobs atomic.Pointer[configurationGlobs]
}

// OffReasonFor returns why the rule named ruleName is off, found by its own key or by a key that reaches
// it, the way the resolver lets a key spelled differently configure a rule.
func (configuration *Configuration) OffReasonFor(ruleName string) (OffReason, bool) {
	if configuration == nil {
		return OffReason{}, false
	}
	if offReason, found := lookupByReach(configuration.OffReasons, ruleName); found {
		return offReason, true
	}
	// Under zero config, a rule a per-file set turns on is off wherever the set does not fit, and the set
	// says why. Asked only after the chain's own reasons, so a project's own off keeps its own.
	if configuration.house != nil && !configuration.TurnsOffAtTopLevel(ruleName) {
		return lookupByReach(configuration.house.gatedReasons, ruleName)
	}
	return OffReason{}, false
}

// TurnsOffAtTopLevel reports whether the chain's top-level rules turn the rule named ruleName off, by its
// own key or, when no key names it exactly, by a key that reaches it.
func (configuration *Configuration) TurnsOffAtTopLevel(ruleName string) bool {
	if configuration == nil {
		return false
	}
	if setting, found := configuration.Rules[ruleName]; found {
		return setting.Severity == SeverityOff
	}
	for key, setting := range configuration.Rules {
		if setting.Severity == SeverityOff && KeyReachesRule(key, ruleName) {
			return true
		}
	}
	return false
}

// RuleKeys returns every rule name the config mentions, in the base block or any override, at any
// severity, `off` included.
//
// A key is evidence the rule exists somewhere even when cohere has not ported it: someone wrote a
// decision about it. That is what lets a suppression naming an unported rule be told apart from one
// naming a rule that exists nowhere.
func (configuration *Configuration) RuleKeys() []string {
	if configuration == nil {
		return nil
	}
	keys := make([]string, 0, len(configuration.Rules))
	for key := range configuration.Rules {
		keys = append(keys, key)
	}
	for _, override := range configuration.Overrides {
		for key := range override.Rules {
			keys = append(keys, key)
		}
	}
	return keys
}

// globs returns the Config's compiled patterns, compiling them on first use. Two goroutines racing to
// compile build the same answer, and the first one stored wins.
func (configuration *Configuration) globs() *configurationGlobs {
	if compiled := configuration.compiledGlobs.Load(); compiled != nil {
		return compiled
	}
	compiled := &configurationGlobs{
		ignore:    make([]glob, len(configuration.IgnorePatterns)),
		overrides: make([][]glob, len(configuration.Overrides)),
	}
	for index, pattern := range configuration.IgnorePatterns {
		compiled.ignore[index] = compileGlob(pattern)
	}
	for index, override := range configuration.Overrides {
		compiled.overrides[index] = make([]glob, len(override.Files))
		for fileIndex, pattern := range override.Files {
			compiled.overrides[index][fileIndex] = compileGlob(pattern)
		}
	}
	configuration.compiledGlobs.CompareAndSwap(nil, compiled)
	return configuration.compiledGlobs.Load()
}

// resolutions returns the Config's memo, creating it on first use.
func (configuration *Configuration) resolutions() *resolutionMemo {
	if memo := configuration.resolutionMemo.Load(); memo != nil {
		return memo
	}
	configuration.resolutionMemo.CompareAndSwap(nil, &resolutionMemo{byMatch: map[string]Resolved{}})
	return configuration.resolutionMemo.Load()
}

// Resolve computes the effective configuration for one file path.
//
// Precedence is base rules first, then each matching override in order, later blocks winning. That
// is how both ESLint and oxlint resolve, and matching them exactly is the whole point: cohere cannot
// be diffed honestly against the gate it replaces while the two disagree about which rules were
// even supposed to run.
//
// Under zero config the file resolves through the variant of the house stack it gets (see house.go).
// That dispatch comes first, before the memo, so each variant memoizes its own resolutions.
//
// Resolutions are memoized by which overrides a path matched (see resolutionMemo), so the result is
// shared with every other path that matched the same ones.
func (configuration *Configuration) Resolve(path string) Resolved {
	if configuration.house != nil {
		return configuration.house.variantFor(path).Resolve(path)
	}
	// Split once, and asked of every compiled pattern.
	segments := splitPath(configuration.relativePath(path))
	globs := configuration.globs()

	for index, pattern := range globs.ignore {
		if pattern.matches(segments) {
			return Resolved{Ignored: true, IgnoredBy: configuration.IgnorePatterns[index]}
		}
	}

	// The match, as the indices of the overrides that apply, in order. On the stack for the common case
	// of a few dozen overrides, and looked up without converting it to a string, so a path whose match is
	// already memoized allocates nothing here.
	var matchStorage [64]byte
	match := matchStorage[:0]
	for index, files := range globs.overrides {
		if matchesAny(files, segments) {
			match = binary.AppendUvarint(match, uint64(index))
		}
	}

	memo := configuration.resolutions()
	memo.mutex.Lock()
	defer memo.mutex.Unlock()
	if resolved, found := memo.byMatch[string(match)]; found {
		return resolved
	}
	resolved := configuration.resolveMatch(match)
	memo.byMatch[string(match)] = resolved
	return resolved
}

// resolveMatch builds the resolution for one match: the base rules, then each override the match names,
// in order.
func (configuration *Configuration) resolveMatch(match []byte) Resolved {
	// The base map is copied rather than shared, because an override writes into the result and a
	// shared map would leak one file's overrides into every later file.
	effective := make(map[string]RuleSetting, len(configuration.Rules))
	for name, setting := range configuration.Rules {
		effective[name] = setting
	}

	for remaining := match; len(remaining) > 0; {
		index, width := binary.Uvarint(remaining)
		remaining = remaining[width:]
		override := configuration.Overrides[index]
		for name, setting := range override.Rules {
			// A bare severity re-states the rule's level and keeps its options, as ESLint does and as a
			// bare severity across `extends` already does. Dropping them changed what a rule checked on
			// every file the override matched (#na0hgjz).
			if setting.Options == nil {
				if inForce, configured := effective[name]; configured {
					setting.Options = inForce.Options
				}
			}
			effective[name] = setting
		}
	}

	return Resolved{Rules: effective, identity: &ResolutionIdentity{match: string(match)}}
}

// relativePath expresses a path the way the config's patterns are written: relative to the config
// file, slash-separated, with no leading dot.
//
// Getting this wrong is silent in the dangerous direction. An absolute path never matches
// `modules/**`, so every override quietly stops applying and the rules they scoped off come back,
// which is exactly the 336 findings this package exists to remove.
func (configuration *Configuration) relativePath(path string) string {
	normalized := filepath.ToSlash(path)

	if configuration.Root != "" && filepath.IsAbs(path) {
		if relative, relativePathError := filepath.Rel(configuration.Root, path); relativePathError == nil {
			normalized = filepath.ToSlash(relative)
		}
	}

	return strings.TrimPrefix(normalized, "./")
}

// InOurTiers reports whether this configuration's chain reaches one of our tier sets. See InOurTiers.
func (configuration *Configuration) InOurTiers() bool {
	return configuration != nil && InOurTiers(configuration.Sources)
}

// Applied is the house sets a zero-config run applied, and the ones it looked for and did not, with
// the evidence for each. Nil for a configuration that names its sets.
func (configuration *Configuration) Applied() []AppliedSet {
	if configuration == nil || configuration.house == nil {
		return nil
	}
	return configuration.house.applied
}

// SetsLine is the run's first line under zero config: each house set and why it applies or does not.
// Empty for a configuration that names its own sets, since its file already says.
func (configuration *Configuration) SetsLine() string {
	applied := configuration.Applied()
	if len(applied) == 0 {
		return ""
	}
	terms := make([]string, 0, len(applied))
	for _, set := range applied {
		if set.Applied {
			terms = append(terms, set.Name+" on "+set.Evidence)
			continue
		}
		terms = append(terms, set.Name+" not applied, "+set.Evidence)
	}
	return "sets: " + strings.Join(terms, "; ")
}

// DetectionFingerprint is a digest of which files each per-file set applied to and the Tailwind entry,
// for a cache keyed on what decides which rules run: a file can change sets without its own bytes
// changing, when another file starts importing next. Empty for a configuration that names its sets.
func (configuration *Configuration) DetectionFingerprint() string {
	if configuration == nil || configuration.house == nil {
		return ""
	}
	digest := sha256.New()
	for _, files := range []map[string]bool{
		configuration.house.detection.ReactFiles,
		configuration.house.detection.NextFiles,
	} {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(digest, "%d\n%s\n", len(names), strings.Join(names, "\n"))
	}
	fmt.Fprintf(digest, "tailwind %s\n", configuration.house.detection.TailwindEntryPoint)
	return fmt.Sprintf("%x", digest.Sum(nil))
}
