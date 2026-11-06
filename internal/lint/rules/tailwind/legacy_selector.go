package tailwind

import (
	"encoding/json"
	"fmt"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// LegacySelector is one entry of upstream's legacy `attributes`, `callees`, `tags` or `variables`
// options: a name pattern, or a name pattern and its matchers, `["cva", [{"match": "objectValues",
// "pathPattern": "^variants"}]]`.
type LegacySelector struct {
	Name string
	// Match is nil for a bare name, which reads direct strings only, as a flat selector with no matchers.
	Match []SelectorMatcher
}

// UnmarshalJSON reads a legacy entry. A custom decoder decodes with its own json.Unmarshal, which
// would accept a misspelled key the options decoder refuses, so the matchers are decoded through
// rule.UnmarshalOptions here.
func (legacy *LegacySelector) UnmarshalJSON(raw []byte) error {
	var name string
	if decodeError := json.Unmarshal(raw, &name); decodeError == nil {
		*legacy = LegacySelector{Name: name}
		return nil
	}
	var pair []json.RawMessage
	if decodeError := json.Unmarshal(raw, &pair); decodeError != nil || len(pair) != 2 {
		return fmt.Errorf("a selector is a name, or a name and its matchers, not %s", raw)
	}
	if decodeError := json.Unmarshal(pair[0], &name); decodeError != nil {
		return fmt.Errorf("a selector's name is a string, not %s", pair[0])
	}
	var matchers []legacyMatcher
	if unmarshalOptionsError := rule.UnmarshalOptions(pair[1], &matchers); unmarshalOptionsError != nil {
		return fmt.Errorf("selector %q's matchers: %w", name, unmarshalOptionsError)
	}
	match := make([]SelectorMatcher, 0, len(matchers))
	for _, matcher := range matchers {
		switch matcher.Match {
		case MatcherTypeStrings, MatcherTypeObjectKeys, MatcherTypeObjectValues:
		default:
			return fmt.Errorf(
				"selector %q: a matcher is strings, objectKeys or objectValues, not %q",
				name,
				matcher.Match,
			)
		}
		// upstream's toSelectorMatcher: a strings matcher drops its path.
		if matcher.Match == MatcherTypeStrings {
			match = append(match, SelectorMatcher{Type: matcher.Match})
			continue
		}
		match = append(match, SelectorMatcher{Type: matcher.Match, Path: matcher.PathPattern})
	}
	*legacy = LegacySelector{Name: name, Match: match}
	return nil
}

// MarshalJSON writes a legacy entry back in its upstream form.
func (legacy LegacySelector) MarshalJSON() ([]byte, error) {
	if legacy.Match == nil {
		return json.Marshal(legacy.Name)
	}
	matchers := make([]legacyMatcher, 0, len(legacy.Match))
	for _, matcher := range legacy.Match {
		matchers = append(matchers, legacyMatcher{Match: matcher.Type, PathPattern: matcher.Path})
	}
	return json.Marshal([]any{legacy.Name, matchers})
}

// selector is upstream's migrateLegacySelector: a legacy entry as the flat selector it stands for. A
// callee's or tag's name is also its path, so it matches a call by either.
func (legacy LegacySelector) selector(kind SelectorKind) Selector {
	selector := Selector{Kind: kind, Name: legacy.Name, Match: legacy.Match}
	if kind == SelectorKindCallee || kind == SelectorKindTag {
		selector.Path = legacy.Name
	}
	return selector
}
