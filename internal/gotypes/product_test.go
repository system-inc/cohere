package gotypes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keyFixture writes one checkout at root: a module of its own with one package, which the listing names
// beside a standard package and one from the module cache, and returns that listing as go list would print
// it from there.
func keyFixture(t *testing.T, root string, source string) *listing {
	t.Helper()
	shim := filepath.Join(root, "TypeScript-shim", "ast")
	if err := os.MkdirAll(shim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shim, "go.mod"), []byte("module example.com/shim/ast\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shim, "ast.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	packages := []*listedPackage{
		{ImportPath: "strings", Name: "strings", Standard: true, GoFiles: []string{"builder.go", "strings.go"}},
		{ImportPath: "golang.org/x/sync/errgroup", Name: "errgroup", GoFiles: []string{"errgroup.go"},
			Module: &listedModule{Path: "golang.org/x/sync", Version: "v0.17.0", Dir: filepath.Join(t.TempDir(), "mod", "golang.org", "x", "sync@v0.17.0")}},
		{ImportPath: "example.com/shim/ast", Name: "ast", Dir: shim, GoFiles: []string{"ast.go"},
			Module: &listedModule{Path: "example.com/shim/ast", Dir: shim, GoMod: filepath.Join(shim, "go.mod")}},
	}
	result := &listing{packages: packages, byPath: map[string]*listedPackage{}}
	for _, listedPackage := range packages {
		result.byPath[listedPackage.ImportPath] = listedPackage
	}
	return result
}

var keyEnvironment = map[string]string{"GOVERSION": "go1.27.0", "GOOS": "linux", "GOARCH": "amd64", "GOEXPERIMENT": ""}

var everyPackage = map[string]bool{"strings": true, "golang.org/x/sync/errgroup": true, "example.com/shim/ast": true}

// The same tree keys the same from any checkout path, and a module from the module cache keys by its
// version, never by where that cache sits: what makes a product Workshop built the one a runner finds.
func TestAProductKeysTheSameFromAnotherCheckoutPath(t *testing.T) {
	t.Parallel()

	product := Product{Name: "fixture", Patterns: []string{"./..."}}
	first, second := t.TempDir(), t.TempDir()
	firstKey, firstDescription, err := productKey(product, first, keyEnvironment, keyFixture(t, first, "package ast\n"), everyPackage)
	if err != nil {
		t.Fatal(err)
	}
	secondKey, secondDescription, err := productKey(product, second, keyEnvironment, keyFixture(t, second, "package ast\n"), everyPackage)
	if err != nil {
		t.Fatal(err)
	}
	if firstKey != secondKey || firstDescription != secondDescription {
		t.Fatalf("one tree at two paths keyed %s and %s:\n%s\n%s", firstKey, secondKey, firstDescription, secondDescription)
	}
	if strings.Contains(firstDescription, first) {
		t.Fatalf("the description names the checkout path %s:\n%s", first, firstDescription)
	}
}

// A product's key moves with what changes its export data: a repository file's contents, the Go release,
// the target. The description names the package whose facts moved, so a mismatch is found by reading it.
func TestAProductKeyMovesWithWhatChangesItsExportData(t *testing.T) {
	t.Parallel()

	product := Product{Name: "fixture", Patterns: []string{"./..."}}
	root := t.TempDir()
	baseKey, baseDescription, err := productKey(product, root, keyEnvironment, keyFixture(t, root, "package ast\n"), everyPackage)
	if err != nil {
		t.Fatal(err)
	}

	edited := t.TempDir()
	editedKey, editedDescription, err := productKey(product, edited, keyEnvironment, keyFixture(t, edited, "package ast\n\nvar Planted int\n"), everyPackage)
	if err != nil {
		t.Fatal(err)
	}
	if editedKey == baseKey {
		t.Error("an edited repository file kept the product's key")
	}
	if baseLine, editedLine := packageLine(baseDescription, "example.com/shim/ast"), packageLine(editedDescription, "example.com/shim/ast"); baseLine == editedLine {
		t.Errorf("the description's line for the edited package did not move: %q", baseLine)
	}
	if packageLine(baseDescription, "strings") != packageLine(editedDescription, "strings") {
		t.Error("the description's line for a package nothing changed moved")
	}

	for name, value := range map[string]string{"GOVERSION": "go1.27.1", "GOOS": "darwin", "GOARCH": "arm64", "GOEXPERIMENT": "nosynchashtriemap"} {
		environment := map[string]string{}
		for key, original := range keyEnvironment {
			environment[key] = original
		}
		environment[name] = value
		key, _, err := productKey(product, root, environment, keyFixture(t, root, "package ast\n"), everyPackage)
		if err != nil {
			t.Fatal(err)
		}
		if key == baseKey {
			t.Errorf("%s=%s kept the product's key", name, value)
		}
	}
}

func packageLine(description string, path string) string {
	for _, line := range strings.Split(description, "\n") {
		if strings.HasPrefix(line, "package "+path+" ") {
			return line
		}
	}
	return ""
}

// A package from a directory outside the repository has no path every machine shares, so it is refused a
// key rather than keyed by where it sits here.
func TestAPackageOutsideTheRepositoryIsRefusedAKey(t *testing.T) {
	t.Parallel()

	root, elsewhere := t.TempDir(), t.TempDir()
	listed := keyFixture(t, elsewhere, "package ast\n")
	_, _, err := productKey(Product{Name: "fixture"}, root, keyEnvironment, listed, everyPackage)
	if err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("a package outside the repository was keyed: %v", err)
	}
}

