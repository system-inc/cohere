package nexus

import (
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/naming/abbreviation"
	"github.com/system-inc/cohere/policy"
)

// The abbreviation vocabulary, policy/Abbreviations.json, is read and interpreted in
// internal/naming/abbreviation, which the Go engine's twin of this rule reads too, so the words, their
// forms and the order they are judged in live once for every engine (#2cqemcd for the file itself).
//
// What stays here is this rule's own policy: the framework exemptions in the rule's listener, and its
// findings as nexus's rule.Message, rendered from this rule's entry in the policy message catalog.

// vocabulary is policy's file as every engine reads it, loaded once by the package that interprets it.
var vocabulary = abbreviation.Policy

// abbreviationTexts are the rule's four messages by the id a vocabulary finding names. The vocabulary
// names only these four, so every finding has its text here.
var abbreviationTexts = map[string]policy.MessageHandle{
	abbreviation.AbbreviatedIdentifier:  abbreviatedIdentifierText,
	abbreviation.AbbreviatedSuffix:      abbreviatedSuffixText,
	abbreviation.MillisecondSuffix:      millisecondSuffixText,
	abbreviation.AbbreviatedWordSegment: abbreviatedWordSegmentText,
}

// abbreviationFinding is what the vocabulary says about a name before any framework exemption, with
// its message in this rule's shape.
type abbreviationFinding struct {
	// form is "whole", "prefix", "suffix" or "segment", and entry is the word that matched. The
	// listener's framework exemptions are keyed on the two together.
	form    string
	entry   *abbreviation.Entry
	message rule.Message
}

// findAbbreviation answers what the vocabulary reports for a name, if anything, rendered as this
// rule's message.
//
// It knows nothing about where the name sits. The listener asks it only after the cheap gate and
// the foreign-name skips, and then silences the framework exemptions by the form and word it
// returns, which is exactly what each exemption did when it was a `return` at that branch.
func findAbbreviation(name string) (abbreviationFinding, bool) {
	finding, found := vocabulary.Find(name)
	if !found {
		return abbreviationFinding{}, false
	}
	text := abbreviationTexts[finding.MessageId]
	return abbreviationFinding{
		form:    string(finding.Form),
		entry:   finding.Entry,
		message: rule.Message{Id: text.Id, Description: text.Render(finding.Values)},
	}, true
}
