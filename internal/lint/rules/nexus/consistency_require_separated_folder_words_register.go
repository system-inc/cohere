package nexus

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers nexus/consistency-require-separated-folder-words.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing at once touch
// nothing in common.
//
// Its decoder is the folder rules' one: no option is read, and the run's project root is taken, so only the folders
// inside the project are judged.
func init() {
	rule.Register(rule.Registration[ProjectRootOptions]{
		Rule:     ConsistencyRequireSeparatedFolderWords,
		DecodeAt: decodeProjectRootOptions,
	})
}
