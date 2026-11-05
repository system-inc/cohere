package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A go.work's modules are its uses, minus any inside a submodule, found by its gitlink in the index rather
// than by its name; a nested module's pattern is its module path, which reaches it through the workspace
// from the root.
func TestReadWorkspaceLeavesOutAModuleInsideASubmodule(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	covered, readError := ReadWorkspace(built.repository)
	if readError != nil {
		test.Fatal(readError)
	}
	if !covered.FromWorkspace || covered.Root != built.repository {
		test.Errorf("read %s as a workspace %t, want true, at %s",
			built.repository, covered.FromWorkspace, covered.Root)
	}
	wantModules := []Module{{Directory: "."}, {Directory: "inner", Path: "example.com/root/inner"}}
	if !slices.Equal(covered.Modules, wantModules) {
		test.Errorf("modules are %v, want %v", covered.Modules, wantModules)
	}
	if patterns := covered.Patterns(); !slices.Equal(patterns, []string{"./...", "example.com/root/inner/..."}) {
		test.Errorf("patterns are %q, want the root's and inner's", patterns)
	}
	if !slices.Equal(covered.InSubmodule, []string{"library/mod"}) {
		test.Errorf("left to a submodule: %q, want library/mod", covered.InSubmodule)
	}
	if !slices.Equal(covered.Submodules, []string{"library"}) {
		test.Errorf("submodules are %q, want library", covered.Submodules)
	}
	if environment := covered.Environment(); len(environment) != 0 {
		test.Errorf("a workspace root's go commands add %q, want nothing", environment)
	}
}

// A go.work in no repository, as a fresh `go work init` in a temporary directory is, has no index and so no
// gitlinks: it reads its uses without asking git, which would refuse it as "not a git repository".
func TestReadWorkspaceOutsideARepositoryReadsWithoutGit(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	if repository := nearestRepository(built.outside); repository != "" {
		test.Fatalf("the fixture's temporary directory is inside the repository at %s, so this proves nothing",
			repository)
	}
	covered, readError := ReadWorkspace(built.outside)
	if readError != nil {
		test.Fatalf("a workspace in no repository was not read: %v", readError)
	}
	wantModules := []Module{{Directory: "."}, {Directory: "tool", Path: "example.com/outside/tool"}}
	if !covered.FromWorkspace || !slices.Equal(covered.Modules, wantModules) || len(covered.Submodules) != 0 {
		test.Errorf("read %+v, want a workspace of %v with no submodules", covered, wantModules)
	}
	if refusal := covered.RefuseUnlistedModules(); refusal != nil {
		test.Errorf("a workspace using every module it holds was refused: %v", refusal)
	}
}

// A root with a `.git` is in a repository whether or not git can read it, so a git that fails there is still
// an error rather than read as no repository and no submodules.
func TestReadWorkspaceInARepositoryGitCannotReadFails(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	if _, readError := ReadWorkspace(built.broken); readError == nil {
		test.Error("a workspace whose repository git cannot read was read, as if it had no submodules")
	}
}

// A root without a go.work is its one module, with no git to ask and nothing to refuse.
func TestReadWorkspaceOfALoneModuleIsThatModule(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	covered, readError := ReadWorkspace(built.lone)
	if readError != nil {
		test.Fatal(readError)
	}
	if covered.FromWorkspace || !slices.Equal(covered.Modules, []Module{{Directory: "."}}) {
		test.Errorf("a lone go.mod read as %+v, want its one module", covered)
	}
	if patterns := covered.Patterns(); !slices.Equal(patterns, []string{"./..."}) {
		test.Errorf("patterns are %q, want ./...", patterns)
	}
	if refusal := covered.RefuseUnlistedModules(); refusal != nil {
		test.Errorf("a lone module refused: %v", refusal)
	}
}

// A module root runs with `GOWORK=off`, because a go.work above it that does not use it makes go refuse the
// module's packages. nested is that module: go refuses its package as the workspace leaves it, and lists it
// with the environment. `go list -m` would not show this, since it lists the workspace's modules either way.
func TestReadWorkspaceOfAModuleUnderAnotherWorkspaceTurnsTheWorkspaceOff(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	nested := filepath.Join(built.repository, "nested")
	covered, readError := ReadWorkspace(nested)
	if readError != nil {
		test.Fatal(readError)
	}
	if environment := covered.Environment(); !slices.Equal(environment, []string{"GOWORK=off"}) {
		test.Fatalf("a module root's go commands add %q, want GOWORK=off", environment)
	}

	listPackage := func(extra []string) (string, error) {
		command := exec.Command("go", "list", ".")
		command.Dir = nested
		command.Env = append(withoutVariable(os.Environ(), "GOWORK"), "GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=")
		command.Env = append(command.Env, extra...)
		output, runError := command.CombinedOutput()
		return strings.TrimSpace(string(output)), runError
	}
	if output, listError := listPackage(nil); listError == nil {
		test.Errorf("go listed the package under a go.work that does not use it, as %q, so GOWORK=off proves nothing",
			output)
	}
	output, listError := listPackage(covered.Environment())
	if listError != nil || output != "example.com/nested" {
		test.Errorf("with %q go listed %q (%v), want example.com/nested", covered.Environment(), output, listError)
	}
}

// A go.mod go.work doesn't use is refused, by path and with the use line to add, rather than left ungated,
// which is how the shims went untested. One in testdata, in a dot directory or inside a submodule is not.
// Reading the workspace refuses nothing: for the engine an unlisted module is a project of its own.
func TestRefuseUnlistedModulesNamesAModuleGoWorkDoesNotUse(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	covered, readError := ReadWorkspace(built.repository)
	if readError != nil {
		test.Fatalf("reading a workspace with an unlisted module failed, want it read: %v", readError)
	}
	refusal := covered.RefuseUnlistedModules()
	if refusal == nil || !strings.Contains(refusal.Error(), "nested/go.mod: add `./nested` to go.work's use") {
		test.Fatalf("an unlisted module was not refused by path: %v", refusal)
	}
	for _, quiet := range []string{"testdata", ".cache", "library"} {
		if strings.Contains(refusal.Error(), quiet) {
			test.Errorf("the refusal names %s, which is not a module of the repository's: %v", quiet, refusal)
		}
	}

	listed := covered
	listed.Modules = append(slices.Clone(covered.Modules), Module{Directory: "nested", Path: "example.com/nested"})
	if listedRefusal := listed.RefuseUnlistedModules(); listedRefusal != nil {
		test.Errorf("with the module used, the workspace was still refused: %v", listedRefusal)
	}
}

func withoutVariable(environment []string, name string) []string {
	kept := make([]string, 0, len(environment))
	for _, variable := range environment {
		if !strings.HasPrefix(variable, name+"=") {
			kept = append(kept, variable)
		}
	}
	return kept
}
