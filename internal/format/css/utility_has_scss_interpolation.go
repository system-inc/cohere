package css

// src/language-css/utilities/has-scss-interpolation.js.

import (
	"strings"

	"github.com/system-inc/cohere/internal/format/estree"
)

// hasScssInterpolation is not gated on the parser upstream, so it runs for css too.
func hasScssInterpolation(groupList []*estree.Node) bool {
	if len(groupList) > 0 {
		for index := len(groupList) - 1; index > 0; index-- {
			// If we find `#{`, return true.
			if groupList[index].Is("word") &&
				groupList[index].String("value") == "{" &&
				groupList[index-1].Is("word") &&
				strings.HasSuffix(groupList[index-1].String("value"), "#") {
				return true
			}
		}
	}
	return false
}
