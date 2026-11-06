package nexus

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
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

// ConsistencyNoUtilsFolderOptions is never written in a config, since the rule takes no options and its
// decoder refuses any. It carries the project root the run discovered, the one place a rule learns it, so
// only the folders inside the project are judged.
type ConsistencyNoUtilsFolderOptions struct {
	// ProjectRoot is the project's absolute root, spelled as tspath spells a file name.
	ProjectRoot string `json:"-"`
}

// decodeConsistencyNoUtilsFolderOptions takes the project root from the run, for a bare severity, and refuses
// any option element, as the config layer refuses one for a rule with no decoder.
//
// With no root known it fails, and the config layer then runs the rule unconfigured, which judges nothing. A
// lint run always knows its root; a harness that hands no base is the only caller left without one.
func decodeConsistencyNoUtilsFolderOptions(
	raw []byte,
	base rule.OptionsBase,
) (ConsistencyNoUtilsFolderOptions, error) {
	if len(raw) > 0 {
		return ConsistencyNoUtilsFolderOptions{}, fmt.Errorf(
			"the rule takes no options, and the config gives it %s, which it would never read. "+
				"Remove the options or write the bare severity",
			raw,
		)
	}
	if base.ProjectRoot == "" {
		return ConsistencyNoUtilsFolderOptions{}, fmt.Errorf(
			"no project root is known, so no folder inside the project could be told from one above it",
		)
	}
	return ConsistencyNoUtilsFolderOptions{ProjectRoot: tspath.NormalizePath(base.ProjectRoot)}, nil
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
var ConsistencyNoUtilsFolder = rule.Rule[ConsistencyNoUtilsFolderOptions]{
	Name:       "nexus/consistency-no-utils-folder",
	NoListener: rule.NoListenerDeclinesIrrelevantFiles,
	Run: func(
		ruleContext rule.Context,
		options rule.Given[ConsistencyNoUtilsFolderOptions],
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
	relative, under := strings.CutPrefix(fileName, strings.TrimSuffix(projectRoot, "/")+"/")
	if !under {
		return "", false
	}
	segments := strings.Split(relative, "/")
	for _, segment := range segments[:len(segments)-1] {
		if isUtilsFolder(segment) {
			return segment, true
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
