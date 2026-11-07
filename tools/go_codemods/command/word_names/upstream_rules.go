package main

import (
	"sort"
	"strings"
	"unicode"
)

// upstreamRules are the names of the rules the workspace ports, as each rule.Rule literal's Name spells them, in
// the camel case a Go name spells them in: one-var as OneVar, react's jsx-no-duplicate-props as
// JsxNoDuplicateProps. A Go name that spells one, the rule's variable, its tests, its helpers, mirrors the rule's
// name, which is upstream's.
type upstreamRules struct {
	// spans maps each camel spelling, its leading capital lowered, to the rule's name.
	spans map[string]string
}

// add records a rule's camel spellings: its whole name, and without a leading no or prefer when two or more words
// remain, since a message or helper names the rule's subject (messageUnexpectedDeleteVar for no-delete-var).
func (rules upstreamRules) add(ruleName string) {
	words := strings.FieldsFunc(ruleName[strings.LastIndex(ruleName, "/")+1:], func(character rune) bool {
		return character == '-' || character == '_'
	})
	if len(words) == 0 {
		return
	}
	camel := func(words []string) string {
		var builder strings.Builder
		for index, word := range words {
			if index == 0 {
				builder.WriteString(strings.ToLower(word[:1]) + word[1:])
				continue
			}
			builder.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
		return builder.String()
	}
	rules.spans[camel(words)] = ruleName
	if len(words) > 2 && (words[0] == "no" || words[0] == "prefer") {
		rules.spans[camel(words[1:])] = ruleName
	}
}

// spelledIn is the rule whose name a flagged name spells, with the flagged word inside that spelling, or "".
func (rules upstreamRules) spelledIn(vocabulary *vocabulary, name string) string {
	spans := make([]string, 0, len(rules.spans))
	for span := range rules.spans {
		spans = append(spans, span)
	}
	// The longest spelling first, so a name holding no-unused-vars is not read as holding only unused-vars.
	sort.Slice(spans, func(left, right int) bool {
		if len(spans[left]) != len(spans[right]) {
			return len(spans[left]) > len(spans[right])
		}
		return spans[left] < spans[right]
	})
	for _, span := range spans {
		capitalized := strings.ToUpper(span[:1]) + span[1:]
		// The rule's word must lie inside the spelling, read where a name can hold it: at the start, where a prefix
		// form judges maxLines, or after another word, where a segment form judges the Vars of TestVarsOnTop.
		_, flaggedAtStart := vocabulary.find(span)
		_, flaggedInside := vocabulary.find("in" + capitalized)
		if !flaggedAtStart && !flaggedInside {
			continue
		}
		for _, spelling := range []string{span, capitalized} {
			index := strings.Index(name, spelling)
			if index < 0 {
				continue
			}
			end := index + len(spelling)
			// The spelling must be whole camel words of the name: it starts the name or a word, and ends it or one.
			if (index == 0 || unicode.IsUpper(rune(spelling[0]))) &&
				(end == len(name) || !unicode.IsLower(rune(name[end]))) {
				return rules.spans[span]
			}
		}
	}
	return ""
}
