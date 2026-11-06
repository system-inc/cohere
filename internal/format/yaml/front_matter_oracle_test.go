package yaml

import (
	"fmt"

	"github.com/system-inc/cohere/internal/format/markdown/mdast"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// frontMatterOracle formats a markdown file with Prettier and keeps the front matter block of the
// output: the first line through the first later line that is the end delimiter ("---", or "..." as
// pandoc allows).
type frontMatterOracle struct{ engine *prettier.Engine }

func (oracle frontMatterOracle) Handles(fileName string) bool { return oracle.engine.Handles(fileName) }

func (oracle frontMatterOracle) Format(fileName string, text string) (string, error) {
	formatted, formatError := oracle.engine.Format(fileName, text)
	if formatError != nil {
		return "", formatError
	}
	normalized, _ := normalizeInput(text)
	frontMatter, _ := mdast.ParseFrontMatter(normalized)
	if frontMatter == nil {
		return "", fmt.Errorf("no front matter")
	}
	return frontMatterBlock(formatted, frontMatter.EndDelimiter), nil
}
