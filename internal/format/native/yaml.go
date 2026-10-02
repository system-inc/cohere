package native

import (
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/format/printing"
	"github.com/system-inc/cohere/internal/format/yaml"
)

// The YAML printer: Prettier's language-yaml printer over yaml-unist-parser and eemeli/yaml, ported in
// internal/format/yaml. Its doc entry is what markdown's front matter embeds, stripped of the trailing
// hardline as upstream's textToDoc strips it.
func init() {
	print := func(fileName string, text string, options prettier.Options) (string, error) {
		return yaml.Format(fileName, text, options, TextToDoc(options, "yaml"))
	}
	printDoc := func(_ string, text string, options prettier.Options, _ string, _ string, textToDoc printing.TextToDoc) (doc.Doc, error) {
		printed, err := yaml.FormatDoc(text, options, textToDoc)
		if err != nil {
			return nil, err
		}
		return doc.StripTrailingHardline(printed), nil
	}
	for _, extension := range []string{".yaml", ".yml"} {
		Register(extension, print)
		RegisterDoc(extension, printDoc)
	}
}
