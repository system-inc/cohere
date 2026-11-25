//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling

// Package staticcheck_configuration is staticcheck's config analyzer as the engine runs it, for the ports that read a
// staticcheck.conf (ST1001 and ST1013), and the directory it starts from, which the findings cache keys a package on.
//
// It is upstream's (honnef.co/go/tools@v0.8.1 config/config.go, Analyzer, Dir and dirAST) but for which of a package's
// files it skips to find that directory. Upstream skips every file under os.UserCacheDir(), meant for the files Go's
// build cache holds, as the cgo tool's outputs, which the engine loads from the cache. That skipped every file of a
// project kept under the user's cache directory too, `~/.cache` on Linux and `~/Library/Caches` on darwin, so none of
// its staticcheck.conf files ever applied (#sc5qmy7). This skips only files under a build cache go env named, every
// `GOCACHE` the process's runs read (AddBuildCache), and never reads the user's cache directory.
//
// It is an analyzer of its own rather than upstream's with its Run replaced in place, so upstream's stays as upstream
// wrote it for staticcheck's originals, which the parity tests and cohere-parity run beside the ports as the oracle.
// The two differ on purpose, and only for a project under the user's cache directory, where the original reports what
// that project's staticcheck.conf allows. Load, DefaultConfig and the result type are upstream's, called as they are.
// The house's copied-code header does not fit: it declares a whole file a copy of one upstream file, byte for byte
// outside marked changes, and this carries a few lines of one beside code of its own.
package staticcheck_configuration

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/config"
)

// buildCaches is every build cache go env has named in this process, cleaned and absolute. It is every run's and never
// one run's: tests run many engines in one process, some under a `GOCACHE` of their own, and a file under any of them
// is a file go wrote to a build cache, never one a person keeps a project in, so a union is right for every run where
// a single value one run set would be another's at the moment a third read it.
var buildCaches = struct {
	mutex       sync.Mutex
	directories map[string]bool
}{directories: map[string]bool{}}

// AddBuildCache adds directory, a `GOCACHE` go env reported, to the build caches whose files Directory skips. Every
// go env the engine asks for a run's build adds its own (load.ReadGoEnvironment), before any analyzer runs.
func AddBuildCache(directory string) {
	// go env reports `GOCACHE` as "off" when the cache is turned off, and go refuses a relative one, so only an
	// absolute directory can hold the files a load reads.
	if !filepath.IsAbs(directory) {
		return
	}
	buildCaches.mutex.Lock()
	defer buildCaches.mutex.Unlock()
	buildCaches.directories[filepath.Clean(directory)] = true
}

// isInBuildCache reports whether file, an absolute name, is inside one of the build caches.
func isInBuildCache(file string) bool {
	buildCaches.mutex.Lock()
	defer buildCaches.mutex.Unlock()
	for directory := range buildCaches.directories {
		// Upstream's prefix test without its separator would take `/cache-other/a.go` for a file in `/cache`.
		if strings.HasPrefix(file, directory+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Directory is the directory a package's staticcheck.conf is read from, starting from files, the absolute names that
// make up one package: the directory of the first file outside every build cache, or empty when every file is in one,
// which gives the default configuration. Upstream's Dir, but for what it skips (see the package's comment).
func Directory(files []string) string {
	// From honnef.co/go/tools@v0.8.1 config/config.go, Dir, with the user's cache directory replaced by the build
	// caches.
	for _, file := range files {
		if isInBuildCache(file) {
			// A file in the build cache, as a cgo output, which lives nowhere near the package's staticcheck.conf.
			continue
		}
		return filepath.Dir(file)
	}
	// The package only consists of files in a build cache, or of none.
	return ""
}

// syntaxDirectory is Directory over files' names, each where its package clause is, `//line` directives followed, so a
// cgo output's file is named as the package file it was translated from. From honnef.co/go/tools@v0.8.1
// config/config.go, dirAST, unchanged but for its names and calling Directory.
func syntaxDirectory(files []*ast.File, fileSet *token.FileSet) string {
	names := make([]string, len(files))
	for index, file := range files {
		names[index] = fileSet.PositionFor(file.Pos(), true).Filename
	}
	return Directory(names)
}

// Analyzer is staticcheck's config analyzer as the engine runs it: the merged staticcheck.conf files from the
// package's directory up, as a *config.Config, read from the directory Directory finds.
var Analyzer = &analysis.Analyzer{
	Name:             "staticcheck_configuration",
	Doc:              "loads staticcheck.conf for the current package tree, from the first file outside Go's build cache",
	Run:              run,
	RunDespiteErrors: true,
	ResultType:       reflect.TypeFor[*config.Config](),
}

// run is upstream's config Analyzer's Run, from honnef.co/go/tools@v0.8.1 config/config.go, but for finding the
// directory through syntaxDirectory.
func run(pass *analysis.Pass) (any, error) {
	directory := syntaxDirectory(pass.Files, pass.Fset)
	if directory == "" {
		configuration := config.DefaultConfig
		return &configuration, nil
	}
	configuration, loadError := config.Load(directory)
	if loadError != nil {
		// Upstream's message, word for word.
		return nil, fmt.Errorf("error loading staticcheck.conf: %w", loadError)
	}
	return &configuration, nil
}
