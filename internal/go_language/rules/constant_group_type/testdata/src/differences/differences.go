// Package differences holds the constant groups where the port and the original could most plausibly part, and
// records no difference, since they part nowhere: a parenthesized literal, a converted literal, a first constant
// whose type is a qualified name, comments after values gofmt aligns, and literals whose type converts to the first's
// but whose value does not fit it, which both report and both fix into a declaration that does not compile.
package differences

import "time"

const (
	parenthesized int = 1
	wrapped           = (2)
)

const (
	converted  int = 1
	conversion     = int(2)
)

const (
	duration time.Duration = 1
	longer                 = 2      // a comment after a short value
	longest                = 300000 // a comment after a long value, which gofmt aligns
)

const (
	small int8 = 1
	large      = 1000
)

const (
	unsigned uint = 1
	signed        = -1
)
