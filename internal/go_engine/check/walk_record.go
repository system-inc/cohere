package check

import (
	"sync"

	"golang.org/x/tools/go/packages"
)

// walkRecord is each analyzed package's files' walk, taken as the checker finishes the package, before it lets go
// of the package's syntax and type information (checker.Options.Finished), so the packages a run judges leave the
// live heap as early as their dependencies do, rather than all at once when the run ends. The collector marks the
// live heap on every cycle, and under the engine's memory cap a cold run cycles whenever the heap reaches the
// limit, so what stays live is what each cycle costs.
type walkRecord struct {
	lock  sync.Mutex
	files map[*packages.Package][]storedFile
}

// take records finished's files' walk, for checker.Options.Finished, which calls it from the action that finished
// the package, so several at once.
func (record *walkRecord) take(finished *packages.Package) {
	files := storedFiles(finished)
	record.lock.Lock()
	defer record.lock.Unlock()
	record.files[finished] = files
}
