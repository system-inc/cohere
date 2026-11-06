package optionschema

import (
	"fmt"
	"strconv"
	"strings"
)

// Violation is the first place an option list departs from upstream's schema: where, as a path into the
// list ESLint validates (`[0].allow[1].from`), and what the schema expected there.
type Violation struct {
	Path     string
	Expected string
}

// Error names the element the way a config author counts them, the first after the severity being
// element 1, and the path inside it: `element 1 at allow[1].from: upstream's schema expects ...`.
func (violation *Violation) Error() string {
	where := violation.Path
	if rest, isElement := strings.CutPrefix(violation.Path, "["); isElement {
		if closing := strings.Index(rest, "]"); closing > 0 {
			if index, parseError := strconv.Atoi(rest[:closing]); parseError == nil {
				where = "element " + strconv.Itoa(index+1)
				if inner := strings.TrimPrefix(rest[closing+1:], "."); inner != "" {
					where += " at " + inner
				}
			}
		}
	}
	return fmt.Sprintf("%s: upstream's schema, which ESLint validates against, expects %s", where, violation.Expected)
}
