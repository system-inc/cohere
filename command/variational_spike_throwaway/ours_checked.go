package main

import (
	"sync"

	"github.com/system-inc/go-types/types"
)

// oursChecked are the packages of ours a plain baseline checked from source, by path. A parallel baseline puts each
// as its job ends while other jobs' checks import from it, so a lock guards them.
type oursChecked struct {
	mutex    sync.Mutex
	packages map[string]*types.Package
}

// newOursChecked holds no package yet.
func newOursChecked() *oursChecked {
	return &oursChecked{packages: map[string]*types.Package{}}
}

// put makes checkedTypes the package of path.
func (checked *oursChecked) put(path string, checkedTypes *types.Package) {
	checked.mutex.Lock()
	defer checked.mutex.Unlock()
	checked.packages[path] = checkedTypes
}

// get is path's package and whether one was put, never one for a nil checked.
func (checked *oursChecked) get(path string) (*types.Package, bool) {
	if checked == nil {
		return nil, false
	}
	checked.mutex.Lock()
	defer checked.mutex.Unlock()
	checkedTypes, isChecked := checked.packages[path]
	return checkedTypes, isChecked
}
