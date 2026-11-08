package load

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// A standard package's tests import a vendored package its own files do not, as net's external tests import
// x/net's nettest: go list prints the test imports resolved, `vendor/example.org/x`, and its ImportMap names only the
// package's own imports. Both test variants still key the import by the path the test files write, so each resolves
// its own import, and the vendored package the tests reach that imports p is built again against "p [p.test]". No
// standard package's in-package tests import a vendored package its own files do not, so the graph is read directly.
func TestAStandardPackagesTestsKeyAVendoredImportByItsWrittenPath(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	vendored := &listedPackage{
		ImportPath: "vendor/example.org/x", Name: "x",
		Directory: filepath.Join(directory, "vendor", "example.org", "x"), GoFiles: []string{"x.go"},
		Imports: []string{"p"}, Deps: []string{"p"},
	}
	root := &listedPackage{
		ImportPath: "p", Name: "p", Directory: filepath.Join(directory, "p"), GoFiles: []string{"p.go"},
		TestGoFiles: []string{"p_test.go"}, XTestGoFiles: []string{"external_test.go"},
		TestImports: []string{"testing", "vendor/example.org/x"}, XTestImports: []string{"p", "vendor/example.org/x"},
	}
	listed := &listing{
		packages: []*listedPackage{root, vendored},
		byPath:   map[string]*listedPackage{"p": root, "vendor/example.org/x": vendored},
		roots:    map[string]bool{"p": true},
		testOnly: map[string]bool{"vendor/example.org/x": true},
	}
	built := newGraph(listed, directory, nil)
	for _, identifier := range []string{"p [p.test]", "p_test [p.test]"} {
		variant := built.nodes[identifier]
		if variant == nil {
			test.Fatalf("want %s in the graph, got none", identifier)
		}
		if got := variant.imports["example.org/x"]; got != "vendor/example.org/x [p.test]" {
			test.Errorf("want %s to import example.org/x as vendor/example.org/x [p.test], got %q of %v",
				identifier, got, variant.imports)
		}
		if _, found := variant.imports["vendor/example.org/x"]; found {
			test.Errorf("%s keys an import by its resolved path, which no test file writes: %v", identifier,
				variant.imports)
		}
	}
}

// net's external tests import x/net's nettest, which net's own files do not, and they load, as go test builds them:
// "net_test [net.test]" imports nettest by the path its file writes, as nettest rebuilt against "net [net.test]",
// since nettest imports net. Loaded from the toolchain's own source under linux/amd64, so the load is the same on
// every host; one of std's packages, with its tests, is a few seconds of checking.
func TestNetsExternalTestsImportTheVendoredNettest(test *testing.T) {
	test.Parallel()

	goRoot, goEnvironmentError := exec.Command("go", "env", "GOROOT").Output()
	if goEnvironmentError != nil {
		test.Fatalf("reading GOROOT with go env: %v", goEnvironmentError)
	}
	source := filepath.Join(strings.TrimSpace(string(goRoot)), "src")
	loaded, loadError := Packages(source, []string{"net"},
		[]string{"GOWORK=off", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"}, nil)
	if loadError != nil {
		test.Fatalf("loading net: %v", loadError)
	}
	if len(loaded.Diagnostics) > 0 {
		test.Fatalf("want net and its tests loaded with no diagnostics, got %+v", loaded.Diagnostics)
	}
	var external *packages.Package
	for _, analyzed := range loaded.Packages {
		if analyzed.ID == "net_test [net.test]" {
			external = analyzed
		}
	}
	if external == nil {
		test.Fatalf("want net_test [net.test] analyzed, got %q", identifiers(loaded))
	}
	nettest := external.Imports["golang.org/x/net/nettest"]
	if nettest == nil || nettest.ID != "vendor/golang.org/x/net/nettest [net.test]" || nettest.Types == nil {
		test.Fatalf("want net_test to import golang.org/x/net/nettest as vendor/golang.org/x/net/nettest [net.test], "+
			"checked, got %+v", nettest)
	}
}

// moduleVendorFiles is a module that vendors the one module it requires, whose package p imports nothing and whose
// tests, in-package and external, import the vendored package.
var moduleVendorFiles = map[string]string{
	"vendoring/go.mod": "module example.com/vendoring\n\ngo 1.21\n\nrequire example.org/dependency v1.0.0\n",
	"vendoring/vendor/modules.txt": "# example.org/dependency v1.0.0\n## explicit; go 1.21\n" +
		"example.org/dependency\n",
	"vendoring/vendor/example.org/dependency/dependency.go": "package dependency\n\nconst Name = \"dependency\"\n",
	"vendoring/p/p.go": "package p\n\nconst Name = \"p\"\n",
	"vendoring/p/p_test.go": "package p\n\nimport (\n\t\"testing\"\n\n\t\"example.org/dependency\"\n)\n\n" +
		"func TestName(test *testing.T) { _ = dependency.Name + Name }\n",
	"vendoring/p/external_test.go": "package p_test\n\nimport (\n\t\"testing\"\n\n\t\"example.com/vendoring/p\"\n" +
		"\t\"example.org/dependency\"\n)\n\nfunc TestNames(test *testing.T) { _ = dependency.Name + p.Name }\n",
}

// A module's own vendor directory holds packages by their import paths, as go list lists them, so a module's tests
// import a vendored package by the path they write with nothing to map back: both test variants load, each importing
// example.org/dependency from the vendor directory, which only a standard package's test imports needed mapping for.
func TestAModulesTestsImportItsVendoredPackage(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeListingFiles(test, directory, moduleVendorFiles)
	workspace := filepath.Join(directory, "vendoring")
	loaded, loadError := Packages(workspace, []string{"./..."}, []string{"GOWORK=off", "GOFLAGS=-mod=vendor"}, nil)
	if loadError != nil {
		test.Fatalf("loading the vendoring module: %v", loadError)
	}
	if len(loaded.Diagnostics) > 0 {
		test.Fatalf("want the vendoring module loaded with no diagnostics, got %+v", loaded.Diagnostics)
	}
	vendoredDirectory := filepath.Join(workspace, "vendor", "example.org", "dependency")
	analyzed := map[string]*packages.Package{}
	for _, each := range loaded.Packages {
		analyzed[each.ID] = each
	}
	for _, identifier := range []string{
		"example.com/vendoring/p [example.com/vendoring/p.test]",
		"example.com/vendoring/p_test [example.com/vendoring/p.test]",
	} {
		variant := analyzed[identifier]
		if variant == nil {
			test.Fatalf("want %s analyzed, got %q", identifier, identifiers(loaded))
		}
		dependency := variant.Imports["example.org/dependency"]
		if dependency == nil || dependency.ID != "example.org/dependency" || dependency.Dir != vendoredDirectory ||
			dependency.Types == nil {
			test.Errorf("want %s to import example.org/dependency from %s, checked, got %+v", identifier,
				vendoredDirectory, dependency)
		}
	}
}
