package nexus

import (
	"path"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/file_name_words"
	"github.com/system-inc/cohere/internal/lint/ecmascript/nextjs"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// consistencyNoUtilsFileNameText is the rule's message, whose wording lives in
// `policy/messages/consistency-no-utils-file-name.json`.
var consistencyNoUtilsFileNameText = policy.MessageOf("nexus/consistency-no-utils-file-name", "utilsFileName")

// ConsistencyNoUtilsFileName refuses `util` or `utils` as a word of a file's name, as
// nexus/consistency-no-utils-folder refuses it as a folder's: `utils.ts`, `stringUtils.ts`, `StringUtils.tsx` and
// `string-utils.ts` are each reported once, at the file's start (#2xt6v5r, the text rules family `utils-file-name`).
// A file named for nothing in particular collects whatever has no other home, which is what a folder of that name
// does, one level down.
//
// The words are internal/file_name_words', the judgment the Go engine's cohere-go/consistency-no-utils-file-name
// reads too: a name is cut at its hyphens, underscores and dots and where its case changes, a dotted role a tool
// reads (`.test`, `.d`, `.config`) is no word, and each word is held whole to file_name_words.IsUtils, so
// `utilities.ts` and `myutils.ts` pass, as `utilities` and `myutils` do as folders.
//
// Which files are judged is told by structure, as the folder rules tell it (typeScriptFileNameWords): a file under
// the project root and under no folder whose files are someone else's, whose name is neither a generated file's nor
// one Next.js reads or writes by its exact spelling.
//
// The rule reads only the path, so it decides on a string and declines the file before any node is visited.
var ConsistencyNoUtilsFileName = rule.Rule[ProjectRootOptions]{
	Name:       "nexus/consistency-no-utils-file-name",
	NoListener: rule.NoListenerDeclinesIrrelevantFiles,
	Run: func(
		ruleContext rule.Context,
		options rule.Given[ProjectRootOptions],
	) rule.Listeners {
		if ruleContext.SourceFile == nil {
			return nil
		}
		settings, configured := options.Read()
		if !configured {
			// No root was handed in, so a folder of someone else's inside the project cannot be told from the
			// checkout's own folders, and the folder rules judge nothing then too.
			return nil
		}
		fileName, words, judged := typeScriptFileNameWords(
			settings.ProjectRoot,
			ruleContext.SourceFile.FileName().AsString(),
		)
		if !judged {
			return nil
		}
		word, found := file_name_words.UtilsWord(words)
		if !found {
			// Declining costs one walk of the name. Listening would cost a walk of the whole file to learn the
			// same thing.
			return nil
		}
		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				ruleContext.ReportRange(node.Loc.WithEnd(node.Loc.Pos()), rule.Message{
					Id: consistencyNoUtilsFileNameText.Id,
					Description: consistencyNoUtilsFileNameText.Render(map[string]string{
						"fileName":    fileName,
						"word":        word,
						"replacement": file_name_words.UtilitiesReplacement(word),
					}),
				})
			},
		}
	},
}

// typeScriptFileNameWords is a file's own name and its words, as nexus/consistency-no-utils-file-name and
// nexus/consistency-no-abbreviated-file-name both judge them, and false for a file neither judges: one outside the
// project root, one under a folder whose files are someone else's or a test's input (file_name_words.IsForeignFolder:
// node_modules, vendor, third_party, testdata), one whose name Next.js reads or writes by its exact spelling, and one
// file_name_words does not judge, a generated file's.
func typeScriptFileNameWords(projectRoot string, filePath string) (string, []string, bool) {
	folders, under := foldersInProject(projectRoot, filePath)
	if !under || file_name_words.InForeignFolder(folders) {
		return "", nil, false
	}
	fileName := path.Base(filePath)
	baseName, _, _ := strings.Cut(fileName, ".")
	if nextjs.IsConventionFile(baseName) || nextjs.IsWrittenByNext(fileName) {
		return "", nil, false
	}
	words, judged := file_name_words.Words(fileName, file_name_words.LanguageTypeScript)
	return fileName, words, judged
}
