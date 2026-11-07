package nexus

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/folder_words"
	"github.com/system-inc/cohere/internal/lint/ecmascript/nextjs"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// consistencyRequireSeparatedFolderWordsText is the rule's message, whose wording lives in
// `policy/messages/consistency-require-separated-folder-words.json`.
var consistencyRequireSeparatedFolderWordsText = policy.MessageOf(
	"nexus/consistency-require-separated-folder-words",
	"runTogetherProjectFolder",
)

// typeScriptFolderSpelling separates a folder's words with a hyphen, an underscore, or a capital after a lowercase
// letter: serviceTypes and ProfileImages separate their words by case, and whether a folder should be kebab-case is
// a casing question, not this rule's. A split takes the separator the folder already uses between two parts, by case
// where it separates them by case, and a hyphen where it uses none, since every multi-word folder in the house's
// TypeScript trees that uses no capitals is spelled with hyphens (net-worth, sign-in, social-media), and a Next.js
// route folder is a URL segment, where the hyphen is the web's separator.
var typeScriptFolderSpelling = folder_words.Spelling{Separators: "-_", Joiner: "-", CaseSeparates: true}

// nodeModulesFolder holds what Node's resolution installs. A file below one is a package's, and so are its folders.
const nodeModulesFolder = "node_modules"

// ConsistencyRequireSeparatedFolderWords reports a folder inside the project whose name runs words together:
// profileimages, not profile-images (#narw7aq, the text rules family `separated-folder-words`). It is the
// TypeScript twin of cohere-go/consistency-require-separated-folder-words, and both read one judgment,
// internal/folder_words: a name is cut into parts at its hyphens and underscores and where a capital follows a
// lowercase letter, a part that is a word passes, and a part that splits into two or more words is the finding, with
// the split as the name it asks for.
//
// A mind reading cold reads `format-options` at a glance and has to stop and parse `formatoptions`, and a folder's
// name is read before anything in it, on every import line and every path.
//
// Only the folders this project names are judged, and each is told by structure, never by a list:
//
//   - Folders between the project root and the file. The root's own name is the checkout's, which nobody inside
//     can rename, as Go's rule leaves a module's root alone.
//   - Nothing below node_modules, which holds packages, not the project's source. Discovery never enters one, and a
//     tsconfig that includes one is the only way a file of it reaches a rule. Build output and other generated
//     trees are ignored by .gitignore, which discovery never enters either, so no rule sees them.
//   - No App Router folder, which Next.js reads by its punctuation (nextjs.IsRoutingFolder): `[id]` is a key the
//     code reads as `params.id`, `(os-layout)` and `(.)photo` a group and an interception, `@modal` a slot. A
//     private folder's underscore is Next's, and the name after it is the project's, so `_components` is judged,
//     and passes.
//
// The finding is one per file, at the file's start, for the outermost folder that runs its words together, as
// nexus/consistency-no-utils-folder's is: a TypeScript rule sees one file, and a per-file cache keeps one file's
// findings, so a finding that sat once per folder would move to another file whenever the first was renamed or
// deleted. Renaming the folder clears every file's finding in it at once, and the next folder down, if it runs
// together too, is reported then.
//
// A folder that mirrors another project's name, a brand or a package, keeps it by the name joining
// policy/HouseWords.txt with its owner's page, as the census's Cloudflare and PlanetScale did, so every engine reads
// it as one word. The rule offers no per-folder exemption, and a per-file finding would need a directive in every
// file of the folder.
//
// The rule reads only the path, so it decides on a string and declines the file before any node is visited.
var ConsistencyRequireSeparatedFolderWords = rule.Rule[ProjectRootOptions]{
	Name:       "nexus/consistency-require-separated-folder-words",
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
			// No root was handed in, so the checkout's own folders cannot be told from the project's, and
			// judging the whole path would report names nobody inside the project can change.
			return nil
		}

		folder, replacement, runTogether := outermostRunTogetherFolder(
			settings.ProjectRoot,
			ruleContext.SourceFile.FileName().AsString(),
		)
		if !runTogether {
			// Declining costs one walk of the path. Listening would cost a walk of the whole file to learn the
			// same thing.
			return nil
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				ruleContext.ReportRange(node.Loc.WithEnd(node.Loc.Pos()), rule.Message{
					Id: consistencyRequireSeparatedFolderWordsText.Id,
					Description: consistencyRequireSeparatedFolderWordsText.Render(map[string]string{
						"folder":      folder,
						"replacement": replacement,
					}),
				})
			},
		}
	},
}

// outermostRunTogetherFolder is the first folder between the project root and the file, outermost first, whose
// name runs its words together, with the name it asks for, and false when there is none, when the file is not under
// the root, or when the file sits below node_modules.
func outermostRunTogetherFolder(projectRoot string, fileName string) (string, string, bool) {
	folders, under := foldersInProject(projectRoot, fileName)
	if !under || slices.Contains(folders, nodeModulesFolder) {
		return "", "", false
	}
	for _, folder := range folders {
		if nextjs.IsRoutingFolder(folder) {
			continue
		}
		if replacement, runTogether := folder_words.Separated(folder, typeScriptFolderSpelling); runTogether {
			return folder, replacement, true
		}
	}
	return "", "", false
}
