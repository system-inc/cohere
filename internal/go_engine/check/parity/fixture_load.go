package parity

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// fixtureLoad is one load of the fixtures a Require function is given: the folder go runs in, the patterns it lists
// there, and, for a fixture module named by a pattern, that pattern, which names its files in findings and records.
//
// A test says its fixtures are modules by giving each its go.mod, as the go command itself reads a folder, so a
// fixture whose check reads the Go version loads under the version its go.mod names, as staticcheck's testutil runs
// each version's folder. A pattern naming a folder of testdata that holds a go.mod is a module, loaded whole from
// there as `./...`, and each such module is a load of its own; the other patterns name packages of the tree under
// testdata's src, loaded together. A testdata that holds a go.mod itself is one module, its patterns listed there, as
// analysistest loads one.
type fixtureLoad struct {
	directory string
	patterns  []string

	// module is the pattern that named a fixture module, or empty for the tree under src and for a testdata that is
	// itself a module.
	module string
}

// fixtureLoads is the loads that the fixtures patterns name in testdata take, the tree's first and then each module's
// in the order of patterns.
func fixtureLoads(testdata string, patterns []string) []fixtureLoad {
	if isModule(testdata) {
		return []fixtureLoad{{directory: testdata, patterns: patterns}}
	}
	tree := []string{}
	modules := []fixtureLoad{}
	for _, pattern := range patterns {
		directory := filepath.Join(testdata, filepath.FromSlash(pattern))
		if !isModule(directory) {
			tree = append(tree, pattern)
			continue
		}
		modules = append(modules, fixtureLoad{directory: directory, patterns: []string{"./..."}, module: pattern})
	}
	if len(tree) == 0 {
		return modules
	}
	return append([]fixtureLoad{{directory: testdata, patterns: tree}}, modules...)
}

// isModule is whether directory holds a go.mod.
func isModule(directory string) bool {
	_, statError := os.Stat(filepath.Join(directory, "go.mod"))
	return statError == nil
}

// fixtureEnvironment is the environment a fixture tree loads under, as analysistest loads one: a module when
// testdata holds a go.mod, and otherwise a tree laid out by import path under src.
func fixtureEnvironment(testdata string) []string {
	if isModule(testdata) {
		work := filepath.Join(testdata, "go.work")
		if _, workError := os.Stat(work); workError != nil {
			work = "off"
		}
		return []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=" + work}
	}
	return []string{"GOPATH=" + testdata, "GO111MODULE=off", "GOWORK=off"}
}

// described is the load as a failure names it: its patterns, or the module a pattern named.
func (fixtures fixtureLoad) described() string {
	if fixtures.module != "" {
		return "the module " + fixtures.module
	}
	return fmt.Sprint(fixtures.patterns)
}

// fileName is a file of the load as findings, records and failures name it: its path below the fixtures' root, so
// two files of one name in different folders never pair, as every version's folder of staticcheck's fixtures holds
// one. The root is testdata's src in the tree, as `go1.0/CheckIneffectiveRandInt/CheckIneffectiveRandInt.go`, and
// testdata itself when it is a module; in a module a pattern named, it is the module's folder, the path after that
// pattern, as `module_one/inner/inner.go`. A file outside the root, as a related entry can name in the standard
// library, keeps its path.
func (fixtures fixtureLoad) fileName(file string) string {
	root := fixtures.directory
	if fixtures.module == "" && !isModule(fixtures.directory) {
		root = filepath.Join(fixtures.directory, "src")
	}
	below, relativeError := filepath.Rel(root, file)
	if relativeError != nil || strings.HasPrefix(below, "..") {
		return filepath.ToSlash(file)
	}
	return path.Join(fixtures.module, filepath.ToSlash(below))
}

// loadedFixtures is one load's root packages, beside the load.
type loadedFixtures struct {
	fixtureLoad
	roots []*packages.Package
}

