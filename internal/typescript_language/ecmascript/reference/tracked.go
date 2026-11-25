package reference

import (
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// Tracked is one use the tracker found: the node to report, the global path it holds, and the use.
type Tracked struct {
	Node  *ast.Node
	Path  []string
	Usage Usage
}

// String renders a use for test failures and debugging: the path and how it is used.
func (tracked Tracked) String() string {
	usage := map[Usage]string{Read: "Read", Call: "Call", Construct: "Construct"}[tracked.Usage]
	return usage + " " + strings.Join(tracked.Path, ".") + " at " + strconv.Itoa(tracked.Node.Pos())
}
