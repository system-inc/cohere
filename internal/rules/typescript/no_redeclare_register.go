package typescript

import "github.com/system-inc/verify/internal/rule"

// init registers no-redeclare.
//
// A file of its own rather than a line in the package's shared `register.go`, which is the file
// every porter used to conflict on.
//
// The decoder is this rule's own rather than `rule.DecodeOptionsInto`, because its one option
// defaults to true and the generic helper cannot distinguish an absent key from an explicit false.
func init() {
	rule.Register(rule.Registration{
		Rule:   NoRedeclare,
		Decode: DecodeNoRedeclareOptions,
	})
}
