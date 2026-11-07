package nexus

import (
	"strings"
	"unicode"

	"github.com/system-inc/cohere/policy"
)

// The casing predicates and converters for consistency-require-constant-casing, and the UpperCamelCase
// reading it shares with consistency-require-identifier-casing, so a listed brand is one judgment in both.
//
// These are deliberately not the toPascalCase and toCamelCase already in this package. Those serve
// consistency-no-screaming-snake-case, where every word arrives all-uppercase and lowercasing a part
// whole is exactly right. Here the input is an ordinary identifier whose internal boundaries are
// load bearing: folding numberAbsentPlaceholder the shouting way yields numberabsentplaceholder.
// Two converters that do almost the same thing is worse than one of the wrong shape, so the
// difference is stated rather than shared.

// isPascalCase and isCamelCase each refuse a leading underscore, as both casing rules and the Go and
// Swift engines do: a leading underscore is flagged on every name we declare (Kirk, 2026-11-06,
// #4b337jk, case us05). It is a convention for private or for unused, and TypeScript says private with
// `#` or `private`, which the compiler checks, while an unused name is deleted, or written as the bare
// `_`, which is no name and is never judged. The converters below drop the underscore, so the advice is
// always a name these predicates accept.
func isPascalCase(name string) bool {
	return matchesCasing(name, unicode.IsUpper)
}

func isCamelCase(name string) bool {
	return matchesCasing(name, unicode.IsLower)
}

// matchesCasing checks the shared shape: a first letter the caller judges, then letters and digits
// only. An underscore anywhere disqualifies the name, which is what sends snake_case and `_cache` to a
// converter rather than letting either pass as a casing.
func matchesCasing(name string, firstLetterMatches func(rune) bool) bool {
	if name == "" {
		return false
	}

	for index, character := range name {
		if index == 0 {
			if !unicode.IsLetter(character) || !firstLetterMatches(character) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			return false
		}
	}
	return true
}

// constantNameWords folds the separators a name carries into word boundaries, so a suggestion is a
// name the predicates above would actually accept.
//
// Both predicates reject an underscore anywhere, so a converter that did not remove one would report a
// name and then hand back its own spelling: `Constant "user_id" should be camelCase ("user_id")`. That
// is an error no edit can satisfy, and a reader who tries the suggestion gets the same report again. A
// leading underscore is removed with the rest, so `_cache` is told `cache`.
func constantNameWords(name string) []string {
	var words []string
	for _, part := range strings.Split(name, "_") {
		if part == "" {
			continue
		}
		// An all-uppercase part has no internal boundaries to preserve, so lowercase it whole:
		// leaving it produces "utmSOURCE" from "utm_SOURCE", which is neither casing.
		if isAllUppercase(part) {
			part = strings.ToLower(part)
		}
		words = append(words, part)
	}
	return words
}

// isAllUppercase reports whether a word carries no lowercase letter, so it has no boundary to keep.
func isAllUppercase(part string) bool {
	for _, character := range part {
		if unicode.IsLower(character) {
			return false
		}
	}
	return true
}

// constantNameToPascalCase raises the first character of each word, so numberAbsentPlaceholder
// suggests NumberAbsentPlaceholder and apple_root_ca suggests AppleRootCa.
func constantNameToPascalCase(name string) string {
	words := constantNameWords(name)
	if len(words) == 0 {
		return name
	}

	var builder strings.Builder
	for _, word := range words {
		builder.WriteString(raiseFirstCharacter(word))
	}
	return builder.String()
}

// isUpperCamelCaseName reports a name both casing rules read as UpperCamelCase: one opening on a capital, or on a
// brand policy/Brands.json lists, spelled exactly as listed, with nothing after it or an UpperCamelCase
// rest. The rest opens on its capital, never on an underscore, which isPascalCase refuses, so
// `iMessage_Thing` is not read as `iMessageThing`.
func isUpperCamelCaseName(name string) bool {
	if isPascalCase(name) {
		return true
	}
	brand, opensOnBrand := policy.Brands.LeadingBrand(name)
	if !opensOnBrand {
		return false
	}
	rest := strings.TrimPrefix(name, brand)
	return rest == "" || isPascalCase(rest)
}

// upperCamelCaseSuggestion is the UpperCamelCase form the advice names. A name opening on a listed brand
// keeps the brand as its owner spells it, so `iMessagecommandLineInterface` is told
// `iMessageCommandLineInterface` rather than to respell the brand, and any other name is the converter's.
func upperCamelCaseSuggestion(name string) string {
	if brand, opensOnBrand := policy.Brands.LeadingBrand(name); opensOnBrand {
		rest := strings.TrimPrefix(name, brand)
		if suggestion := brand + constantNameToPascalCase(rest); isUpperCamelCaseName(suggestion) {
			return suggestion
		}
	}
	return constantNameToPascalCase(name)
}

// constantNameToCamelCase is the mirror: the first word starts lowercase, every later one is raised.
func constantNameToCamelCase(name string) string {
	words := constantNameWords(name)
	if len(words) == 0 {
		return name
	}

	var builder strings.Builder
	builder.WriteString(lowerFirstCharacter(words[0]))
	for _, word := range words[1:] {
		builder.WriteString(raiseFirstCharacter(word))
	}
	return builder.String()
}

func raiseFirstCharacter(word string) string {
	runes := []rune(word)
	if len(runes) == 0 {
		return ""
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// lowerFirstCharacter lowers only the first character, since the rest of the word carries boundaries
// the caller wants kept. constantNameWords has already lowered an all-uppercase word whole, so this
// never produces the "tHRESHOLDS" shape.
func lowerFirstCharacter(word string) string {
	runes := []rune(word)
	if len(runes) == 0 {
		return ""
	}
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// reservedWords are the words a program cannot use as a binding name.
//
// A trailing underscore is the usual escape when a keyword is the name the domain wants, and
// ChatUsageReportService used exactly that for a Durable Object handle: "do_". Folding the separator
// away produced "do", which parses as the loop keyword and took the file with it. A suggestion that
// cannot be typed is worse than no suggestion, so a name whose escape is load bearing is left alone.
var reservedWords = map[string]bool{
	"arguments": true, "await": true, "break": true, "case": true, "catch": true, "class": true,
	"const": true, "continue": true, "debugger": true, "default": true, "delete": true, "do": true,
	"else": true, "enum": true, "eval": true, "export": true, "extends": true, "false": true,
	"finally": true, "for": true, "function": true, "if": true, "implements": true, "import": true,
	"in": true, "instanceof": true, "interface": true, "let": true, "new": true, "null": true,
	"package": true, "private": true, "protected": true, "public": true, "return": true,
	"static": true, "super": true, "switch": true, "this": true, "throw": true, "true": true,
	"try": true, "typeof": true, "var": true, "void": true, "while": true, "with": true,
	"yield": true,
}

// escapesAReservedWord reports a name that only escapes a keyword, with an underscore after it or before
// it, `do_` and `_default` alike, so recasing it would produce something unusable. The Go engine leaves
// `type_` alone for the same reason: a suggestion that cannot be typed is worse than none.
func escapesAReservedWord(name string) bool {
	return reservedWords[strings.ToLower(strings.Trim(name, "_"))]
}
