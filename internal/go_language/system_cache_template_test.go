package go_language

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/rules"
)

// systemCacheTemplate is the standard library's system cache this test binary fills once, which every run of a test
// that names no system cache of its own starts from (#2htryg8): a run computing the standard library cold costs about
// 0.5 CPU-s and a warm one 0.19, and the package makes about 900 runs. Each run is given a directory of its own whose
// store holds a hard link to every one of the template's entries (seededSystemCache), so every run starts from the
// same system cache whatever ran before it or beside it, alone or in the whole package, and what one run writes no
// other run reads.
type systemCacheTemplate struct {
	// store is the template's store, `<root>/go/store` of the system cache the filling run wrote.
	store string
	// shards are the store's shard directories, by name, and files every entry in them, by its path under the store.
	shards []string
	files  []string
}

// templateRefusal is what templateFilled says as it refuses an environment other than TestMain's.
const templateRefusal = "the system cache's template is filled under TestMain's environment alone, and this test " +
	"changed it"

// templateFilledThoughRefused is what the refusal's child says when the template was filled all the same.
const templateFilledThoughRefused = "the template was filled under the environment templateFilled refused"

// templateRefusalChildVariable marks a test binary TestTheTemplateRefusesAChangedEnvironment started on itself as the
// child that changes the environment and asks for the template.
const templateRefusalChildVariable = "COHERE_TEST_TEMPLATE_REFUSAL_CHILD"

// templateTime is every template entry's modification time, far in the future, so no run marks a linked entry used:
// markUsed sets a file's times only when they are more than an hour old, and a link shares its times with the
// template and every other run's link, which a run must never move. A trim keeps a file from the future, as it keeps
// any file read in the last five days.
var templateTime = time.Date(2200, time.January, 1, 0, 0, 0, 0, time.UTC)

// templateModule is the module the template is filled from, by file name: it imports the standard-library packages
// the package's fixtures reach most, its test the `testing` package, and its file only windows compiles makes the run
// fill the windows build's context beside the host's. A package it does not reach, or another build's, a run computes
// cold, as it did before the template, and writes to its own directory alone.
var templateModule = map[string]string{
	"go.mod": "module example.com/template\n\ngo 1.24\n",
	"template.go": `// Package template reaches the standard library the fixtures reach.
package template

import (
	_ "bytes"
	_ "context"
	_ "encoding/json"
	_ "errors"
	_ "fmt"
	_ "io"
	_ "log/slog"
	_ "os"
	_ "os/exec"
	_ "reflect"
	_ "sort"
	_ "strings"
	_ "sync"
	_ "time"
	_ "unicode/utf8"
)
`,
	"template_test.go":    "package template\n\nimport \"testing\"\n\nfunc TestNothing(test *testing.T) {}\n",
	"template_windows.go": "package template\n",
}

// filledSystemCacheTemplate is the template, filled the first time a run needs it, under the system cache TestMain
// gave this test binary (testsSystemCache), apart from its store, so the directory TestMain removes takes it, and no
// run reading that system cache reads or trims it.
var filledSystemCacheTemplate = sync.OnceValues(fillSystemCacheTemplate)

// fillSystemCacheTemplate fills the template by one run under `--no-cache` over templateModule, with every rule and
// the executable's own identity, as a plain run keys the standard library, and makes each entry read-only, so a write
// in place through any run's link fails rather than reach every run, with its times templateTime.
func fillSystemCacheTemplate() (systemCacheTemplate, error) {
	base, resolveError := filepath.EvalSymlinks(testsSystemCache)
	if resolveError != nil {
		return systemCacheTemplate{}, resolveError
	}
	project := filepath.Join(base, "template", "project")
	root := filepath.Join(base, "template", "system-cache")
	if makeError := os.MkdirAll(project, 0o755); makeError != nil {
		return systemCacheTemplate{}, makeError
	}
	for name, contents := range templateModule {
		if writeError := os.WriteFile(filepath.Join(project, name), []byte(contents), 0o644); writeError != nil {
			return systemCacheTemplate{}, writeError
		}
	}
	// A repository of its own, as every fixture's copy is (copyFixture).
	if writeError := writeRepository(project); writeError != nil {
		return systemCacheTemplate{}, writeError
	}
	var standardError bytes.Buffer
	exitCode := runWithStandardLibrary(
		[]string{"--contract", fmt.Sprint(contract.Version), "--root", project, "--no-cache", "--no-fix"},
		io.Discard, &standardError, rules.All(), nil, standardLibrarySource{systemCacheRoot: root})
	if exitCode != exitClean && exitCode != exitFindings {
		return systemCacheTemplate{}, fmt.Errorf("the filling run exited %d: %s", exitCode, standardError.String())
	}
	template := systemCacheTemplate{store: systemStoreDirectory(root)}
	shards, readError := os.ReadDir(template.store)
	if readError != nil {
		return systemCacheTemplate{}, readError
	}
	for _, shard := range shards {
		// Only a shard's directory, two hex characters, holds entries: the packs beside them are each one project's,
		// which no other project's run reads.
		if decoded, decodeError := hex.DecodeString(shard.Name()); !shard.IsDir() || decodeError != nil ||
			len(decoded) != 1 {
			continue
		}
		template.shards = append(template.shards, shard.Name())
		entries, entriesError := os.ReadDir(filepath.Join(template.store, shard.Name()))
		if entriesError != nil {
			return systemCacheTemplate{}, entriesError
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			relative := filepath.Join(shard.Name(), entry.Name())
			path := filepath.Join(template.store, relative)
			if timesError := os.Chtimes(path, templateTime, templateTime); timesError != nil {
				return systemCacheTemplate{}, timesError
			}
			if modeError := os.Chmod(path, 0o444); modeError != nil {
				return systemCacheTemplate{}, modeError
			}
			template.files = append(template.files, relative)
		}
	}
	if len(template.files) == 0 {
		return systemCacheTemplate{}, fmt.Errorf("the filling run wrote no entry to %s: %s", template.store,
			standardError.String())
	}
	return template, nil
}