// recordsFolder is the folder that holds the one fixture package fixtures loaded, read from what the load found rather
// than from where a layout would put it: a module a pattern named is its own folder, and otherwise it is the folder
// the root package's files sit in, wherever the go command found them, under src in a tree or wherever a module's
// go.work points, as time_until's differences module sits under its testdata's src. A package and its test variants
// share that folder, so roots in more than one folder, or none, fail the test, since records read from a guessed
// folder would read as none.
func recordsFolder(test testing.TB, fixtures loadedFixtures) string {
	test.Helper()
	if fixtures.module != "" {
		return fixtures.directory
	}
	folders := []string{}
	for _, root := range fixtures.roots {
		for _, file := range root.GoFiles {
			if folder := filepath.Dir(file); !slices.Contains(folders, folder) {
				folders = append(folders, folder)
			}
		}
	}
	if len(folders) != 1 {
		test.Fatalf("the packages of %s sit in %d folders, %v, where a differences fixture is one package's, so its "+
			"records could not be read", fixtures.described(), len(folders), folders)
	}
	return folders[0]
}

// loadFixtures loads the fixture packages patterns name from testdata for target, a load for the tree and one for
// each module. A load that found nothing fails the test, since silence there would read as agreement, and so do the
// fixture packages' load errors unless they are exactly expected (see requireLoadErrors). With nothing expected, as
// every caller but RequireIdenticalDespiteLoadErrors expects, a fixture package that did not load clean fails on its
// own. A `.fixed` file beside a fixture package it loads fails too, named, since only RequireOnlyAdjudicated and
// RequirePortWhereOriginalPanics hold one (see requireFixedFiles), and those two load through
// loadFixturesHoldingFixed: under any other form it would be read by nothing, and its fixes held by nothing.
func loadFixtures(
	test testing.TB,
	target Target,
	testdata string,
	patterns []string,
	expected []LoadError,
) []loadedFixtures {
	test.Helper()
	loads := loadFixturesHoldingFixed(test, target, testdata, patterns, expected)
	for _, fixtures := range loads {
		for _, fixed := range fixedFilesBeside(test, fixtures) {
			test.Errorf("%s is a fixed file beside a fixture package this call loads, and it holds no fixed file, so "+
				"nothing reads it: hold it with RequireOnlyAdjudicated or RequirePortWhereOriginalPanics, or delete it",
				fixtures.fileName(fixed))
		}
	}
	return loads
}

// fixedFilesBeside is every `.fixed` file in a folder that holds a root package of fixtures, each once and sorted.
func fixedFilesBeside(test testing.TB, fixtures loadedFixtures) []string {
	test.Helper()
	fixed := []string{}
	for _, root := range fixtures.roots {
		for _, file := range slices.Concat(root.GoFiles, root.OtherFiles, root.IgnoredFiles) {
			names, globError := filepath.Glob(filepath.Join(filepath.Dir(file), "*.fixed"))
			if globError != nil {
				test.Fatalf("listing the fixed files beside %s: %v", fixtures.fileName(file), globError)
			}
			for _, name := range names {
				if !slices.Contains(fixed, name) {
					fixed = append(fixed, name)
				}
			}
		}
	}
	slices.Sort(fixed)
	return fixed
}

// loadFixturesHoldingFixed is loadFixtures for a form that holds the `.fixed` files beside the fixture package it
// loads, so it leaves them to that form's requireFixedFiles.
func loadFixturesHoldingFixed(
	test testing.TB,
	target Target,
	testdata string,
	patterns []string,
	expected []LoadError,
) []loadedFixtures {
	test.Helper()
	loads := []loadedFixtures{}
	loadErrors := []LoadError{}
	for _, fixtures := range fixtureLoads(testdata, patterns) {
		loaded, loadError := load.Packages(fixtures.directory, fixtures.patterns, target.environmentOf(test, fixtures),
			nil)
		if loadError != nil {
			test.Fatalf("loading %s: %v", fixtures.described(), loadError)
		}
		// The checker passes over a root that does not type-check without a word, so a fixture with one type error
		// would compare nothing and read green. There must be a root, and every root must load clean or with exactly
		// the errors expected.
		if len(loaded.Roots) == 0 {
			test.Fatalf("loading %s found no package, so the comparison would prove nothing", fixtures.described())
		}
		if len(expected) == 0 {
			for _, failure := range LoadFailures(loaded.Roots) {
				test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %s",
					failure.Package, failure.Error)
			}
		}
		loadErrors = append(loadErrors, loadErrorsOf(loaded.Roots, fixtures.fileName)...)
		loads = append(loads, loadedFixtures{fixtureLoad: fixtures, roots: loaded.Roots})
	}
	requireLoadErrors(test, loadErrors, expected)
	return loads
}
