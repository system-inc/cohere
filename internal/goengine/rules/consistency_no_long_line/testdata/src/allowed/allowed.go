// Package allowed holds the lines past 120 columns the rule leaves alone, because none of them can wrap.
package allowed

import (
	"strings"

	longpath "longimport/a_package_whose_import_path_runs_past_the_limit_so_its_import_line_is_longer_than_one_hundred_and_twenty"
)

//go:generate echo a directive the toolchain reads as one line cannot wrap, so it may run past the limit and so on and so on a

// See https://example.com/a/path/that/has/no/space/in/it/anywhere/so/it/cannot/wrap/and/would/not/fit/even/on/a/comment/line/of/its/own
func use(arguments ...any) string { return strings.Repeat("", len(arguments)) + longpath.Value }

func cases(argument string, name string) []string {
	_ = name // https://example.com/a/path/that/has/no/space/in/it/anywhere/so/it/cannot/wrap/and/would/not/fit/even/on/a/comment/line/of/its/own
	use(use(
		"a string literal alone at the start of its line, past the limit, closed by the calls around it and so on and so on"))
	return []string{
		"a string literal alone at the start of its line, past the limit, as a formatter prints it and so on and so on and s",
		`a raw string alone at the start of its line, its first line past the limit, data like its body and so on and so on an
the rest of the raw string`,
		use(argument, `a raw string's body
holds data with its own line structure, so a line of it past the limit is the data's own length and so on and so on and so onx
and its closing line may run past the limit too, since the overflow is still the data, not code and so on and so on and sox`),
	}
}

// Record carries a struct tag past the limit, which cannot wrap, since a tag is one literal.
type Record struct {
	Name string `json:"name" description:"a struct field's tag past the limit, after only a short name and typeand so on and"`
}

// describe returns a string past the limit, which cannot wrap, since a newline after return ends it.
func describe() string {
	return "a returned string past the limit, which cannot move to the next line, as return ends thereand so on and so on and"
}
