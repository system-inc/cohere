package check

import (
	"crypto/sha256"
	"go/types"
	"os"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// diskDigests is the sha256 of each file's bytes on disk, as a build that parsed them now would hold them.
func diskDigests(test *testing.T) func(parsed string) ([]byte, bool) {
	return func(parsed string) ([]byte, bool) {
		contents, readError := os.ReadFile(parsed)
		if readError != nil {
			test.Errorf("reading %s for its digest: %v", parsed, readError)
			return nil, false
		}
		digest := sha256.Sum256(contents)
		return digest[:], true
	}
}

// The placed hand answers a question about a function of a package it holds by the function's place in the package's
// own syntax, only for a function standing: bottom's Double, standing, is answered its own answer and its parameter
// its own, and twice, not standing, is left to the resolver, as is a kind the host never answered of Double and an
// object of top, a package the hand was not handed.
func TestThePlacedHandAnswersAStandingFunctionByItsPlace(test *testing.T) {
	test.Parallel()
	loaded := placedModule(test, test.TempDir(), "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	var bottom *packages.Package
	var top *packages.Package
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
	digest := sha256.Sum256([]byte(placedBottom))
	hand := newPlacedHand()
	hand.hold(bottom, []fact_resolver.PlacedAnswer{
		{Kind: "function_purity", File: file, FileDigest: digest[:], Offset: doubleAt, Parameter: -1,
			Encoded: []byte("true")},
		{Kind: "odd_slice_argument", File: file, FileDigest: digest[:], Offset: doubleAt, Parameter: 0,
			Encoded: []byte("false")},
		{Kind: "function_purity", File: file, FileDigest: digest[:], Offset: twiceAt, Parameter: -1,
			Encoded: []byte("true")},
	}, func(function fact_resolver.Place) bool {
		// Asked of a function, never of a parameter: a parameter's answer stands with its function.
		return function.Offset == doubleAt && function.Parameter == -1
	}, diskDigests(test))
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

// The placed hand refuses every place in a file whose bytes differ in the build from the host's, though an offset still
// lines up (#k7k3wwc): bottom's Double, answered by the host at its `func`, is answered so while the build parses the
// host's bytes, and left to the resolver once the build's bottom.go doubles by three, with Double where it was.
func TestThePlacedHandRefusesAFileWhoseBytesDiffer(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	hostLoaded := placedModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	file := bottomFileOf(test, hostLoaded)
	doubleAt := strings.Index(placedBottom, "func Double(")
	digest := sha256.Sum256([]byte(placedBottom))
	placed := []fact_resolver.PlacedAnswer{
		{Kind: "function_purity", File: file, FileDigest: digest[:], Offset: doubleAt, Parameter: -1,
			Encoded: []byte("true")},
	}
	var hostBottom *packages.Package
	for _, each := range hostLoaded {
		if each.PkgPath == "example.com/placed/bottom" {
			hostBottom = each
		}
	}
	same := newPlacedHand()
	same.hold(hostBottom, placed, func(fact_resolver.Place) bool { return true }, diskDigests(test))
	if encoded, answered := same.Answer("function_purity", hostBottom.Types.Scope().Lookup("Double")); !answered ||
		string(encoded) != "true" {
		test.Fatalf("want Double answered true while the build parses the host's bytes, got %q (%t)", encoded,
			answered)
	}
	edited := strings.Replace(placedBottom, "value * 2", "value * 3", 1)
	if writeError := os.WriteFile(file, []byte(edited), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	buildLoaded, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	var bottom *packages.Package
	for _, each := range buildLoaded.Packages {
		if each.PkgPath == "example.com/placed/bottom" {
			bottom = each
		}
	}
	hand := newPlacedHand()
	hand.hold(bottom, placed, func(fact_resolver.Place) bool { return true }, diskDigests(test))
	if encoded, answered := hand.Answer("function_purity", bottom.Types.Scope().Lookup("Double")); answered {
		test.Errorf("want Double, in a file whose bytes differ, left to the resolver, got %q", encoded)
	}
}
