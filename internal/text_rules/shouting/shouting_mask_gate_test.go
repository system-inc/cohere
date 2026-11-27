package shouting

import (
	"strings"
	"testing"
)

// The gates must actually skip work, or they are pure overhead.
func TestShoutingMaskGatesActuallySkip(test *testing.T) {
	test.Parallel()

	plain := "// just some ordinary prose about the thing\n// with no commands and no tags\n"
	if containsAnyCommandStarter(plain) {
		test.Fatalf("command gate admits plain prose")
	}
	if strings.ContainsRune(plain, '@') {
		test.Fatalf("example gate admits plain prose")
	}

	// And they must admit the real cases, or the rule goes quiet for them.
	for _, admitted := range []string{
		"$ ls",
		"git commit",
		"pnpm install",
		"curl x",
		"s c",
		"ahra os",
		"sqlite3 db",
		"npm run",
	} {
		if !containsAnyCommandStarter(admitted) {
			test.Fatalf("command gate rejects %q, which the pattern matches", admitted)
		}
	}
	// The whitespace arm is the one a literal cannot express, and a fixture is the only thing that
	// catches it: 800 real files once contained no multi-space form, so a comparison over them passed
	// while the gate was wrong.
	for _, spacing := range []string{"s c", "s  c", "s\tc"} {
		if !containsAnyCommandStarter(spacing) {
			test.Fatalf("command gate rejects %q, which the pattern matches", spacing)
		}
	}
}

// The cases a sample of real comments cannot be relied on to hold, written out because that lesson has now cost
// twice.
func TestShoutingUppercaseGateAdmitsRealShouts(test *testing.T) {
	test.Parallel()

	// Every one of these must reach the expensive path.
	admitted := []string{
		"NEVER do this", "this is REALLY bad", "DO NOT EDIT",
		"a TODO marker", "the API contract", // acronyms: admitted, then declined downstream
		"MAX_RETRY_COUNT is an identifier", // underscore token
		"shouting at the very ENDOFLINE",
	}
	for _, body := range admitted {
		if !HasAdjacentUppercaseLetters(body) {
			test.Fatalf("gate rejects %q, which reaches the token scan", body)
		}
	}

	// And these must be skipped, or the gate is not a gate.
	skipped := []string{
		"ordinary prose about a thing",
		"a sentence with One capital",
		"CamelCase identifiers do not shout",
		"",
	}
	for _, body := range skipped {
		if HasAdjacentUppercaseLetters(body) {
			test.Fatalf("gate admits %q, which holds no adjacent uppercase pair", body)
		}
	}

	// The boundary: exactly two adjacent uppercase letters is the shortest reportable shape.
	if !HasAdjacentUppercaseLetters("it IS wrong") {
		test.Fatalf("gate rejects a two-letter shout")
	}
}
