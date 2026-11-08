package go_engine

import (
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// describedProject is what the run looks at, read once before the first record, since each part of it can
// refuse the run and a refusal must come before any record.
type describedProject struct {
	workspace project.Workspace

	// readableOurs are the census's files that are ours and could be read, before the build constraints narrow
	// them: what the load accounts for. An unreadable file is left out, since the load
	// reads each file it accounts for and one it cannot open fails it whole; the file is already a record.
	readableOurs []string

	// goEnvironment is go env's answer the census read the build constraints by, which every load of the run takes
	// in place of asking again, nil when the census judged no file (#f53yra8).
	goEnvironment *load.GoEnvironment

	// manifest is the root's go.work, or its go.mod for a module root: where a diagnostic that names no file
	// is placed, since the front door refuses a finding with no position.
	manifest string

	// record is the project record.
	record contract.ProjectRecord

	// inScope are the files that are ours, not excluded, and inside the paths, sorted; unreadable ones too.
	inScope    []string
	inScopeSet map[string]bool

	// constrainedInScope are the readable files that are ours and inside the paths but that the build constraints
	// leave out on this platform: no phase here reads them, and a rename proven under the platform that builds one
	// may write it.
	constrainedInScope map[string]bool

	// contents are the in-scope files that could be read, as they were read.
	contents map[string][]byte

	// unreadable are the in-scope files that could not be read, each written before the first phase.
	unreadable []contract.UnreadableRecord
}

// readable are the in-scope files that could be read, in order: the files a phase that reads source is given.
func (described *describedProject) readable() []string {
	files := make([]string, 0, len(described.contents))
	for _, file := range described.inScope {
		if _, read := described.contents[file]; read {
			files = append(files, file)
		}
	}
	return files
}

// scopeWords says what the fix phase formatted, for its record's formatScope.
func (described *describedProject) scopeWords() string {
	if described.record.ScopeDescription != "" {
		return described.record.ScopeDescription
	}
	if described.workspace.FromWorkspace {
		return "every Go file in the workspace"
	}
	return "every Go file in the module"
}
