package go_engine

import (
	"go/constant"
	"go/token"
	"go/types"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// A use reaches its body by the load mode (#w72zxtq) when its object's package is one the host and the build load in
// different modes, one from export data and the other from source: export data holds an untyped float as a 512-bit
// binary fraction and a check from source holds it exactly, so at cohere's root, where the host reads math from the
// system cache and a build under `--build-dependence` checks it from source, goja's math.Sqrt2 / 2 type-checks to two
// values. The Square ruled the rule general: every use of any object of such a package reaches, of any kind, and only
// a package loaded alike, or one the host does not hold, reaches nothing. The census still counts the narrower rule it
// replaced, an untyped float or complex constant read by its value alone. The engine's fixture cannot load a package
// two ways, since a test's host checks the standard library from source, so the rule is pinned here.
func TestAUseReachesItsBodyWhereTheLoadModesDiffer(test *testing.T) {
	test.Parallel()

	math := types.NewPackage("math", "math")
	unheld := types.NewPackage("example.com/unheld", "unheld")
	sqrt2 := types.NewConst(token.NoPos, math, "Sqrt2", types.Typ[types.UntypedFloat],
		constant.MakeFromLiteral("1.41421356237309504880168872420969807856967187537694807317667974", token.FLOAT, 0))
	imaginary := types.NewConst(token.NoPos, math, "Imaginary", types.Typ[types.UntypedComplex],
		constant.MakeImag(constant.MakeInt64(1)))
	maximum := types.NewConst(token.NoPos, math, "MaxInt8", types.Typ[types.UntypedInt], constant.MakeInt64(127))
	typed := types.NewConst(token.NoPos, math, "Typed", types.Typ[types.Float64], constant.MakeFloat64(0.5))
	function := types.NewFunc(token.NoPos, math, "Abs", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	elsewhere := types.NewConst(token.NoPos, unheld, "Pi", types.Typ[types.UntypedFloat],
		constant.MakeFloat64(3.14))
	named := build_reads.UsedNamed
	byValue := build_reads.UsedNamed | build_reads.UsedConstant
	cases := []struct {
		name            string
		object          types.Object
		kinds           build_reads.UseKinds
		hostExportData  bool
		buildFromSource bool
		general         bool
		constantsOnly   bool
	}{
		{"an untyped float, export data on the host and source in the build", sqrt2, byValue, true, true, true,
			true},
		{"an untyped float, source on the host and export data in the build", sqrt2, byValue, false, false, true,
			true},
		{"an untyped float, source in both", sqrt2, byValue, false, true, false, false},
		{"an untyped float, export data in both", sqrt2, byValue, true, false, false, false},
		{"an untyped complex, export data on the host and source in the build", imaginary, byValue, true, true, true,
			true},
		{"an untyped integer, export data on the host and source in the build", maximum, byValue, true, true, true,
			false},
		{"a typed float, export data on the host and source in the build", typed, byValue, true, true, true, false},
		{"a function, export data on the host and source in the build", function, named, true, true, true, false},
		{"a function, source in both", function, named, false, true, false, false},
		{"an untyped float of a package the host does not hold", elsewhere, byValue, true, true, false, false},
	}
	for _, each := range cases {
		reacher := &bodyReacher{
			host: &bodyReachHost{identifiers: map[*types.Package]string{math: "math"},
				dependence: &hostDependence{exportData: map[string]bool{"math": each.hostExportData}}},
			fromSource: map[string]bool{"math": each.buildFromSource},
		}
		if got := reacher.loadModeReaches(each.object, each.kinds); got != each.general {
			test.Errorf("%s: the load mode reaches its body %t, want %t", each.name, got, each.general)
		}
		reacher.constantsOnly = true
		if got := reacher.loadModeReaches(each.object, each.kinds); got != each.constantsOnly {
			test.Errorf("%s: the constants-only rule reaches its body %t, want %t", each.name, got,
				each.constantsOnly)
		}
	}
}
