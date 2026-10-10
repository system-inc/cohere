package guard

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/gotypes"
)

// lintTypes is the export data of every package outside cohere that ./internal/lint/... imports: the
// standard library, the TypeScript checker and its shims, the modules they need. The guards read cohere's own
// packages from source, overlay and all, and those from this product, which is built where building is
// allowed and never here: a guard that compiled ran `go list -compiled`, which a runner of prebuilt tests
// refuses (#qnnnkbf).
var lintTypes = gotypes.Product{
	Name:      "lint",
	Test:      "TestProduct_LintTypes (internal/guard)",
	Directory: "../..",
	Patterns:  []string{"./internal/lint/..."},
}

// Not parallel: it builds the product every other test here reads, so it finishes before they resume. A
// runner of prebuilt tests skips it and finds the product Workshop built.
func TestProduct_LintTypes(t *testing.T) {
	exports, err := lintTypes.Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the lint types product is %s", exports.Key()[:12])
}

// lintExports is lintTypes found once per test binary.
var lintExports = sync.OnceValues(lintTypes.Find)

// lintPackages is ./internal/lint/... loaded with its syntax and types, read with overlay in place of the
// files it names: cohere's own packages from source, and every other from lintTypes. The creating-getter
// and TypeReach guards and their probes all read it, and each loaded it for itself: six loads of the whole
// lint tree from source, about 21s of CPU in a package run, where three distinct inputs ever occur (the
// working tree, and one plant for each guard's probe) (#nxgt2ca). So each distinct overlay is loaded once
// per test binary and shared. The guards only read what they are handed.
//
// The packages it returns are the named roots, each with its syntax and type information, so the scans see
// the same calls they saw when every dependency was type-checked from source too: a dependency's types are
// the same read either way, and no scan reads a dependency's syntax.
func lintPackages(t *testing.T, overlay map[string][]byte) []*gotypes.Package {
	t.Helper()

	key := overlayKey(overlay)
	lintLoads.Lock()
	load := lintLoads.byOverlay[key]
	if load == nil {
		load = &lintLoad{}
		lintLoads.byOverlay[key] = load
	}
	lintLoads.Unlock()

	load.once.Do(func() { load.packages, load.failure = loadLintPackages(overlay) })
	if load.failure != "" {
		t.Fatal(load.failure)
	}
	return load.packages
}

var lintLoads = struct {
	sync.Mutex
	byOverlay map[string]*lintLoad
}{byOverlay: map[string]*lintLoad{}}

type lintLoad struct {
	once     sync.Once
	packages []*gotypes.Package

	// failure, when set, is what every caller fails with: one test cannot fail another, so the load
	// records it and each caller reports it.
	failure string
}

// overlayKey names an overlay by its paths and their contents.
func overlayKey(overlay map[string][]byte) string {
	paths := make([]string, 0, len(overlay))
	for path := range overlay {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		fmt.Fprintf(hash, "%d:%s:%d:", len(path), path, len(overlay[path]))
		hash.Write(overlay[path])
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func loadLintPackages(overlay map[string][]byte) ([]*gotypes.Package, string) {
	root, err := filepath.Abs("../..")
	if err != nil {
		return nil, err.Error()
	}
	exports, err := lintExports()
	if err != nil {
		return nil, err.Error()
	}
	loaded, err := gotypes.Load(gotypes.Config{Directory: root, Overlay: overlay, Exports: exports}, "./internal/lint/...")
	if err != nil {
		return nil, fmt.Sprintf("loading the rule packages: %v", err)
	}
	// Every package read from source, a dependency of the rule packages too: one that does not type-check
	// would hand the scans a partial tree.
	for _, pkg := range loaded.Source {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Sprintf("loading %s: %v", pkg.ImportPath, pkg.Errors[0])
		}
	}
	return loaded.Roots, ""
}