// templateFilled is the template (systemCacheTemplate), filled now if no run has filled it yet, failing test if the
// fill fails. A test that reads a count of the whole process around a run, such as cache.FilesHashed, or observes it,
// such as cache.ObserveReads, calls it before its first read: the fill is a run of its own, which moves those counts,
// and the first run to need the template fills it, inside the window of a test run alone (#2htryg8). It refuses an
// environment other than TestMain's (testsEnvironment), which the template's keys are made under.
func templateFilled(test *testing.T) systemCacheTemplate {
	test.Helper()
	if !slices.Equal(slices.Sorted(slices.Values(os.Environ())), testsEnvironment) {
		test.Fatal(templateRefusal)
	}
	template, templateError := filledSystemCacheTemplate()
	if templateError != nil {
		test.Fatalf("filling the system cache's template: %v", templateError)
	}
	return template
}

// A test that changed the environment and then asks for the template fails, refused, and never fills it under keys of
// an environment other than TestMain's (templateFilled). A test that must fail would fail this one, so the asker runs
// in a child, this test binary run on this test alone, as the timing margin's run is (TestMain): the child sets a
// variable and asks, and must exit failing with the refusal, having made no template: a refusal that only marked
// the test failed and went on to fill would pass the first half alone.
//
// Not parallel: its child sets a variable with test.Setenv, which a parallel test refuses; the parent calls Parallel
// first thing on its own branch.
func TestTheTemplateRefusesAChangedEnvironment(test *testing.T) {
	if os.Getenv(templateRefusalChildVariable) != "" {
		// The child, not parallel, since test.Setenv refuses a parallel test.
		test.Setenv(templateRefusalChildVariable, "changed")
		// Deferred, since the refusal ends this goroutine: the fill alone makes the template's directory.
		defer func() {
			if _, statError := os.Stat(filepath.Join(testsSystemCache, "template")); statError == nil {
				test.Error(templateFilledThoughRefused)
			}
		}()
		templateFilled(test)
		return
	}
	test.Parallel()
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatalf("finding the test binary: %v", executableError)
	}
	command := exec.Command(executable, "-test.run=^TestTheTemplateRefusesAChangedEnvironment$", "-test.count=1")
	command.Env = append(os.Environ(), templateRefusalChildVariable+"=1")
	output, runError := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(runError, &exitError) || !strings.Contains(string(output), templateRefusal) ||
		strings.Contains(string(output), templateFilledThoughRefused) {
		test.Errorf("want the child to fail with %q and fill nothing, got %v:\n%s", templateRefusal, runError,
			output)
	}
}

// seededSystemCache is a new system cache root of the run's own whose store holds a hard link to every entry of the
// template (systemCacheTemplate). The store never writes a file in place: a write renames a whole file over the
// link's name, and a removal, a damaged entry's or a trim's, removes the link alone, so nothing a run does reaches
// the template or another run's directory. It is empty for a test of a cold system cache (coldSystemCacheFor), and
// under an environment other than TestMain's (testsEnvironment), as a test that sets `GOROOT`, `GOFLAGS` or
// `CGO_ENABLED` has: the template is filled, and its keys made, under TestMain's alone, and a test that changes what
// the go commands answer runs cold, as before the template.
func seededSystemCache(test *testing.T) string {
	test.Helper()
	if _, cold := coldTests.Load(test); cold ||
		!slices.Equal(slices.Sorted(slices.Values(os.Environ())), testsEnvironment) {
		return test.TempDir()
	}
	template := templateFilled(test)
	root := test.TempDir()
	store := systemStoreDirectory(root)
	for _, shard := range template.shards {
		if makeError := os.MkdirAll(filepath.Join(store, shard), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
	}
	for _, relative := range template.files {
		linkError := os.Link(filepath.Join(template.store, relative), filepath.Join(store, relative))
		if linkError != nil {
			test.Fatal(linkError)
		}
	}
	return root
}

// A run seeded from the template restores the standard library from it and leaves it as it was: over the standard
// library fixture, a plain run under `--no-cache` restores packages from the system cache, and every template entry
// keeps its time and its read-only mode, so no run moves another's file times or writes through a link.
func TestASeededRunRestoresFromTheTemplateAndLeavesItAsItWas(test *testing.T) {
	test.Parallel()

	seeded := runCached(test, copyFixture(test, standardLibraryFixture), "--no-cache")
	if seeded.counts.FromSystemCache == 0 {
		test.Errorf("want the seeded run to restore the standard library from the template, got %+v", seeded.counts)
	}
	template := templateFilled(test)
	for _, relative := range template.files {
		information, statError := os.Stat(filepath.Join(template.store, relative))
		if statError != nil {
			test.Fatal(statError)
		}
		if !information.ModTime().Equal(templateTime) || information.Mode().Perm() != 0o444 {
			test.Errorf("%s is %s at %s, want %s at %s", relative, information.Mode().Perm(), information.ModTime(),
				os.FileMode(0o444), templateTime)
		}
	}
}
