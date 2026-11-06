package core

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers this package's `one-var` rule.
//
// A file of its own rather than a line in `register.go`, so a port is its own owned files and no
// shared edit.
//
// `Decode` rather than `rule.DecodeOptionsInto`, because upstream's single schema element is a
// union of a string enum and two object shapes, which no one Go struct decodes. See
// `DecodeOneVariableOptions`.
//
// Deliberately not enabled in `CohereSettings.json`. See `one_var.md` and the entry in
// `internal/lint/registry/live_wiring_test.go`.
func init() {
	rule.Register(rule.Registration[OneVariableSettings]{Rule: OneVariable, Decode: DecodeOneVariableOptions})
}
