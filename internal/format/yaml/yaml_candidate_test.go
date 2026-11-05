package yaml

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/format/formatoptions"
)

// yamlCandidate is the port, measured two ways. The full stack is Format: the Go parser and the printer,
// from the text. The tree loader prints the trees the real parser built, parsed by Node in batches before
// the comparison starts, so the printer is measured apart from the parser; Format then checks it is
// printing the text the tree was parsed from.
type yamlCandidate struct {
	options   formatoptions.Options
	fullStack bool
	texts     map[string]string
	trees     map[string]parsed
}

func (candidate yamlCandidate) Handles(fileName string) bool {
	extension := strings.ToLower(filepath.Ext(fileName))
	return extension == ".yaml" || extension == ".yml"
}

func (candidate yamlCandidate) Format(fileName string, text string) (string, error) {
	if candidate.fullStack {
		return formatFullStack(fileName, text, candidate.options, "preserve", nil)
	}
	normalized, hasByteOrderMark := normalizeInput(text)
	if candidate.texts[fileName] != normalized {
		return "", fmt.Errorf("the file changed after it was parsed")
	}
	return formatParsed(
		fileName,
		candidate.trees[fileName],
		normalized,
		hasByteOrderMark,
		candidate.options,
		"preserve",
		nil,
	)
}
