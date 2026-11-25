package flagged

// Multi-byte runes before the reference move its column, not its order in the file.
func greeting() string {
	return "héllo, wörld 你好 " + farewell // want "`farewell` is a package-level constant"
}

// A //line directive renames the lines below it, and the order in the file still decides.
//
//line renamed.go:1
func renamedReader() int {
	return renamedLimit // want "`renamedLimit` is a package-level variable"
}

var renamedLimit = 4

const farewell = "goodbye"
