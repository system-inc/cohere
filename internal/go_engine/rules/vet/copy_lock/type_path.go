package copy_lock

import "strings"

// typePath is where a lock sits inside a type, innermost first: the lock, then each type holding it, out to the
// type judged. x/tools' typePath.
type typePath []string

// String is the path as the messages print it, outermost first, `a.Holder contains sync.Mutex`.
func (path typePath) String() string {
	var builder strings.Builder
	for index := range path {
		if index > 0 {
			builder.WriteString(" contains ")
		}
		builder.WriteString(path[len(path)-index-1])
	}
	return builder.String()
}
