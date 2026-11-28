// Package cross_compile proves cohere compiles for every platform a release ships. It reads the whole
// program, so any edit to it reruns it, and it lives apart from internal/release/packaging so that the edit
// reruns it alone and not packaging's Swift and staging tests too (#nxgt2ca).
//
// It reads the program and not the module: the Windows type-check of every package's tests reads every
// directory of the module, so it lives in cross_compile/windows_tests, and a change to a file no package
// imports reruns that alone and leaves this cached (#0zdyk51). TypeCheck is here for both.
package cross_compile

import (
	"fmt"

	release "github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/testing/go_types"
)

// TypeCheck type-checks the packages patterns name, and everything they import, from source as target
// would compile them, their tests too when tests is set, and returns every error once, or none. It runs only
// `go list -deps -json` and `go env`, which read and never build, so a runner of prebuilt tests can run it
// (#qnnnkbf): go/packages asked go list for -compiled files and test variants, flags such a runner refuses.
func TypeCheck(moduleDirectory string, target release.Target, tests bool, patterns ...string) []string {
	loaded, loadError := go_types.Load(go_types.Configuration{
		Directory:   moduleDirectory,
		Environment: []string{"GOOS=" + target.GoOperatingSystem, "GOARCH=" + target.GoArchitecture, "CGO_ENABLED=0"},
		Tests:       tests,
	}, patterns...)
	if loadError != nil {
		return []string{loadError.Error()}
	}
	if len(loaded.Source) == 0 {
		return []string{fmt.Sprintf("nothing was loaded for %s, so nothing was checked", target)}
	}
	var problems []string
	// With tests a package is checked again with its test files, and against its test variant, and an error
	// in the code they share is reported by each, so each is kept once.
	seen := map[string]bool{}
	for _, problem := range loaded.ErrorsAnywhere() {
		if !seen[problem.Error()] {
			seen[problem.Error()] = true
			problems = append(problems, problem.Error())
		}
	}
	return problems
}
