// Package flagged holds every shape of line the rule must report. Each want comment sits on the line it expects,
// which makes that line longer still, so every flagged line here is already past 120 columns before its want.
package flagged

import (
	// A comment in the import block is prose like any other, so this one, past the limit, wraps and so on and so on and so o. // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
	"strings"
)

func use(arguments ...any) string { return strings.Repeat("", len(arguments)) }

func cases(argument string, name string) {
	// A long string after code that could wrap: the call and its first argument could open on a line above it.
	use(argument, "a string literal running past the limit, after code that could move to a line above and so on and so on a") // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
	// A long string alone at the start of its line, followed by more code, which could move to the next line.
	use(
		"a string literal alone at the start of its line, running past the limit, followed by code and so on and so o" + name, // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
	)
	// A long string alone at the start of its line, with a trailing comment that could sit above it instead.
	use(
		"a string literal alone at the start of its line, running past the limit and so on and so on andx", // a trailing note // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
	)
	// A long line of code, which wraps at an operator.
	_ = argument + name + argument + name + argument + name + argument + name + argument + name + argument + name + name + name // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
	// The next three lines are measured as written, so their wants sit on the line above them and point down.
	// A line of 121 columns, with a tab counted as four.
	// want +1 "This line is 121 columns"
	_ = use(argument, name, "and so on and so on and so on and so on and so on and so on and so on and so on and so onx")
	// A comment whose last word runs past the limit, a word that fits on a comment line of its own.
	// want +1 `the house limit is 120`
	// A comment whose last word runs past the limit is flagged, since the last word would fit on a comment line of its own,
	// A trailing comment whose URL would fit on a comment line of its own, so the comment can wrap before it.
	// want +1 `the house limit is 120`
	_ = name // See the page that explains how all of this works, at https://example.com/a/short/url/that/fits/on/its/own/line
	// A comment of words that runs past the limit is flagged, since a comment wraps like any prose and so on and so on and s. // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
	// A raw string opening after code, its first line past the limit, since the code before it could wrap.
	_ = use( /* want `the house limit is 120` */ argument, name, `a raw string opening past the limit after code and so on and so on and so on and so on and so on
the rest of the raw string`)
	// The closing line of a raw string, with code after the backtick that runs on past the limit.
	_ = use(`a raw string
whose closing line is short`, argument, name, argument, name, argument, name, argument, name, "and so on and so on and so on") // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
	// A //go: comment after code is no directive, so it is a comment like any other, and it wraps.
	_ = name //go:generate echo a directive after code is no directive, so it is prose and wrapsand so on and so on and so onx // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
	// A raw string whose closing line runs its data past the limit, with code after it that could wrap.
	_ = use(`a raw string
whose closing line runs its data past the limit and then has code after its backtickand so on and so on and so on and so on`, argument, name, use()) // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
}

// Record carries the long tag after names and a type that are past the limit before the tag starts.
type Record struct {
	FirstName, SecondName map[string]map[string]map[string]map[string]map[string]map[string]map[string]map[string]map[string]string `json:"name" description:"a struct field's tag past the limit, after only a short name and typeand so on and"` // want `This line is \d+ columns, a tab counted as four, and the house limit is 120\.`
}

// Entry is what the keyed cases build.
type Entry struct{ Name, Other string }

// keyed holds strings given to a key or a name past the limit, with code before or after them that could wrap. Each
// expectation sits on the line above its case, so no trailing comment is what makes the case a finding.
func keyed(entry *Entry, other string) []any {
	// A struct literal opening on its key's line, which could open on a line above the key.
	// want +1 `the house limit is 120`
	opened := Entry{Name: "a keyed string past the limit after a struct literal that could wrap and so on and so and so o"}
	// Two names on the left, given two values.
	// want +1 `the house limit is 120`
	first, second := "a string past the limit after two names on the left, which could wrap and so on and so on and", other
	// want +1 `the house limit is 120`
	entry.Name, entry.Other = other, "a string past the limit after two selectors and a name, which could wrap and so on a"
	// want +1 `the house limit is 120`
	var left, right = "a var's string past the limit after two names, which could wrap and so on and so on and so a", other
	// A string followed by more code, which could move to the next line.
	// want +1 `the house limit is 120`
	joined := "a string past the limit after a name and :=, followed by code that could wrap and so on and so o an" + other
	// want +1 `the house limit is 120`
	entry.Name = "a string past the limit after a selector and =, followed by a call that could wrap and so a" + use(other)
	// A trailing comment, which could sit above the line instead.
	// want +1 `the house limit is 120`
	joined = "a string past the limit after a name and =, followed by a trailing note and so on and so on and so" // a note
	// An index, a compound operator, a type and an if statement's opening are code, and wrap.
	values := []string{other}
	// want +1 `the house limit is 120`
	values[0] = "a string past the limit after an index on the left, which is code and so on and so on and so on and and s"
	// want +1 `the house limit is 120`
	joined += "a string past the limit after a name and +=, a compound operator, and so on and so on and so on and so and "
	// want +1 `the house limit is 120`
	var typed string = "a var's string past the limit after a name and a type, which could be long and so on and so on and"
	// want +1 `the house limit is 120`
	if initialized := "a string past the limit after the if keyword, which could wrap and so on and so"; initialized != "" {
		joined = initialized
	}
	return []any{
		opened, first, second, left, right, joined, values, typed,
		Entry{
			// Another key before the string, which could wrap after its comma.
			// want +1 `the house limit is 120`
			Other: other, Name: "a keyed string past the limit after another key that could wrap and so on and so on and ",
		},
		Entry{
			// A comment between the key and its string, which could sit above the key instead.
			// want +1 `the house limit is 120`
			Name: /* a note */ "a keyed string past the limit after a comment that could move and so on and so on and and",
		},
		Entry{
			// More code after the string, which could move to the next line.
			// want +1 `the house limit is 120`
			Name: "a keyed string past the limit followed by code that could wrap and so on and so on and so and " + other,
		},
		Entry{
			// Another key after the string, which could wrap after the string's comma.
			// want +1 `the house limit is 120`
			Other: "a keyed string past the limit followed by another key that could wrap and so on and so o", Name: other,
		},
		map[string]string{
			// A key built by a call, which is code and could wrap.
			// want +1 `the house limit is 120`
			use(other): "a keyed string past the limit after a key built by a call and so on and so on and so on and so o",
		},
	}
}
