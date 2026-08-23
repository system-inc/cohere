package react

import (
	"strings"
	"unicode"
)

// The two name predicates React conventions rest on, lifted from `internal/rules/structure/` where
// four rules shared them and nothing outside that package could see them.
//
// A research pass on `nextjs/no-async-client-component` asked for exactly `IsLikelyComponentName`
// and reported that no capitalization helper existed on any shelf. It did exist, in a rule package,
// which is the same invisibility this shelf was built to remove: a helper four rules already agreed
// on, unreachable from the fifth that needs it.

// IsHookName reports the shape React's own linting recognizes as a hook: "use" followed by an
// uppercase letter.
//
// A function named "used" or "user" is not a hook, which is why the letter after the prefix is
// tested rather than the prefix alone.
func IsHookName(name string) bool {
	if !strings.HasPrefix(name, "use") || len(name) == 3 {
		return false
	}
	return unicode.IsUpper([]rune(name[3:])[0])
}

// IsLikelyComponentName reports a name starting with a capital.
//
// JSX decides between a component and an intrinsic element by that letter, so the capital is the
// author's claim that this is a component rather than an inference anyone is making about it.
//
// The test is `unicode.IsUpper` on the first rune rather than an ASCII range, and that is upstream's
// behavior rather than a generalization: oxlint uses `char::is_uppercase` while ESLint's regex is
// ASCII-only, and oxlint is the port target.
func IsLikelyComponentName(name string) bool {
	runes := []rune(name)
	return len(runes) > 0 && unicode.IsUpper(runes[0])
}
