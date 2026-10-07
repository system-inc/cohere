package tailwind

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// Selector is better-tailwindcss 4.7.0's selector (options/schemas/selectors.js): where a rule reads
// class strings from, and with which matchers.
//
// The kind says which node names it: a JSX attribute, a call, a variable, or a tagged template. A
// selector with no Match reads only a direct string or template, the attribute's value, a call's
// argument, a variable's initializer. A selector with Match walks that value and reads what the
// matchers match, which is how cva's `variants` and cn's object keys are read.
//
// Kind and matcher type values are upstream's own spellings, since they are read from a config file
// written for upstream.
type Selector struct {
	Kind SelectorKind `json:"kind"`
	// Name matches the attribute's, call's, variable's or tag's name, and Path a call's or tag's
	// dotted path, `twc.div`. Both must match the whole name, upstream's matchesName.
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
	// Match is nil when the selector has no matchers, which reads direct strings only, and empty when
	// it has an empty list, which reads nothing.
	Match []SelectorMatcher `json:"match,omitempty"`
	// TargetCall picks which call of a curried chain is read, the first by default, and CallTarget is
	// its older spelling, read when TargetCall is not written. TargetArgument picks which of that call's
	// arguments are, all of them by default.
	TargetCall     *SelectorTarget `json:"targetCall,omitempty"`
	CallTarget     *SelectorTarget `json:"callTarget,omitempty"`
	TargetArgument *SelectorTarget `json:"targetArgument,omitempty"`
}

// SelectorKind is the node a selector names.
type SelectorKind string

const (
	SelectorKindAttribute SelectorKind = "attribute"
	SelectorKindCallee    SelectorKind = "callee"
	SelectorKindTag       SelectorKind = "tag"
	SelectorKindVariable  SelectorKind = "variable"
)

// SelectorMatcher is one matcher of a selector.
type SelectorMatcher struct {
	Type MatcherType `json:"type"`
	// Path filters an object key or value by its path in the object, `variants.size.sm`. Unlike a
	// selector's name it is a partial match, upstream's `RegExp.test`.
	Path string `json:"path,omitempty"`
	// Match is an anonymousFunctionReturn matcher's own matchers, run on what the function returns.
	Match []SelectorMatcher `json:"match,omitempty"`
}

// MatcherType is what a matcher reads.
type MatcherType string

const (
	// MatcherTypeStrings reads every string that is not an object key or value, nor in a position that
	// cannot be the class value: a comparison, a conditional's test, the left of a logical expression.
	MatcherTypeStrings MatcherType = "strings"
	// MatcherTypeObjectKeys reads object keys that are strings, `cn({'p-2': on})`.
	MatcherTypeObjectKeys MatcherType = "objectKeys"
	// MatcherTypeObjectValues reads strings in object values, cva's `variants`.
	MatcherTypeObjectValues MatcherType = "objectValues"
	// MatcherTypeAnonymousFunctionReturn reads what an anonymous function returns, twc's
	// `twc.div(() => '...')`.
	MatcherTypeAnonymousFunctionReturn MatcherType = "anonymousFunctionReturn"
)

// SelectorTarget is a call or argument target: all, first, last, or an index, negative from the end.
type SelectorTarget struct {
	Word  string
	Index int
}

