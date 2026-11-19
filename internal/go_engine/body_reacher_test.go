package go_engine

import (
	"go/constant"
	"go/token"
	"go/types"
	"testing"
)

// A constant use reaches its body by the load mode (#w72zxtq) when the constant is an untyped float or complex of a
// package the host and the build load in different modes, one from export data, which holds such a value as a 512-bit
// binary fraction, and the other from source, which holds it exactly: at cohere's root the host reads math from the
// system cache and a build under `--build-dependence` checks it from source, and goja's math.Sqrt2 / 2 type-checks to
// two values. Loaded alike, the value is the same, and an integer constant or a typed float, written exactly or
// rounded to its type in both, never reaches; nor does a package the host does not hold. The engine's fixture cannot
// load a package two ways, since a test's host checks the standard library from source, so the rule is pinned here.
func TestAnUntypedFloatReachesItsBodyWhereTheLoadModesDiffer(test *testing.T) {
	test.Parallel()

	math := types.NewPackage("math", "math")
	unheld := types.NewPackage("example.com/unheld", "unheld")
	sqrt2 := types.NewConst(token.NoPos, math, "Sqrt2", types.Typ[types.UntypedFloat],
		constant.MakeFromLiteral("1.41421356237309504880168872420969807856967187537694807317667974", token.FLOAT, 0))
	imaginary := types.NewConst(token.NoPos, math, "Imaginary", types.Typ[types.UntypedComplex],
		constant.MakeImag(constant.MakeInt64(1)))
	maximum := types.NewConst(token.NoPos, math, "MaxInt8", types.Typ[types.UntypedInt], constant.MakeInt64(127))
	typed := types.NewConst(token.NoPos, math, "Typed", types.Typ[types.Float64], constant.MakeFloat64(0.5))
	elsewhere := types.NewConst(token.NoPos, unheld, "Pi", types.Typ[types.UntypedFloat],
		constant.MakeFloat64(3.14))
	reacherOf := func(hostExportData bool, buildFromSource bool) *bodyReacher {
		return &bodyReacher{
			host: &bodyReachHost{identifiers: map[*types.Package]string{math: "math"},
				dependence: &hostDependence{exportData: map[string]bool{"math": hostExportData}}},
			fromSource: map[string]bool{"math": buildFromSource},
		}
	}
	cases := []struct {
		name            string
		object          types.Object
		hostExportData  bool
		buildFromSource bool
		want            bool
	}{
		{"an untyped float, export data on the host and source in the build", sqrt2, true, true, true},
		{"an untyped float, source on the host and export data in the build", sqrt2, false, false, true},
		{"an untyped float, source in both", sqrt2, false, true, false},
		{"an untyped float, export data in both", sqrt2, true, false, false},
		{"an untyped complex, export data on the host and source in the build", imaginary, true, true, true},
		{"an untyped integer, export data on the host and source in the build", maximum, true, true, false},
		{"a typed float, export data on the host and source in the build", typed, true, true, false},
		{"an untyped float of a package the host does not hold", elsewhere, true, true, false},
	}
	for _, each := range cases {
		reacher := reacherOf(each.hostExportData, each.buildFromSource)
		if got := reacher.loadModeDiffers(each.object); got != each.want {
			test.Errorf("%s: the load mode reaches its body %t, want %t", each.name, got, each.want)
		}
	}
}
