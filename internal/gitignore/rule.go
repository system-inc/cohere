package gitignore

import "strings"

// rule is one compiled pattern line.
type rule struct {
	source        Source
	negated       bool
	directoryOnly bool

	// anchored is a pattern matched against the path below base rather than against a base name. base is
	// the directory of the file the rule came from, relative to the root, "" at the root.
	anchored bool
	base     string
	glob     glob
}

func (candidate *rule) matches(relativePath string, baseName string) bool {
	if !candidate.anchored {
		return candidate.glob.matches(baseName, false)
	}
	below := relativePath
	if candidate.base != "" {
		var ok bool
		below, ok = strings.CutPrefix(relativePath, candidate.base+"/")
		if !ok {
			return false
		}
	}
	return candidate.glob.matches(below, true)
}
