// Package load_memo keeps what a rule reads off one package's types once for the whole load, however many packages
// of the load reach that one. A rule that reads the packages a package imports, every method they declare, would
// otherwise read a package the whole tree imports, as fmt is, again for every importer.
//
// The rules that read the import closure, consistency_no_any and consistency_no_abbreviated_identifier, keep their
// reads here, so the two cannot drift on how long an entry lives or what it is keyed by. What each reads off a
// package stays its own, given as the function a memo computes.
package load_memo

import (
	"go/token"
	"runtime"
	"sync"
	"weak"

	"github.com/system-inc/go-types/types"
)

// Memo is what a rule reads off each package, by the load the package belongs to. A run of the engine can load the
// tree more than once, a rename's rounds each load it again, so an entry is a load's rather than the process's: it
// holds its load's types, and is let go when the load's file set is collected.
type Memo[Value any] struct {
	// compute reads the value off one package, once for each package of each load.
	compute func(declaring *types.Package) Value
	mutex   sync.Mutex
	// byLoad are the loads the memo holds, by the file set each load parsed into, which every pass of the load is
	// given as pass.Fset. The file set is held weakly, since nothing in the types an entry holds leads back to it, so
	// the entry alone never keeps a load alive.
	byLoad map[weak.Pointer[token.FileSet]]*Load[Value]
}

// New is a memo that reads each package's value with compute. Nothing compute reads may depend on the package that
// reaches the declaring one, since every package of the load is handed what the first to ask read.
func New[Value any](compute func(declaring *types.Package) Value) *Memo[Value] {
	return &Memo[Value]{compute: compute, byLoad: map[weak.Pointer[token.FileSet]]*Load[Value]{}}
}

// Load is the memo of the load whose files were parsed into fileSet.
func (memo *Memo[Value]) Load(fileSet *token.FileSet) *Load[Value] {
	key := weak.Make(fileSet)
	memo.mutex.Lock()
	defer memo.mutex.Unlock()
	load, known := memo.byLoad[key]
	if !known {
		load = &Load[Value]{compute: memo.compute, byPackage: map[*types.Package]func() Value{}}
		memo.byLoad[key] = load
		runtime.AddCleanup(fileSet, func(collected weak.Pointer[token.FileSet]) {
			memo.mutex.Lock()
			defer memo.mutex.Unlock()
			delete(memo.byLoad, collected)
		}, key)
	}
	return load
}
