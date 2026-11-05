package flagged

// This file once held `func semicolonOnly() { ; }`, a body whose only statement is an empty one. cohere's own
// gofmt guard keeps every fixture formatted, and gofmt rewrites that body to `{}`, which the engine's fix phase
// does before any rule runs, so the shape the rule meets is the empty body below.

// A body gofmt emptied of a lone semicolon is an empty body.
func semicolonOnly() {} // want "body is empty"
