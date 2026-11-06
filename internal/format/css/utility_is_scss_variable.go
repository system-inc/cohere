package css

// src/language-css/utilities/is-scss-variable.js.

import (
	"strings"

	"github.com/system-inc/cohere/internal/format/estree"
)

func isScssVariable(node *estree.Node, options *parseOptions) bool {
	return options.parser == "scss" &&
		node.Is("word") &&
		strings.HasPrefix(node.String("value"), "$")
}
