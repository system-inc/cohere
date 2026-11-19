package check

import (
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// The placed hand answers a question about a function of a package it holds by the function's place in the package's
// own syntax, only for a function standing: bottom's Double, standing, is answered its own answer and its parameter
// its own, and twice, not standing, is left to the resolver, as is a kind the host never answered of Double and an
// object of top, a package the hand was not handed.
func TestThePlacedHandAnswersAStandingFunctionByItsPlace(test *testing.T) {
	test.Parallel()
	loaded := placedModule(test, test.TempDir(), "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	var bottom, top *packages.Package
	for _, each := range loaded {
		switch each.PkgPath {
		case "example.com/placed/bottom":
			bottom = each
		case "example.com/placed/top":
			top = each
		}
	}
	if bottom == nil || top == nil {
		test.Fatal("the module holds no bottom and top")
	}
	file := bottomFileOf(test, loaded)
	doubleAt, twiceAt := strings.Index(placedBottom, "func Double("), strings.Index(placedBottom, "func twice(")
	hand := newPlacedHand()
	hand.hold(bottom, []fact_resolver.PlacedAnswer{
		{Kind: "function_purity", File: file, Offset: doubleAt, Parameter: -1, Encoded: []byte("true")},
		{Kind: "odd_slice_argument", File: file, Offset: doubleAt, Parameter: 0, Encoded: []byte("false")},
		{Kind: "function_purity", File: file, Offset: twiceAt, Parameter: -1, Encoded: []byte("true")},
	}, func(function fact_resolver.Place) bool { return function.Offset == doubleAt })
	function := func(scope *types.Scope, name string) *types.Func {
		declared, isFunction := scope.Lookup(name).(*types.Func)
		if !isFunction {
			test.Fatalf("no function %s", name)
		}
		return declared
	}
	double := function(bottom.Types.Scope(), "Double")
	values := double.Signature().Params().At(0)
	for name, asked := range map[string]struct {
		kind   string
		object types.Object
		want   string
	}{
		"Double, standing":               {kind: "function_purity", object: double, want: "true"},
		"Double's parameter":             {kind: "odd_slice_argument", object: values, want: "false"},
		"twice, not standing":            {kind: "function_purity", object: function(bottom.Types.Scope(), "twice")},
		"a kind the host never answered": {kind: "return_nil_ness", object: double},
		"a package not handed":           {kind: "function_purity", object: function(top.Types.Scope(), "Use")},
	} {
		encoded, answered := hand.Answer(asked.kind, asked.object)
		if answered != (asked.want != "") || string(encoded) != asked.want {
			test.Errorf("%s: want %q answered (%t), got %q (%t)", name, asked.want, asked.want != "", encoded,
				answered)
		}
	}
}
