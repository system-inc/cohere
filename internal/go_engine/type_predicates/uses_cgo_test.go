package type_predicates

import (
	"go/types"
	"testing"
)

// UsesCgo answers whether a package's types import runtime/cgo directly, and nothing else: vet's cgocall and the
// load's never-restore both ask it, so the restore may leave cgocall's property out of a foreign field's rendering
// (see UsesCgo). An indirect import does not count, since cgocall's own check never looked further.
func TestUsesCgoAsksOnlyForADirectImportOfRuntimeCgo(test *testing.T) {
	test.Parallel()
	runtimeCgo := types.NewPackage(RuntimeCgoPath, "cgo")
	between := types.NewPackage("example.com/between", "between")
	between.SetImports([]*types.Package{runtimeCgo})
	direct := types.NewPackage("example.com/direct", "direct")
	direct.SetImports([]*types.Package{runtimeCgo})
	indirect := types.NewPackage("example.com/indirect", "indirect")
	indirect.SetImports([]*types.Package{between})
	for _, each := range []struct {
		checked *types.Package
		want    bool
	}{{direct, true}, {indirect, false}, {between, true}} {
		if got := UsesCgo(each.checked); got != each.want {
			test.Errorf("UsesCgo(%s) is %t, want %t", each.checked.Path(), got, each.want)
		}
	}
}
