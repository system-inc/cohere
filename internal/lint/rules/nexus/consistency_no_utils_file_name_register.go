package nexus

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers nexus/consistency-no-utils-file-name.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing at once touch
// nothing in common.
//
// Its decoder is the folder rules' one: no option is read, and the run's project root is taken, so only the files
// inside the project are judged, and none under a folder of someone else's.
func init() {
	rule.Register(rule.Registration[ProjectRootOptions]{
		Rule:     ConsistencyNoUtilsFileName,
		DecodeAt: decodeProjectRootOptions,
	})
}
