package typescript

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers @typescript-eslint/init-declarations.
//
// Its own file rather than a line in the package's register.go: that shared list is one file every
// concurrent port has to edit, so two ports landing at once conflict on a file neither is really
// changing.
//
// The Decode entry names the rule's own decoder, and here that is required rather than defensive: the
// option wire shape is a positional array whose first element is a bare mode string, which the
// generic struct decoder cannot express at all.
func init() {
	rule.Register(rule.Registration{
		Rule:   InitDeclarations,
		Decode: DecodeInitDeclarationsOptions,
	})
}
