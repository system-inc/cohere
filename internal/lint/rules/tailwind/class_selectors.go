package tailwind

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
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

// legacyMatcher is a legacy entry's matcher, upstream's options/schemas/matchers.js.
type legacyMatcher struct {
	Match       MatcherType `json:"match"`
	PathPattern string      `json:"pathPattern,omitempty"`
}

// defaultSelectorsJson is upstream 4.7.0's DEFAULT_SELECTORS, written by
// tools/generate_class_literals from the installed package, never by hand.
//
//go:embed default_selectors.json
var defaultSelectorsJson []byte

// DefaultSelectors is upstream's default selectors, decoded once.
var DefaultSelectors = sync.OnceValue(func() []Selector {
	var selectors []Selector
	decoder := json.NewDecoder(bytes.NewReader(defaultSelectorsJson))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&selectors); decodeError != nil {
		// Invariant: default_selectors.json is embedded at build time, and every test that reads the default
		// selectors decodes this same file, so one that does not decode fails the tests before it ships.
		panic(fmt.Sprintf(
			"default_selectors.json does not decode, so it was not written by tools/generate_class_literals: %v",
			decodeError,
		))
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
			return fmt.Errorf(
				"selectors[%d]: the kind is attribute, callee, tag or variable, not %q",
				index,
				selector.Kind,
			)
		}
		if selector.Kind != SelectorKindCallee &&
			(selector.TargetCall != nil || selector.CallTarget != nil || selector.TargetArgument != nil) {
			return fmt.Errorf("selectors[%d]: only a callee selector takes a call or argument target", index)
		}
		if selector.Kind != SelectorKindCallee && selector.Kind != SelectorKindTag && selector.Path != "" {
			return fmt.Errorf("selectors[%d]: only a callee or tag selector takes a path", index)
		}
		if checkMatchersError := checkMatchers(selector.Match, true); checkMatchersError != nil {
			return fmt.Errorf("selectors[%d]: %w", index, checkMatchersError)
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
			if checkMatchersError := checkMatchers(matcher.Match, false); checkMatchersError != nil {
				return checkMatchersError
			}
		default:
			return fmt.Errorf(
				"a matcher is strings, objectKeys, objectValues or anonymousFunctionReturn, not %q",
				matcher.Type,
			)
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