// Export data stands only for a package that imports nothing of the source module: one that does is read
// from source, against the source module as this load reads it.
func TestExportDataStandsOnlyForWhatImportsNothingOfTheSource(t *testing.T) {
	t.Parallel()

	source := &listedModule{Path: "example.com/cohere"}
	outside := &listedModule{Path: "example.com/outside"}
	packages := []*listedPackage{
		{ImportPath: "fmt", Standard: true},
		{ImportPath: "example.com/outside/leaf", Module: outside, Deps: []string{"fmt"}},
		{ImportPath: "example.com/cohere/rule", Module: source, Deps: []string{"example.com/outside/leaf", "fmt"}},
		{ImportPath: "example.com/outside/adapter", Module: outside, Deps: []string{"example.com/cohere/rule", "fmt"}},
		{ImportPath: "example.com/cohere/rules", Module: source, Deps: []string{"example.com/outside/adapter", "example.com/cohere/rule"}},
	}
	listed := &listing{packages: packages}
	exported := exportSet(listed, "example.com/cohere")
	want := map[string]bool{"fmt": true, "example.com/outside/leaf": true}
	if len(exported) != len(want) {
		t.Fatalf("export data stands for %v, want %v", exported, want)
	}
	for path := range want {
		if !exported[path] {
			t.Errorf("export data does not stand for %s, which imports nothing of the source module", path)
		}
	}
}

// A runner of prebuilt tests refuses a go command whose GOFLAGS carries -overlay, and the loader reads an
// overlay itself, so its go commands never see one; every other flag reaches them as set.
func TestTheGoCommandsSeeNoOverlay(t *testing.T) {
	t.Parallel()

	environment := goEnvironment([]string{"GOFLAGS=-p=4 -overlay=/tmp/x.json -tags=planted --overlay=/tmp/y.json"})
	last := ""
	for _, variable := range environment {
		if strings.HasPrefix(variable, "GOFLAGS=") {
			last = variable
		}
	}
	if last != "GOFLAGS=-p=4 -tags=planted" {
		t.Fatalf("the go commands' GOFLAGS is %q, want -p=4 -tags=planted", last)
	}
}

// A product a reader cannot find under its key is named, and so is any product of the same name the cache
// holds under another key, with the first fact the two keyed differently: a runner that computes a key
// other than Workshop's says why instead of reading as a plain miss.
func TestAMissingProductNamesTheKeyTheCacheHoldsInstead(t *testing.T) {
	t.Parallel()

	cache := t.TempDir()
	theirs := "name go types fixture\npatterns ./...\ngo go1.27.0 linux/amd64 GOEXPERIMENT=\npackage strings standard 0123456789ab\n"
	if err := os.WriteFile(filepath.Join(cache, strings.Repeat("a", 64)+".inputs"), []byte(theirs), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, strings.Repeat("b", 64)+".inputs"), []byte("name go types another\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ours := strings.Replace(theirs, "linux/amd64", "darwin/arm64", 1)
	named := otherKeys(Product{Name: "fixture"}, &productState{key: strings.Repeat("c", 64), description: ours, cache: cache})
	if !strings.Contains(named, "aaaaaaaaaaaa") || !strings.Contains(named, "linux/amd64") || !strings.Contains(named, "darwin/arm64") {
		t.Fatalf("the other key and the fact it keyed differently were not named: %q", named)
	}
	if strings.Contains(named, "bbbbbbbbbbbb") {
		t.Fatalf("a product of another name was named: %q", named)
	}
}
