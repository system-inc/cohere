package load

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// missingRequirementFiles is a module root that imports a module its go.mod replaces with a directory beside it but
// never requires, so go list under -mod=mod adds the requirement to go.mod, and under -mod=readonly reports it missing.
var missingRequirementFiles = map[string]string{
	"go.mod":            "module example.com/a\n\ngo 1.21\n\nreplace example.com/other => ../other\n",
	"a.go":              "package a\n\nimport \"example.com/other\"\n\n// Value is other's.\nconst Value = other.Value\n",
	"../other/go.mod":   "module example.com/other\n\ngo 1.21\n",
	"../other/other.go": "package other\n\n// Value is one.\nconst Value = 1\n",
}

// missingSumFiles is a module root that requires the fixture proxy's example.com/dep with no go.sum line for it, so go
// list under -mod=mod writes the sums to go.sum, and under -mod=readonly reports them missing.
var missingSumFiles = map[string]string{
	"go.mod": "module example.com/a\n\ngo 1.21\n\nrequire example.com/dep v1.0.0\n",
	"go.sum": "",
	"a.go":   "package a\n\nimport \"example.com/dep\"\n\n// Value is dep's.\nconst Value = dep.Value\n",
}

// readModuleFiles is the bytes of a module root's go.mod and go.sum, a missing go.sum read as none.
func readModuleFiles(test *testing.T, root string) []byte {
	test.Helper()
	modules, readError := os.ReadFile(filepath.Join(root, "go.mod"))
	if readError != nil {
		test.Fatal(readError)
	}
	sums, sumsError := os.ReadFile(filepath.Join(root, "go.sum"))
	if sumsError != nil && !os.IsNotExist(sumsError) {
		test.Fatal(sumsError)
	}
	return append(append(modules, 0), sums...)
}

// listedAsTheUserWould runs go list of the module root's packages under environment as given, the user's own flags
// and nothing of the load's, and returns its go.mod and go.sum after.
func listedAsTheUserWould(test *testing.T, root string, environment []string) []byte {
	test.Helper()
	command := exec.Command("go", "list", "-e", "-deps", "-json", "./...")
	command.Dir = root
	command.Env = append(append(os.Environ(), environment...), "PWD="+root)
	if output, runError := command.CombinedOutput(); runError != nil {
		test.Fatalf("go list under the user's flags: %v: %s", runError, output)
	}
	return readModuleFiles(test, root)
}

// A load of a module root whose `GOFLAGS` say -mod=mod leaves its go.mod and go.sum byte for byte as they were
// (#ycf4240): under -mod=mod go list writes a requirement the go.mod lacks, and the sums the go.sum lacks, so a
// `--no-fix` check would edit tracked files. The load runs its go commands at -mod=readonly instead, so go reports the
// missing requirement or sums as the load's diagnostics rather than writing them. Each fixture rewrites its file when
// go list runs under the user's flags alone, so the fixtures can fail.
func TestALoadUnderModModLeavesAModuleRootsFilesAsTheyWere(test *testing.T) {
	test.Parallel()
	for name, files := range map[string]map[string]string{
		"a requirement go.mod lacks": missingRequirementFiles,
		"sums go.sum lacks":          missingSumFiles,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			workspace, environment := writeStaleWorkspace(test, files)
			// The fixture's own -modcacherw kept, so the test's cleanup can remove the module cache go writes.
			environment = append(environment, "GOWORK=off", "GOFLAGS=-modcacherw -mod=mod")
			before := readModuleFiles(test, workspace)

			// Under the user's flags alone, go list rewrites the module's files, so the fixture is one go writes.
			unguarded := listedAsTheUserWould(test, workspace, environment)
			if bytes.Equal(unguarded, before) {
				test.Fatalf("go list under -mod=mod left the module's files as they were, so the fixture proves " +
					"nothing")
			}
			writeListingFiles(test, workspace, files)
			before = readModuleFiles(test, workspace)

			goEnvironment := goEnvironmentOf(test, workspace, environment)
			if flags := goEnvironment.value("GOFLAGS"); !strings.HasSuffix(flags, "-mod=readonly") {
				test.Errorf("the load's go commands run with GOFLAGS %q, want -mod=readonly last", flags)
			}
			loaded, loadError := PackagesCached(workspace, []string{"./..."}, environment, nil,
				Caching{GoEnvironment: goEnvironment})
			if loadError != nil {
				test.Fatal(loadError)
			}
			if after := readModuleFiles(test, workspace); !bytes.Equal(after, before) {
				test.Errorf("the load rewrote the module's files:\n%q\nwant them as they were:\n%q", after, before)
			}
			if len(loaded.Diagnostics) == 0 {
				test.Errorf("the load reported nothing of what -mod=mod would have written")
			}
		})
	}
}

// go commands run with -mod=mod only when the last -mod of `GOFLAGS` says so, one dash or two.
func TestGoFlagsRewriteModulesUnderTheLastModAlone(test *testing.T) {
	test.Parallel()
	for goFlags, wanted := range map[string]bool{
		"":                            false,
		"-mod=mod":                    true,
		"--mod=mod -p=4":              true,
		"-mod=mod -mod=readonly":      false,
		"-mod=readonly -mod=mod":      true,
		"-mod=vendor":                 false,
		"-modfile=other.mod":          false,
		"-modcacherw -tags=mod":       false,
		"-trimpath -mod=mod -pgo=off": true,
	} {
		if rewrites := goFlagsRewriteModules(goFlags); rewrites != wanted {
			test.Errorf("GOFLAGS %q: rewrites modules %v, want %v", goFlags, rewrites, wanted)
		}
	}
}
