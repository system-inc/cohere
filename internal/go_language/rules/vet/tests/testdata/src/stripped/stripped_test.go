package stripped

import "strings"

// upper is the only user of strings, and the strip takes its body.
func upper(text string) string { return strings.ToUpper(text) }

// ExampleBuilder names strings' Builder, found through the package's import of strings.
func ExampleBuilder() {}

// ExampleReplacer_Missing names a method strings' Replacer does not have.
func ExampleReplacer_Missing() {}
