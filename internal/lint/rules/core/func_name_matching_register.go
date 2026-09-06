package core

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers this package's `func-name-matching` rule.
//
// A file of its own rather than a line in `register.go`, so a port is three owned files and no
// shared edit. The decoder is hand-written because upstream's schema is an `anyOf` of two array
// shapes rather than a single object; see DecodeFuncNameMatchingOptions.
func init() {
	rule.Register(rule.Registration{Rule: FuncNameMatching, Decode: DecodeFuncNameMatchingOptions})
}
