// Package differences holds the calls where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: the original's slips, a constant identifier taken as a dynamic format, a
// parenthesized callee whose fix takes off the parenthesis's f, fmt.Errorf's fix naming errors without importing it,
// and the shapes the pattern and the walk might read apart, a dot import, a type embedding testing.TB and a method
// expression.
package differences

import (
	. "fmt"
	"log"
	"testing"
)

const constantFormat = "100%"

type embedded struct {
	testing.TB
}

func plausible(text string, holder embedded, logger *log.Logger) {
	Printf(constantFormat)
	Printf(text)
	(log.Printf)(text)
	_ = Errorf(text)
	holder.Logf(text)
	(*log.Logger).Printf(logger, text)
}
