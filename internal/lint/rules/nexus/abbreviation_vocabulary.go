package nexus

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
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
// rule's message, for a name that binds in scope or for a property key.
//
// Where the name sits matters only to the advice: a binding's advice must be a name strict code can
// bind, and a key's need not, since `{ arguments: [] }` is legal. The listener asks it only after the
// cheap gate and the foreign-name skips, and then silences the framework exemptions by the form and
// word it returns, which is exactly what each exemption did when it was a `return` at that branch.
func findAbbreviation(name string, binds bool) (abbreviationFinding, bool) {
	finding, found := vocabulary.Find(name)
	if !found {
		return abbreviationFinding{}, false
	}
	values := finding.Values
	// The advice is legal TypeScript, never a name strict code refuses to bind: `args` becomes a
	// descriptive name ending in "Arguments", not `arguments`, and `pkg` one ending in "Package", not the
	// reserved `package`. Go's twin shapes the advice the same way for a name Go already gives something
	// else. Only a whole-word finding stands where the bare expansion would, so only its advice changes,
	// and a word with more than one expansion keeps the vocabulary's advice, since an ending that named
	// one meaning would be a guess.
	if binds && finding.Form == abbreviation.FormWhole && len(finding.Entry.Expansions) == 1 &&
		isUnbindableInStrictCode(finding.Entry.Expansions[0]) {
		values = map[string]string{
			"name": finding.Values["name"],
			"advice": `Use a descriptive name ending in "` + abbreviation.Capitalize(finding.Entry.Expansions[0]) +
				`", since strict code cannot bind "` + finding.Entry.Expansions[0] + `".`,
		}
	}
	text := abbreviationTexts[finding.MessageId]
	return abbreviationFinding{
		form:    string(finding.Form),
		entry:   finding.Entry,
		message: rule.Message{Id: text.Id, Description: text.Render(values)},
	}, true
}

// isUnbindableInStrictCode reports whether strict code refuses a name as a binding: a reserved word, a
// word strict mode reserves for the future (`package`, `interface`, `static`), or `arguments` and
// `eval`, which the binder refuses by their text. Modules and classes are always strict.
func isUnbindableInStrictCode(name string) bool {
	kind := scanner.GetIdentifierToken(name)
	if kind >= ast.KindFirstReservedWord && kind <= ast.KindLastReservedWord {
		return true
	}
	if kind >= ast.KindFirstFutureReservedWord && kind <= ast.KindLastFutureReservedWord {
		return true
	}
	return name == "arguments" || name == "eval"
}
