package tailwind

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

// selectorGroup is one kind's selectors, with each name's answer remembered: which of them it matches.
//
// The answers are a memo because names repeat and the patterns run on a backtracking engine: ahra's
// attributes are almost all `className`, asked about on every JSX attribute in 3,978 files, and
// upstream's defaults hold 25 callee selectors. An answer is a pure function of the name, so the memo
// is shared by every worker reading with these settings.
type selectorGroup struct {
	selectors []compiledSelector
	lowercase bool
	hasPaths  bool
	// hasNames is whether any selector names by name, which a bare template's comment can only match.
	hasNames bool
	byName   sync.Map
	byPath   sync.Map
}

// matching is the selectors a name, or a path, matches, by index. path is asked only when a selector
// with a path did not match by name, since it is a new string for every member call in the file. A nil
// path matches by name alone, for what has no path: an attribute, a variable, a comment.
func (group *selectorGroup) matching(name string, path func() string) []int {
	if len(group.selectors) == 0 {
		return nil
	}
	byName := group.answer(&group.byName, name, func(selector compiledSelector) *namePatterns { return selector.name })
	if !group.hasPaths || path == nil {
		return byName
	}
	byPath := group.answer(
		&group.byPath,
		path(),
		func(selector compiledSelector) *namePatterns { return selector.path },
	)
	if len(byPath) == 0 {
		return byName
	}
	merged := make([]int, 0, len(byName)+len(byPath))
	for index := range group.selectors {
		if slices.Contains(byName, index) || slices.Contains(byPath, index) {
			merged = append(merged, index)
		}
	}
	return merged
}

func (group *selectorGroup) answer(memo *sync.Map, name string, patternsOf func(compiledSelector) *namePatterns) []int {
	if name == "" {
		return nil
	}
	key := name
	if group.lowercase {
		key = strings.ToLower(name)
	}
	if remembered, isAnswered := memo.Load(key); isAnswered {
		indices, isIndices := remembered.([]int)
		if !isIndices {
			// Invariant: a selector group's memos are written only below, and only with an []int.
			panic(fmt.Sprintf("tailwind: the selector memo for %q holds %T, not []int", key, remembered))
		}
		return indices
	}
	var answer []int
	for index, selector := range group.selectors {
		if patterns := patternsOf(selector); patterns != nil && patterns.matchesUnremembered(name) {
			answer = append(answer, index)
		}
	}
	memo.Store(key, answer)
	return answer
}
