package markdown

import (
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/printing"
)

// src/language-markdown/print/sentence.js.

func printSentence(path *astPath, print printing.PrintFunc) doc.Doc {
	parts := []doc.Doc{doc.Text("")}

	path.Each(func(path *astPath, _ int, _ any) {
		node := currentNode(path)
		printed := print(nil, nil)
		if node.NodeType == "whitespace" {
			if _, isText := printed.(doc.Text); !isText {
				parts = append(parts, printed, doc.Text(""))
				return
			}
		}
		parts[len(parts)-1] = doc.Concat{parts[len(parts)-1], printed}
	}, "children")

	return doc.NewFill(parts)
}
