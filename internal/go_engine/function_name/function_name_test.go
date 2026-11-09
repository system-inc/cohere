package function_name_test

import (
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/function_name"
)

// Every function the names fixture declares or uses, and every function of the packages it imports, is asked
// about every such function's full name and names a step from them, and Matches must answer what comparing
// FullName answers, and what comparing typeutil.FuncName answers, which spells it the same way. The test fails
// unless it met methods and functions, each matching and not, so it can't pass by asking nothing of one.
func TestMatchesAgreesWithFullName(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"names"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading names: %v", loadError)
	}
	var functions []*types.Func
	seen := map[*types.Func]bool{}
	add := func(object types.Object) {
		function, isFunction := object.(*types.Func)
		if isFunction && !seen[function] {
			seen[function] = true
			functions = append(functions, function)
		}
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
		for _, object := range loadedPackage.TypesInfo.Defs {
			add(object)
		}
		for _, object := range loadedPackage.TypesInfo.Uses {
			add(object)
		}
		for _, imported := range loadedPackage.Types.Imports() {
			scope := imported.Scope()
			for _, name := range scope.Names() {
				add(scope.Lookup(name))
				if named, isNamed := types.Unalias(scope.Lookup(name).Type()).(*types.Named); isNamed {
					for method := range named.Methods() {
						add(method)
					}
				}
			}
		}
	}
	// Functions no package declares, or whose package has no path, come only from a hand-built type, so the fixture
	// holds two, each spelled by its name alone.
	unsigned := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	add(types.NewFunc(token.NoPos, nil, "orphan", unsigned))
	add(types.NewFunc(token.NoPos, types.NewPackage("", "pathless"), "pathless", unsigned))
	// Each function is asked about its own full name, names a step from it (a letter short, a letter long, the
	// package's path cut, the receiver without its parentheses or its pointer), and every full name that ends in
	// its own short name, the names it could be mistaken for.
	bySuffix := map[string][]string{}
	for _, function := range functions {
		bySuffix[function.Name()] = append(bySuffix[function.Name()], function.FullName())
	}
	met := map[string]int{}
	questions := 0
	for _, function := range functions {
		full := function.FullName()
		if typeutil.FuncName(function) != full {
			test.Fatalf("%s: typeutil.FuncName spells %q and FullName %q, so one helper can't answer both",
				function, typeutil.FuncName(function), full)
		}
		asked := append([]string{full, full[:len(full)-1], full + "s", function.Name(),
			strings.TrimPrefix(strings.Replace(full, ").", ".", 1), "("), strings.Replace(full, "(*", "(", 1)},
			bySuffix[function.Name()]...)
		if index := strings.LastIndex(full, "."); index > 0 {
			asked = append(asked, full[:index]+"/"+full[index+1:])
		}
		kind := "function"
		if function.Signature().Recv() != nil {
			kind = "method"
		}
		for _, name := range asked {
			want := full == name
			if got := function_name.Matches(function, name); got != want {
				test.Errorf("%s about %q: Matches %t, FullName comparison %t", full, name, got, want)
			}
			questions++
			met[kind+" "+map[bool]string{true: "matching", false: "not"}[want]]++
		}
		if got, want := function_name.Index(function, asked...), slices.Index(asked, full); got != want {
			test.Errorf("%s: Index %d, want the first name equal to its FullName, %d", full, got, want)
		}
	}
	test.Logf("%d functions, %d questions, %v", len(functions), questions, met)
	for _, label := range []string{"function matching", "function not", "method matching", "method not"} {
		if met[label] == 0 {
			test.Errorf("the fixture never met %s, so the comparison proves nothing of it", label)
		}
	}
}
