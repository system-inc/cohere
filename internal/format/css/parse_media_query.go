package css

// src/language-css/parse/parse-media-query.js.

import (
	"github.com/system-inc/cohere/internal/format/css/mediaquery"
	"github.com/system-inc/cohere/internal/format/estree"
)

func parseMediaQuery(parameters string) *estree.Node {
	result, parseError := mediaquery.Parse(parameters)
	if parseError != nil {
		// Ignore bad media queries
		/* c8 ignore next 4 */
		return estree.New("selector-unknown", 0, 0, []estree.Property{{Key: "value", Value: estree.String(parameters)}})
	}

	addTypePrefix(addMissingType(result), "media-", nil)
	return result
}
