// Package locks is copylocks' walk of a type for a lock it holds by value, shared by the copylocks rule and the
// restore from the host's run, which renders another package's unexported field by what that walk finds in it
// (#b2t9xmf).
package locks

import "strings"

// Path is where a lock sits inside a type, innermost first: the lock, then each type holding it, out to the type
// judged. x/tools' copylock typePath.
type Path []string

// String is the path as copylocks' messages print it, outermost first, `a.Holder contains sync.Mutex`.
func (path Path) String() string {
	var builder strings.Builder
	for index := range path {
		if index > 0 {
			builder.WriteString(" contains ")
		}
		builder.WriteString(path[len(path)-index-1])
	}
	return builder.String()
}
