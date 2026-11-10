package nexus

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// ProjectRootOptions is never written in a config, since the folder rules that take it take no options and its
// decoder refuses any. It carries the project root the run discovered, the one place a rule learns it, so only the
// folders inside the project are judged. nexus/consistency-no-utils-folder and
// nexus/consistency-require-separated-folder-words read it, and so do nexus/consistency-no-utils-file-name and
// nexus/consistency-no-abbreviated-file-name, to tell a folder of someone else's inside the project.
type ProjectRootOptions struct {
	// ProjectRoot is the project's absolute root, spelled as tspath spells a file name.
	ProjectRoot string `json:"-"`
}

// decodeProjectRootOptions takes the project root from the run, for a bare severity, and refuses any option
// element, as the config layer refuses one for a rule with no decoder.
//
// With no root known it fails, and the config layer then runs the rule unconfigured, which judges nothing. A
// lint run always knows its root; a harness that hands no base is the only caller left without one.
func decodeProjectRootOptions(raw []byte, base rule.OptionsBase) (ProjectRootOptions, error) {
	if len(raw) > 0 {
		return ProjectRootOptions{}, fmt.Errorf(
			"the rule takes no options, and the config gives it %s, which it would never read. "+
				"Remove the options or write the bare severity",
			raw,
		)
	}
	if base.ProjectRoot == "" {
		return ProjectRootOptions{}, fmt.Errorf(
			"no project root is known, so no folder inside the project could be told from one above it",
		)
	}
	return ProjectRootOptions{ProjectRoot: tspath.NormalizePath(base.ProjectRoot)}, nil
}

// foldersInProject are the folders between the project root and the file, outermost first, and false when the
// file is not under the root. The root's own name is never among them, since nobody inside the project can rename
// it, and the file's own name is never a folder.
func foldersInProject(projectRoot string, fileName string) ([]string, bool) {
	relative, under := strings.CutPrefix(fileName, strings.TrimSuffix(projectRoot, "/")+"/")
	if !under {
		return nil, false
	}
	segments := strings.Split(relative, "/")
	return segments[:len(segments)-1], true
}
