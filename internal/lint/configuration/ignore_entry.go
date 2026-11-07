package configuration

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// IgnoreEntry is one entry of `ignorePatterns`: a glob, and, when its author wrote the object form, why the
// paths it matches are never checked, what they are, and which check still reads them.
//
// A plain string is still an entry, its pattern alone, legal outside our tiers (#cxeq6jw). Inside them, and in
// every set cohere carries, an entry says why, as an off and a departure do: an ignore that says nothing reads
// as an allowance, and nobody reading a run can tell a ruling from a directory somebody wanted quiet.
type IgnoreEntry struct {
	Pattern string
	Reason  string
	Kind    IgnoreKind
	Check   IgnoreCheck

	// File is the settings file or set that wrote the entry, set by the loader rather than read from the entry.
	File string
}

// IgnoreKind says what an ignored path is.
type IgnoreKind string

const (
	// IgnoreKindInput is data a program reads, kept as it was given: fixtures, records, exports.
	IgnoreKindInput IgnoreKind = "input"

	// IgnoreKindGenerated is written by a tool, which owns its shape.
	IgnoreKindGenerated IgnoreKind = "generated"

	// IgnoreKindThirdParty is somebody else's code, vendored or pinned.
	IgnoreKindThirdParty IgnoreKind = "thirdParty"
)

// IgnoreKinds are the kinds an entry may name, in the order the schema lists them.
var IgnoreKinds = []IgnoreKind{IgnoreKindInput, IgnoreKindGenerated, IgnoreKindThirdParty}

// IgnoreCheck names a check that still reads an ignored path.
type IgnoreCheck string

// IgnoreCheckTypes names the type check as one that still reads an ignored path. The loader accepts and carries
// it, and nothing acts on it yet: the type check's reading of it is the next piece of #cxeq6jw.
const IgnoreCheckTypes IgnoreCheck = "types"

// IgnoreChecks are the checks an entry may name.
var IgnoreChecks = []IgnoreCheck{IgnoreCheckTypes}

// ignoreEntryKeys are the keys the object form may carry. Anything else is refused for the reason
// checkTopLevelKeys gives: `encoding/json` would drop it without a word.
var ignoreEntryKeys = map[string]bool{"pattern": true, "reason": true, "kind": true, "check": true}

// UnmarshalJSON reads either form: a string is the pattern alone, and an object is checked key by key, so a
// misspelled key, an unknown kind, a missing pattern or a blank reason is refused at load rather than read as
// an entry that says less than its author wrote.
func (entry *IgnoreEntry) UnmarshalJSON(data []byte) error {
	var pattern string
	if decodeError := json.Unmarshal(data, &pattern); decodeError == nil {
		*entry = IgnoreEntry{Pattern: pattern}
		return nil
	}
	var keyed map[string]json.RawMessage
	if decodeError := json.Unmarshal(data, &keyed); decodeError != nil || keyed == nil {
		return fmt.Errorf("an \"ignorePatterns\" entry is a glob, or an object with \"pattern\" and \"reason\", "+
			"got %s", truncate(string(data)))
	}
	var unknown []string
	for key := range keyed {
		if !ignoreEntryKeys[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("an \"ignorePatterns\" entry declares %s, which this loader does not read: the key would "+
			"be discarded silently. An entry takes \"pattern\", \"reason\", \"kind\" and \"check\"",
			strings.Join(quoteEach(unknown), ", "))
	}

	var decoded struct {
		Pattern *string      `json:"pattern"`
		Reason  *string      `json:"reason"`
		Kind    *IgnoreKind  `json:"kind"`
		Check   *IgnoreCheck `json:"check"`
	}
	if decodeError := json.Unmarshal(data, &decoded); decodeError != nil {
		return fmt.Errorf("an \"ignorePatterns\" entry %s: %w", truncate(string(data)), decodeError)
	}
	if decoded.Pattern == nil || *decoded.Pattern == "" {
		return fmt.Errorf("an \"ignorePatterns\" entry %s names no \"pattern\": say which paths it ignores, as a glob",
			truncate(string(data)))
	}
	*entry = IgnoreEntry{Pattern: *decoded.Pattern}
	if decoded.Reason != nil {
		// Go whitespace: a reason in cohere's own lint config, which no JavaScript tool reads.
		entry.Reason = strings.TrimSpace(*decoded.Reason)
		if entry.Reason == "" {
			return fmt.Errorf("the \"ignorePatterns\" entry for %q gives an empty \"reason\": say why these paths "+
				"are never checked, in a sentence, or write the pattern alone", entry.Pattern)
		}
	}
	if decoded.Kind != nil {
		if !knownIgnoreKind(*decoded.Kind) {
			return fmt.Errorf("the \"ignorePatterns\" entry for %q names the kind %q: a kind is one of %s",
				entry.Pattern, *decoded.Kind, strings.Join(quoteEach(ignoreKindNames()), ", "))
		}
		entry.Kind = *decoded.Kind
	}
	if decoded.Check != nil {
		if *decoded.Check != IgnoreCheckTypes {
			return fmt.Errorf("the \"ignorePatterns\" entry for %q names the check %q: the one check an ignored "+
				"path can still be read by is %q", entry.Pattern, *decoded.Check, IgnoreCheckTypes)
		}
		entry.Check = *decoded.Check
	}
	return nil
}

func knownIgnoreKind(kind IgnoreKind) bool {
	for _, known := range IgnoreKinds {
		if kind == known {
			return true
		}
	}
	return false
}

func ignoreKindNames() []string {
	names := make([]string, 0, len(IgnoreKinds))
	for _, kind := range IgnoreKinds {
		names = append(names, string(kind))
	}
	return names
}
