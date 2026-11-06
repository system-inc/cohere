package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// consistencyNoUtilsFolderUnderscoreText and consistencyNoUtilsFolderText are the rule's messages,
// whose wording lives in `policy/messages/consistency-no-utils-folder.json`.
var (
	consistencyNoUtilsFolderUnderscoreText = policy.MessageOf("nexus/consistency-no-utils-folder", "noUnderscoreUtils")
	consistencyNoUtilsFolderText           = policy.MessageOf("nexus/consistency-no-utils-folder", "noUtils")
)

// messageUtilsFolder names the banned folder and the name to use instead, under the message for a name
// with leading underscores when it has them.
func messageUtilsFolder(folder string) rule.Message {
	text := consistencyNoUtilsFolderText
	if strings.HasPrefix(folder, "_") {
		text = consistencyNoUtilsFolderUnderscoreText
	}
	return rule.Message{
		Id:          text.Id,
		Description: text.Render(map[string]string{"folder": folder, "replacement": utilsFolderReplacement(folder)}),
	}
}

// ConsistencyNoUtilsFolder bans a folder named `util` or `utils` inside the project, in any casing and
// with any leading underscores: a folder's name is judged lowercased with its leading underscores
// removed, the verdict every engine gives in the text rules corpus (#4b337jk, family `utils-folder`).
//
// The abbreviation is the smaller half of the problem. A folder named for nothing in particular
// collects everything, and "utilities" is not meaningfully better at that, but the spelling is what
// the kingdom settled on and one spelling searched is worth two spellings guessed.
//
// Only folders below the project root count. A checkout under `~/utils/` is not a finding anybody
// inside the project can act on, and matching the absolute path flagged every file of one (uf06). The
// finding is one per file, for the outermost banned folder, at the file's start, so each file moved
// clears its own finding, which suits the per-file cache.
//
// This rule reads only the path, so it is the cheapest kind of rule there is: it decides on a
// string and declines the file before any node is visited.
var ConsistencyNoUtilsFolder = rule.Rule[ProjectRootOptions]{
	Name:       "nexus/consistency-no-utils-folder",
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
			// No root was handed in, so a folder above the project cannot be told from one inside it, and
			// judging the whole path is the guess uf06 measured.
			return nil
		}

		folder, banned := outermostUtilsFolder(settings.ProjectRoot, ruleContext.SourceFile.FileName().AsString())
		if !banned {
			// Declining costs one string scan. Listening would cost a walk of the whole file to
			// learn the same thing.
			return nil
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				ruleContext.ReportRange(node.Loc.WithEnd(node.Loc.Pos()), messageUtilsFolder(folder))
			},
		}
	},
}

// outermostUtilsFolder is the first banned folder between the project root and the file, outermost first,
// and false when there is none or the file is not under the root. The file's own name is never a folder:
// `utils.ts` is a file name.
func outermostUtilsFolder(projectRoot string, fileName string) (string, bool) {
	folders, under := foldersInProject(projectRoot, fileName)
	if !under {
		return "", false
	}
	for _, folder := range folders {
		if isUtilsFolder(folder) {
			return folder, true
		}
	}
	return "", false
}

// isUtilsFolder reports whether a folder's name, lowercased with its leading underscores removed, is
// `util` or `utils`.
//
// The name is matched whole: `utilities`, `myutils` and `utilsomething` pass, since a rule that fires on
// the spelling it is asking for is worse than no rule.
func isUtilsFolder(folder string) bool {
	switch strings.ToLower(strings.TrimLeft(folder, "_")) {
	case "util", "utils":
		return true
	}
	return false
}

// utilsFolderReplacement is the name to use instead of a banned folder: the same leading underscores, then
// `utilities` in the casing the folder's first letter has, or in capitals when every letter is one, as
// cohere-swift's rule of the same name advises.
func utilsFolderReplacement(folder string) string {
	name := strings.TrimLeft(folder, "_")
	underscores := folder[:len(folder)-len(name)]
	switch {
	case name == strings.ToUpper(name):
		return underscores + "UTILITIES"
	case strings.HasPrefix(name, "U"):
		return underscores + "Utilities"
	}
	return underscores + "utilities"
}
