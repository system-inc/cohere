// Package policy is the house policy every engine reads as data: what cohere's binary embeds here for its
// TypeScript and Go engines, the Swift engine reads from the same file, so a decision about the house lands
// once and cannot drift between them.
package policy

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed RuleNaming.json
var ruleNamingFile []byte

// RuleNamingWord is one entry in a closed list, with the sentence that says what belongs under it.
type RuleNamingWord struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

// RuleNamingNamespace is one namespace named by a closed list, and the sentence saying what belongs in it.
type RuleNamingNamespace struct {
	Name       string           `json:"name"`
	Definition string           `json:"definition"`
	Names      []RuleNamingWord `json:"names"`
}

// HouseRuleName is a house rule's name after its namespace, split into the scheme's three parts.
type HouseRuleName struct {
	Category string
	Verb     string
	Object   string
}

// Naming is the embedded file, read once. An invalid file panics at startup: a scheme read wrongly would
// let a misnamed rule through while every check reported the names clean.
var Naming = mustLoadRuleNaming(ruleNamingFile)

func mustLoadRuleNaming(data []byte) RuleNaming {
	loaded, loadRuleNamingError := loadRuleNaming(data)
	if loadRuleNamingError != nil {
		// Invariant: the file is embedded at build and read when the package initializes, so an invalid one
		// fails every test binary that links this package, and no build that passed its tests reaches here.
		panic(fmt.Sprintf("RuleNaming.json: %v", loadRuleNamingError))
	}
	return loaded
}

// loadRuleNaming reads the file, refusing an unknown key, an empty list, a duplicate, an entry with no
// definition, and a word that is not lowercase kebab case, since each would read as something other
// than meant.
func loadRuleNaming(data []byte) (RuleNaming, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var naming RuleNaming
	if decodeError := decoder.Decode(&naming); decodeError != nil {
		return RuleNaming{}, decodeError
	}
	lists := []struct {
		label string
		words []RuleNamingWord
	}{{"verbs", naming.Verbs}, {"categories", naming.Categories}}
	namespaces := make([]RuleNamingWord, 0, len(naming.Namespaces))
	for _, namespace := range naming.Namespaces {
		namespaces = append(namespaces, RuleNamingWord{Name: namespace.Name, Definition: namespace.Definition})
		lists = append(lists, struct {
			label string
			words []RuleNamingWord
		}{"namespaces: " + namespace.Name, namespace.Names})
	}
	// Checked as a list of its own, so a namespace named twice is refused the way a duplicate verb is. An
	// empty list is fine: a file with no closed namespace names every rule by the scheme.
	if len(namespaces) > 0 {
		lists = append(lists, struct {
			label string
			words []RuleNamingWord
		}{"namespaces", namespaces})
	}
	for _, list := range lists {
		if len(list.words) == 0 {
			return RuleNaming{}, fmt.Errorf("%s is empty, so no name could fit", list.label)
		}
		seen := map[string]bool{}
		for _, word := range list.words {
			if !isKebabWord(word.Name) {
				return RuleNaming{}, fmt.Errorf("%s: %q is not lowercase kebab case", list.label, word.Name)
			}
			if seen[word.Name] {
				return RuleNaming{}, fmt.Errorf("%s: %q appears twice", list.label, word.Name)
			}
			seen[word.Name] = true
			if strings.TrimSpace(word.Definition) == "" {
				return RuleNaming{}, fmt.Errorf("%s: %q has no definition", list.label, word.Name)
			}
		}
	}
	return naming, nil
}

// isKebabWord is one or more lowercase words of letters and digits joined by single dashes.
func isKebabWord(text string) bool {
	if text == "" {
		return false
	}
	for _, word := range strings.Split(text, "-") {
		if word == "" {
			return false
		}
		for _, character := range word {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				return false
			}
		}
	}
	return true
}
