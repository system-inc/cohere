package contract

// ProjectRecord is what the run looked at: every file the project holds, the ones that are ours, the
// ones in scope, and each file left out with its reason.
type ProjectRecord struct {
	Root                string `json:"root"`
	Package             string `json:"package"`
	ElapsedMilliseconds int    `json:"elapsedMilliseconds"`
	FilesInPackage      int    `json:"filesInPackage"`
	FilesOurs           int    `json:"filesOurs"`
	FilesInScope        int    `json:"filesInScope"`
	// ScopeDescription is printed in the parenthesis of `N in scope (…)`; empty on a whole-project run.
	ScopeDescription string          `json:"scopeDescription"`
	Targets          []ProjectTarget `json:"targets"`
	Excluded         []ExcludedFile  `json:"excluded"`
}

func (ProjectRecord) kind() Kind { return KindProject }
