package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const ambiguousFile = "/repository/source/Thing.tsx"

func TestConsistencyNoAmbiguousIdentifierFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		{"catch parameter", "try {\n    run();\n} catch (e) {\n    report();\n}\n", []string{"noAmbiguousE"}},
		{"bare underscore", "const _ = 1;\n", []string{"noUnderscore"}},
		{"single letter", "const n = 1;\n", []string{"noSingleLetter"}},
		{"a outside a comparator", "const a = 1;\n", []string{"noSingleLetter"}},
		{"b outside a comparator", "const b = 1;\n", []string{"noSingleLetter"}},
		// The reference and the parameter are both identifiers, so both report.
		{"e used in a body", "try {\n    run();\n} catch (e) {\n    report(e);\n}\n", []string{"noAmbiguousE", "noAmbiguousE"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyNoAmbiguousIdentifier, ambiguousFile, testCase.sourceText)
			ruletest.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

func TestConsistencyNoAmbiguousIdentifierStaysSilent(t *testing.T) {
	// The exemptions are the rule. Without them it fires on conventional spellings and on names
	// somebody else chose, which is how a naming rule gets switched off.
	cases := []struct {
		name       string
		sourceText string
	}{
		{"coordinates", "const x = 1;\nconst y = 2;\nconst z = 3;\n"},
		{"descriptive names", "const count = 1;\ntry {\n    run();\n} catch (error) {\n    report(error);\n}\n"},
		{"sort comparator parameters", "const sorted = items.sort((a, b) => a - b);\n"},
		{"sort comparator with a body", "const sorted = items.sort((a, b) => {\n    return a - b;\n});\n"},
		{"sort comparator as a function expression", "const sorted = items.sort(function (a, b) {\n    return a - b;\n});\n"},
		// These are names somebody else chose, not bindings this file has to track.
		{"a property read", "const value = thing.e;\n"},
		{"a property read of a", "const value = thing.a;\n"},
		{"an object key", "const options = { e: 1, a: 2 };\n"},
		{"an import specifier", "import { e } from 'x';\n"},
		{"a type property signature", "interface ThingInterface {\n    e: string;\n    a: number;\n}\n"},
		// An uppercase single letter is a type parameter.
		{"type parameters", "export function identity<T>(value: T): T {\n    return value;\n}\n"},
		{"an underscore-prefixed name is fine", "const _event = 1;\n"},

		// JSX names. An intrinsic element is named by HTML rather than by us, so `<p>` and `<b>`
		// are not names anyone chose. Their absence from the fixtures let this rule report 3,081
		// false findings on the real tree before it was caught.
		{"a jsx intrinsic element", "export const Thing = () => <p>hi</p>;\n"},
		{"a nested jsx element", "export const Thing = () => <p><b>hi</b></p>;\n"},
		{"a self-closing jsx element", "export const Thing = () => <img src=\"x\" />;\n"},
		{"a jsx attribute name", "export const Thing = () => <div a=\"1\" />;\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyNoAmbiguousIdentifier, ambiguousFile, testCase.sourceText)
			ruletest.ExpectClean(t, result)
		})
	}
}

// The whole point of the `e` message is saying which of the two things it thinks this is, so a
// reader can disagree with the reasoning rather than only the verdict.
func TestConsistencyNoAmbiguousIdentifierInfersContext(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantHint   string
		wantName   string
	}{
		{"catch reads as an error", "try {\n    run();\n} catch (e) {\n    report();\n}\n", "appears to be an error", "error"},
		{"a handler key reads as an event", "const properties = { onClick: (e) => run() };\n", "appears to be an event", "event"},
		{"a handler-named variable reads as an event", "const handleClick = (e) => run();\n", "appears to be an event", "event"},
		{"a bare arrow is unclear", "const start = (e) => run();\n", "context unclear", "event"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyNoAmbiguousIdentifier, ambiguousFile, testCase.sourceText)
			if len(result.Diagnostics) == 0 {
				t.Fatalf("expected a finding, got none")
			}
			description := result.Diagnostics[0].Message.Description
			if !strings.Contains(description, testCase.wantHint) {
				t.Fatalf("expected hint %q, got: %s", testCase.wantHint, description)
			}
			if !strings.Contains(description, `"`+testCase.wantName+`"`) {
				t.Fatalf("expected suggestion %q, got: %s", testCase.wantName, description)
			}
		})
	}
}

// No fix, deliberately. Renaming a binding without following its references through scope would
// leave every other use pointing at a name that no longer exists.
func TestConsistencyNoAmbiguousIdentifierProposesNoFix(t *testing.T) {
	result := ruletest.Run(t, ConsistencyNoAmbiguousIdentifier, ambiguousFile,
		"try {\n    run();\n} catch (e) {\n    report(e);\n}\n")
	for _, diagnostic := range result.Diagnostics {
		if len(diagnostic.Fixes) != 0 {
			t.Fatalf("expected no fixes, got %d", len(diagnostic.Fixes))
		}
	}
}
