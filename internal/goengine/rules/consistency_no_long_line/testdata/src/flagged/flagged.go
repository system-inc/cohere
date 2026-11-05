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
}
