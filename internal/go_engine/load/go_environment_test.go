package load

import (
	"go/build"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/staticcheck_configuration"
)

// A load handed go env's answer for its own workspace and environment reads it and asks go env nothing, and a load of
// another workspace or under another environment asks for itself. The answer carries a go env file planted only in it,
// which the inputs record names as absent, so the record says whose answer the load read (#f53yra8).
func TestALoadReadsTheGoEnvironmentItIsHanded(test *testing.T) {
	test.Parallel()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	writeListingFiles(test, root, listingFiles)
	project := filepath.Join(root, "project")
	other := filepath.Join(root, "outside")
	environment := []string{"GOWORK=off"}
	planted := filepath.Join(root, "planted-go.env")

	answer, answerError := ReadGoEnvironment(project, environment, nil)
	if answerError != nil {
		test.Fatal(answerError)
	}
	answer.reported.environmentFile = planted
	readsPlanted := func(directory string, loadEnvironment []string) bool {
		test.Helper()
		loaded, loadError := PackagesCached(directory, []string{"./..."}, loadEnvironment, nil,
			Caching{GoEnvironment: answer})
		if loadError != nil {
			test.Fatal(loadError)
		}
		return slices.Contains(loaded.Inputs.Absent, planted)
	}
	if !readsPlanted(project, environment) {
		test.Error("a load handed go env's answer for its own workspace and environment asked go env again")
	}
	if readsPlanted(project, append(slices.Clone(environment), "CGO_ENABLED=0")) {
		test.Error("a load under another environment read an answer given for this one")
	}
	if readsPlanted(other, environment) {
		test.Error("a load of another workspace read an answer given for this one")
	}
	if _, statError := os.Stat(planted); statError == nil {
		test.Fatal("the planted go env file exists, so its absence proves nothing")
	}
}

// Two loads that take one answer each read their own held bytes: the answer's build context is copied for each
// asker, never handed out shared, so the reader one load set is never the one another reads through, which a run whose
// loads held different bytes would otherwise mix (#f53yra8).
func TestEachLoadOfOneAnswerReadsItsOwnHeldBytes(test *testing.T) {
	test.Parallel()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	writeListingFiles(test, root, listingFiles)
	project := filepath.Join(root, "project")
	answer, answerError := ReadGoEnvironment(project, []string{"GOWORK=off"}, nil)
	if answerError != nil {
		test.Fatal(answerError)
	}
	file := filepath.Join(project, "base", "base.go")
	heldAs := func(contents string) Held {
		return func(path string) ([]byte, bool) {
			if path == file {
				return []byte(contents), true
			}
			return nil, false
		}
	}
	first := answer.contextFor(heldAs("package first\n"))
	second := answer.contextFor(heldAs("package second\n"))
	readThrough := func(buildContext *build.Context) string {
		test.Helper()
		reader, openError := buildContext.OpenFile(file)
		if openError != nil {
			test.Fatal(openError)
		}
		defer func() {
			if closeError := reader.Close(); closeError != nil {
				test.Error(closeError)
			}
		}()
		contents, readError := io.ReadAll(reader)
		if readError != nil {
			test.Fatal(readError)
		}
		return string(contents)
	}
	if got := readThrough(first); got != "package first\n" {
		test.Errorf("the first load read %q through its own context, want its own held bytes", got)
	}
	if got := readThrough(second); got != "package second\n" {
		test.Errorf("the second load read %q through its own context, want its own held bytes", got)
	}
}

// The build cache go env names for a run is the one whose files staticcheck's config analyzer skips to find the
// directory a package's staticcheck.conf is read from (#sc5qmy7): a cgo output there, which lives nowhere near the
// package, is passed over for the package's own file, and a package of build-cache files alone reads no
// staticcheck.conf. The cache is a fresh directory no other go env in the process names, so only this run's answer can
// have made it one.
func TestTheRunsBuildCacheIsSkippedForTheStaticcheckConf(test *testing.T) {
	test.Parallel()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	writeListingFiles(test, root, listingFiles)
	project := filepath.Join(root, "project")
	buildCache := filepath.Join(root, "build-cache")
	goEnvironmentOf(test, project, []string{"GOWORK=off", "GOCACHE=" + buildCache})

	// Named as go names a cgo output it writes to the build cache, and never written: the directory is all it reads.
	cgoOutput := filepath.Join(buildCache, "4f", "4f00c0ffee-d")
	packageFile := filepath.Join(project, "base", "base.go")
	directory := staticcheck_configuration.Directory([]string{cgoOutput, packageFile})
	if directory != filepath.Dir(packageFile) {
		test.Errorf("a package of a cgo output and %s reads its staticcheck.conf from %q, want the package's directory",
			packageFile, directory)
	}
	if alone := staticcheck_configuration.Directory([]string{cgoOutput}); alone != "" {
		test.Errorf("a package of a cgo output alone reads its staticcheck.conf from %q, want none", alone)
	}
}

// goEnvironmentOf is go env's answer in directory under the current environment plus environment, failing the test
// when go env fails.
func goEnvironmentOf(test *testing.T, directory string, environment []string) *GoEnvironment {
	test.Helper()
	goEnvironment, environmentError := ReadGoEnvironment(directory, environment, nil)
	if environmentError != nil {
		test.Fatal(environmentError)
	}
	return goEnvironment
}
