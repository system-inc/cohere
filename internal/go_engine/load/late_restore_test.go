package load

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

// lateRestoreFiles are a module whose package user imports base and returns base's Handle, and top, which imports
// user, as the build being loaded holds them.
var lateRestoreFiles = map[string]string{
	"go.mod":       "module example.com/late\n\ngo 1.21\n",
	"base/base.go": "package base\n\ntype Handle struct{ Count int }\n",
	"user/user.go": "package user\n\nimport \"example.com/late/base\"\n\n" +
		"func Open() base.Handle { return base.Handle{} }\n",
	"top/top.go": "package top\n\nimport \"example.com/late/user\"\n\nvar Opened = user.Open()\n",
}

// otherBuildsUserExportData is user's export data as another build checked it, where base also declares Extra, which
// user's API names, so reading it against this build's base would have to find an Extra this base does not declare.
func otherBuildsUserExportData(test *testing.T) []byte {
	test.Helper()

	fileSet := token.NewFileSet()
	parse := func(name string, source string) *ast.File {
		parsed, parseError := parser.ParseFile(fileSet, name, source, 0)
		if parseError != nil {
			test.Fatal(parseError)
		}
		return parsed
	}
	base, baseError := (&types.Config{}).Check("example.com/late/base", fileSet, []*ast.File{parse("base.go",
		"package base\n\ntype Handle struct{ Count int }\n\ntype Extra struct{ Size int }\n")}, nil)
	if baseError != nil {
		test.Fatal(baseError)
	}
	configuration := types.Config{Importer: importerFunction(func(string) (*types.Package, error) {
		return base, nil
	})}
	user, userError := configuration.Check("example.com/late/user", fileSet, []*ast.File{parse("user.go",
		"package user\n\nimport \"example.com/late/base\"\n\nfunc Open() base.Handle { return base.Handle{} }\n\n"+
			"func More() base.Extra { return base.Extra{} }\n")}, nil)
	if userError != nil {
		test.Fatal(userError)
	}
	var written bytes.Buffer
	if writeError := gcexportdata.Write(&written, fileSet, user); writeError != nil {
		test.Fatal(writeError)
	}
	return written.Bytes()
}

// lateRestorer restores user alone, decided late by holds, from exportData, and counts the declines.
type lateRestorer struct {
	exportData []byte
	holds      bool
	declined   int
}

// Restore is user's late restore.
func (restorer *lateRestorer) Restore(
	_ string,
	reachable []*packages.Package,
	_ []*packages.Package,
) map[*packages.Package]*Restored {
	restored := map[*packages.Package]*Restored{}
	for _, each := range reachable {
		if each.PkgPath != "example.com/late/user" {
			continue
		}
		restored[each] = &Restored{
			ReadExportData: func() ([]byte, error) { return restorer.exportData, nil },
			Imports:        []string{"example.com/late/base"},
			Holds:          func(map[string]*types.Package) bool { return restorer.holds },
			Declined:       func() { restorer.declined++ },
		}
	}
	return restored
}

// A package whose restore is decided late and turned down, or whose export data then does not read, is checked from
// source after all and told so once, and leaves what it imports as the load checked it: base's scope holds the same
// names before and after, never an object another build's export data named (#b2t9xmf). The decision comes before
// anything is read, since export data read against a package that lacks an object it names may declare it there.
func TestADeclinedRestoreLeavesItsImportsScopesAsChecked(test *testing.T) {
	test.Parallel()

	for _, step := range []struct {
		name       string
		exportData func(test *testing.T) []byte
		holds      bool
	}{
		{"turned down", otherBuildsUserExportData, false},
		{"export data that does not read", func(*testing.T) []byte { return []byte("not export data") }, true},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			directory := test.TempDir()
			writeListingFiles(test, directory, lateRestoreFiles)
			restorer := &lateRestorer{exportData: step.exportData(test), holds: step.holds}
			loaded, loadError := PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
				Caching{Restorer: restorer})
			if loadError != nil {
				test.Fatal(loadError)
			}
			if len(loaded.Diagnostics) > 0 {
				test.Fatalf("the load found %+v", loaded.Diagnostics)
			}
			var user *packages.Package
			var base *packages.Package
			packages.Visit(loaded.Roots, nil, func(visited *packages.Package) {
				switch visited.PkgPath {
				case "example.com/late/user":
					user = visited
				case "example.com/late/base":
					base = visited
				}
			})
			if user == nil || base == nil {
				test.Fatal("the load reached no user or no base")
			}
			if restorer.declined != 1 || len(user.Syntax) != 1 || loaded.PackagesFromExportData != 0 {
				test.Errorf("want user declined once and checked from source, got %d declines, %d files parsed, %d "+
					"packages from export data", restorer.declined, len(user.Syntax), loaded.PackagesFromExportData)
			}
			if names := base.Types.Scope().Names(); !slices.Equal(names, []string{"Handle"}) {
				test.Errorf("base's scope holds %q after the declined restore, want only its own Handle", names)
			}
		})
	}
}
