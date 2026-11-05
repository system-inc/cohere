package flagged

// This file is left unformatted on purpose: it holds a shape the parser accepts and gofmt rewrites, since a rule
// meets code before the formatter does.

// A semicolon alone is an empty statement, which runs nothing.
func semicolonOnly() { ; } // want "body is empty"
