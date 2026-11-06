package nexus

import (
	"sort"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// correctnessNoProcessExitAfterOutputWalk names the `try` statements of one root and the chain of
// them each write sits in.
type correctnessNoProcessExitAfterOutputWalk struct {
	root   *ast.Node
	chains map[*ast.Node]string
	tries  map[*ast.Node]int
}

func (walk *correctnessNoProcessExitAfterOutputWalk) tryId(statement *ast.Node) int {
	id, seen := walk.tries[statement]
	if !seen {
		id = len(walk.tries)
		walk.tries[statement] = id
	}
	return id
}

// chainOf is the set of `try` statements, with a `catch`, whose `try` block holds the write: the
// statements whose `catch` a path may enter with the write made or with the write the thing that
// threw. It is written as sorted ids, `,`-separated, with a `,` at each end so a membership test is
// a substring test.
func (walk *correctnessNoProcessExitAfterOutputWalk) chainOf(write *ast.Node) string {
	if chain, seen := walk.chains[write]; seen {
		return chain
	}
	var ids []int
	previous := write
	for current := write.Parent; current != nil && current != walk.root; current = current.Parent {
		if current.Kind == ast.KindTryStatement {
			statement := current.AsTryStatement()
			if statement.TryBlock == previous && statement.CatchClause != nil {
				ids = append(ids, walk.tryId(current))
			}
		}
		previous = current
	}
	sort.Ints(ids)
	var chain strings.Builder
	chain.WriteString(",")
	for _, id := range ids {
		chain.WriteString(strconv.Itoa(id))
		chain.WriteString(",")
	}
	walk.chains[write] = chain.String()
	return walk.chains[write]
}
