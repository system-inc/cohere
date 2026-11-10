package file_name_words

import (
	"slices"
	"strings"
)

// RuleNames are the names of the rules an engine has registered, each held as the words a file name spelling it holds:
// its last slash-separated part, lowercased, with a space for each hyphen, so `@typescript-eslint/no-unused-vars` is
// `no unused vars` and `max-classes-per-file` is `max classes per file`. A rule's name is the registry's, upstream's
// for a port (policy/RuleNaming.json keeps a ported rule's upstream name), so a file named for a rule is named for what
// it holds, in words that are not its author's to spell out. Each engine hands in the registry it holds, never a list
// kept here.
type RuleNames map[string]bool

// NewRuleNames reads registered rules' names into RuleNames.
func NewRuleNames(names []string) RuleNames {
	ruleNames := RuleNames{}
	for _, name := range names {
		if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
			name = name[slash+1:]
		}
		ruleNames[strings.ToLower(strings.ReplaceAll(name, "-", " "))] = true
	}
	return ruleNames
}

// withoutRuleName is a file name's words with the longest run of them that spells a registered rule's name taken out,
// the earliest where two are as long, and the words as they were where none does. What is left is the file's own
// words, which are judged: no_var_cfg.go is no-var's name and `cfg`, and MaxClassesPerFileRule.ts is
// max-classes-per-file's name and `Rule`, a word in full that needs no list of its own.
//
// A rule named in one word, `boundaries/dependencies` or `react-hooks/config`, is never set aside: its name is an
// ordinary word too, so a file that spells it, config.ts, is named for what it holds, and that a rule shares the word
// is a coincidence. Only a name of two words or more says a file is named for a rule.
func (ruleNames RuleNames) withoutRuleName(words []string) []string {
	lowered := make([]string, len(words))
	for index, word := range words {
		lowered[index] = strings.ToLower(word)
	}
	for length := len(words); length > 1; length-- {
		for start := 0; start+length <= len(words); start++ {
			if ruleNames[strings.Join(lowered[start:start+length], " ")] {
				return slices.Concat(words[:start], words[start+length:])
			}
		}
	}
	return words
}