// UnmarshalJSON reads a target as upstream's schema does: one of three words, or a number. A number
// that is not an integer is refused rather than read, since upstream indexes with it and crashes.
func (target *SelectorTarget) UnmarshalJSON(raw []byte) error {
	var word string
	if err := json.Unmarshal(raw, &word); err == nil {
		switch word {
		case "all", "first", "last":
			*target = SelectorTarget{Word: word}
			return nil
		}
		return fmt.Errorf("a target is all, first, last or a number, not %q", word)
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err != nil {
		return fmt.Errorf("a target is all, first, last or a number, not %s", raw)
	}
	if number != math.Trunc(number) || math.Abs(number) > 1<<31 {
		return fmt.Errorf("a target index must be an integer, not %s", raw)
	}
	*target = SelectorTarget{Index: int(number)}
	return nil
}

// MarshalJSON writes a target back in its upstream form, so a settings key built from selectors reads
// the same target the same way.
func (target SelectorTarget) MarshalJSON() ([]byte, error) {
	if target.Word != "" {
		return json.Marshal(target.Word)
	}
	return json.Marshal(target.Index)
}

// LegacySelector is one entry of upstream's legacy `attributes`, `callees`, `tags` or `variables`
// options: a name pattern, or a name pattern and its matchers, `["cva", [{"match": "objectValues",
// "pathPattern": "^variants"}]]`.
type LegacySelector struct {
	Name string
	// Match is nil for a bare name, which reads direct strings only, as a flat selector with no matchers.
	Match []SelectorMatcher
}

// legacyMatcher is a legacy entry's matcher, upstream's options/schemas/matchers.js.
type legacyMatcher struct {
	Match       MatcherType `json:"match"`
	PathPattern string      `json:"pathPattern,omitempty"`
}

// UnmarshalJSON reads a legacy entry. A custom decoder decodes with its own json.Unmarshal, which
// would accept a misspelled key the options decoder refuses, so the matchers are decoded through
// rule.UnmarshalOptions here.
func (legacy *LegacySelector) UnmarshalJSON(raw []byte) error {
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		*legacy = LegacySelector{Name: name}
		return nil
	}
	var pair []json.RawMessage
	if err := json.Unmarshal(raw, &pair); err != nil || len(pair) != 2 {
		return fmt.Errorf("a selector is a name, or a name and its matchers, not %s", raw)
	}
	if err := json.Unmarshal(pair[0], &name); err != nil {
		return fmt.Errorf("a selector's name is a string, not %s", pair[0])
	}
	var matchers []legacyMatcher
	if err := rule.UnmarshalOptions(pair[1], &matchers); err != nil {
		return fmt.Errorf("selector %q's matchers: %w", name, err)
	}
	match := make([]SelectorMatcher, 0, len(matchers))
	for _, matcher := range matchers {
		switch matcher.Match {
		case MatcherTypeStrings, MatcherTypeObjectKeys, MatcherTypeObjectValues:
		default:
			return fmt.Errorf("selector %q: a matcher is strings, objectKeys or objectValues, not %q", name, matcher.Match)
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

// defaultSelectorsJSON is upstream 4.7.0's DEFAULT_SELECTORS, written by
// tools/generate_class_literals from the installed package, never by hand.
//
//go:embed default_selectors.json
var defaultSelectorsJSON []byte

// DefaultSelectors is upstream's default selectors, decoded once.
var DefaultSelectors = sync.OnceValue(func() []Selector {
	var selectors []Selector
	decoder := json.NewDecoder(bytes.NewReader(defaultSelectorsJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selectors); err != nil {
		panic(fmt.Sprintf("default_selectors.json does not decode, so it was not written by tools/generate_class_literals: %v", err))
	}
	return selectors
})

// checkSelectors refuses a selector cohere would read differently from upstream: a kind or matcher
// type upstream has none of, or a callee or tag naming neither a name nor a path. Upstream does not
// check its settings against its schema and ignores these there; refused here, a setting cannot be
// accepted and do nothing.
func checkSelectors(selectors []Selector) error {
	for index, selector := range selectors {
		switch selector.Kind {
		case SelectorKindAttribute, SelectorKindVariable:
			if selector.Name == "" {
				return fmt.Errorf("selectors[%d]: a %s selector needs a name", index, selector.Kind)
			}
		case SelectorKindCallee, SelectorKindTag:
			if selector.Name == "" && selector.Path == "" {
				return fmt.Errorf("selectors[%d]: a %s selector needs a name or a path", index, selector.Kind)
			}
		default:
			return fmt.Errorf("selectors[%d]: the kind is attribute, callee, tag or variable, not %q", index, selector.Kind)
		}
		if selector.Kind != SelectorKindCallee && (selector.TargetCall != nil || selector.CallTarget != nil || selector.TargetArgument != nil) {
			return fmt.Errorf("selectors[%d]: only a callee selector takes a call or argument target", index)
		}
		if selector.Kind != SelectorKindCallee && selector.Kind != SelectorKindTag && selector.Path != "" {
			return fmt.Errorf("selectors[%d]: only a callee or tag selector takes a path", index)
		}
		if err := checkMatchers(selector.Match, true); err != nil {
			return fmt.Errorf("selectors[%d]: %w", index, err)
		}
	}
	return nil
}

func checkMatchers(matchers []SelectorMatcher, isTopLevel bool) error {
	for _, matcher := range matchers {
		switch matcher.Type {
		case MatcherTypeStrings:
			if matcher.Path != "" || matcher.Match != nil {
				return fmt.Errorf("a strings matcher takes neither a path nor matchers")
			}
		case MatcherTypeObjectKeys, MatcherTypeObjectValues:
			if matcher.Match != nil {
				return fmt.Errorf("an %s matcher takes no matchers", matcher.Type)
			}
		case MatcherTypeAnonymousFunctionReturn:
			if !isTopLevel {
				return fmt.Errorf("an anonymousFunctionReturn matcher cannot nest inside another")
			}
			if matcher.Path != "" {
				return fmt.Errorf("an anonymousFunctionReturn matcher takes no path")
			}
			if err := checkMatchers(matcher.Match, false); err != nil {
				return err
			}
		default:
			return fmt.Errorf("a matcher is strings, objectKeys, objectValues or anonymousFunctionReturn, not %q", matcher.Type)
		}
	}
	return nil
}

// mergeSelectors is upstream's createRule getOptions (utils/rule.js at 4.7.0): every legacy kind
// written becomes flat selectors, and drops every flat selector of its kind, the defaults' included.
// A kind not written keeps the flat selectors, the defaults when `selectors` is not written.
func mergeSelectors(selectors *[]Selector, attributes, callees, tags, variables []LegacySelector) []Selector {
	var merged []Selector
	for _, legacy := range attributes {
		merged = append(merged, legacy.selector(SelectorKindAttribute))
	}
	for _, legacy := range callees {
		merged = append(merged, legacy.selector(SelectorKindCallee))
	}
	for _, legacy := range tags {
		merged = append(merged, legacy.selector(SelectorKindTag))
	}
	for _, legacy := range variables {
		merged = append(merged, legacy.selector(SelectorKindVariable))
	}
	flat := DefaultSelectors()
	if selectors != nil {
		flat = *selectors
	}
	for _, selector := range flat {
		switch {
		case selector.Kind == SelectorKindAttribute && attributes != nil,
			selector.Kind == SelectorKindCallee && callees != nil,
			selector.Kind == SelectorKindTag && tags != nil,
			selector.Kind == SelectorKindVariable && variables != nil:
			continue
		}
		merged = append(merged, selector)
	}
	if merged == nil {
		merged = []Selector{}
	}
	return merged
}
