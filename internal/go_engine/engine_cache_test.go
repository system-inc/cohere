package go_engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// cacheFieldPattern is the lint record's cache counts, which are all a cached run may change in the stream once the
// elapsed times are normalized.
var cacheFieldPattern = regexp.MustCompile(`,"cache":\{[^}]*\}`)

// cachedRun is one engine run over root with extra after the contract and root, its stream normalized, its stream
// without the cache counts, and its lint record's cache counts.
type cachedRun struct {
	stream        string
	withoutCounts string
	counts        contract.LintCache
}

// runCachedEngine runs the engine over root, in process, and fails the test unless it exits with a summary, 0 or 1,
// says nothing on stderr and writes a lint record.
func runCachedEngine(test *testing.T, root string, extra ...string) cachedRun {
	test.Helper()
	return runEngineWith(test, root, embeddedStandardLibrary, rules.All(), extra...)
}

// runEngineWith is runCachedEngine with builtIn as the binary's standard-library entries and registered as the
// rules.
func runEngineWith(
	test *testing.T,
	root string,
	builtIn builtInStandardLibrary,
	registered []rule.Rule,
	extra ...string,
) cachedRun {
	test.Helper()
	return runEngineAnsweringInPart(test, root, builtIn, registered, nil, extra...)
}

// runEngineAnsweringInPart is runEngineWith writing the entries of the packages answersInPart names, by import path,
// with only the answers the run's questions computed in them (see check.Caching's AnswersInPart).
func runEngineAnsweringInPart(
	test *testing.T,
	root string,
	builtIn builtInStandardLibrary,
	registered []rule.Rule,
	answersInPart map[string]bool,
	extra ...string,
) cachedRun {
	test.Helper()
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-fix"}, extra...)
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	if exitCode := runOverlapping(arguments, nil, &standardOutput, &standardError, registered, nil, builtIn, true,
		answersInPart); exitCode != exitClean && exitCode != exitFindings {
		test.Fatalf("exit %d, want %d or %d; stderr:\n%s", exitCode, exitClean, exitFindings, standardError.String())
	}
	if standardError.Len() > 0 {
		test.Fatalf("want nothing on stderr, got:\n%s", standardError.String())
	}
	stream := elapsedPattern.ReplaceAllString(standardOutput.String(), `"elapsedMilliseconds":0`)
	ran := cachedRun{stream: stream, withoutCounts: cacheFieldPattern.ReplaceAllString(stream, "")}
	for line := range strings.Lines(stream) {
		if !strings.HasPrefix(line, `{"kind":"lint"`) {
			continue
		}
		var record struct {
			Cache contract.LintCache `json:"cache"`
		}
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("reading the lint record %s: %v", line, decodeError)
		}
		ran.counts = record.Cache
		return ran
	}
	test.Fatalf("no lint record in\n%s", stream)
	return ran
}

// entriesIn is each entry in root's store by its path in the store, with its modification time, so a run that wrote
// or rewrote one shows. The store keeps its entries in directories below its own.
func entriesIn(test *testing.T, root string) map[string]string {
	test.Helper()
	directory := filepath.Join(root, ".cache", "cohere", "go")
	entries := map[string]string{}
	walkError := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() {
			return nil
		}
		information, informationError := entry.Info()
		if informationError != nil {
			return informationError
		}
		entries[path] = information.ModTime().String()
		return nil
	})
	if walkError != nil && !errors.Is(walkError, fs.ErrNotExist) {
		test.Fatalf("listing %s: %v", directory, walkError)
	}
	return entries
}

// A warm run writes the cold run's stream but for the cache counts, restoring every package the cold run wrote and
// running nothing on them. After an edit to the project's one file only that package misses, and printf's finding in
// it, which needs the fact fmt.Sprintf has, comes back from fmt's restored facts. `--no-cache` reads nothing and
// writes nothing: it hits nothing, and leaves the store as it was.
func TestTheFindingsCacheChangesNoRecord(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "vet-finding"))
	cold := runCachedEngine(test, root)
	if !cold.counts.On || cold.counts.Hit != 0 || cold.counts.Written == 0 ||
		cold.counts.Written+cold.counts.Unstorable > cold.counts.Missed {
		test.Fatalf("want a cold run that hit nothing and wrote what it missed, got %+v", cold.counts)
	}
	// Each entry has its export data beside it, but unsafe's, whose types are the type checker's own, and an alias
	// under its second key, since every output below each package is known on a cold run, and go list's answer and
	// gofmt's verdicts are kept beside them.
	written := entriesIn(test, root)
	if len(written) != 3*cold.counts.Written+1 {
		test.Fatalf("want the %d entries written in the store with their export data and aliases, found %d files",
			cold.counts.Written, len(written))
	}

	warm := runCachedEngine(test, root)
	if warm.counts.Hit != cold.counts.Written || warm.counts.Missed != cold.counts.Missed-cold.counts.Written {
		test.Fatalf("want every package written cold hit warm and the rest missed, cold %+v, warm %+v", cold.counts,
			warm.counts)
	}
	if warm.withoutCounts != cold.withoutCounts {
		test.Fatalf("warm\n%s\ncold\n%s", warm.withoutCounts, cold.withoutCounts)
	}

	file := filepath.Join(root, "vet.go")
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatalf("reading %s: %v", file, readError)
	}
	if writeError := os.WriteFile(file, append(contents, []byte("\n// Edited, so its package misses.\n")...),
		0o644); writeError != nil {
		test.Fatalf("editing %s: %v", file, writeError)
	}
	edited := runCachedEngine(test, root)
	if edited.counts.Hit != cold.counts.Written-1 || edited.counts.Written != 1 {
		test.Fatalf("want only the edited package missed and written, cold %+v, edited %+v", cold.counts,
			edited.counts)
	}
	if !strings.Contains(edited.stream, `"rule":"cohere-go/printf"`) {
		test.Fatalf("want printf's finding from fmt's restored facts, got\n%s", edited.stream)
	}

	before := entriesIn(test, root)
	uncached := runCachedEngine(test, root, "--no-cache")
	if uncached.counts != (contract.LintCache{}) {
		test.Fatalf("want a run under --no-cache to count nothing, got %+v", uncached.counts)
	}
	if uncached.withoutCounts != edited.withoutCounts {
		test.Fatalf("under --no-cache\n%s\ncached\n%s", uncached.withoutCounts, edited.withoutCounts)
	}
	after := entriesIn(test, root)
	if fmt.Sprint(after) != fmt.Sprint(before) {
		test.Fatalf("want the store untouched under --no-cache, before %v, after %v", before, after)
	}

	fresh := copyFixture(test, filepath.Join("testdata", "vet-finding"))
	runCachedEngine(test, fresh, "--no-cache")
	if _, statError := os.Stat(filepath.Join(fresh, ".cache", "cohere", "go")); !os.IsNotExist(statError) {
		test.Fatalf("want no store made under --no-cache, got %v", statError)
	}
}

// A staticcheck.conf in a directory above a package is among the package's inputs, since staticcheck's config
// analyzer reads every one from the package's directory up: planting one that allows the fixture's dot import
// misses the package and drops ST1001's finding, and removing it again brings the finding back, each run writing
// what a run under `--no-cache` writes. Were the file outside the key, the run after planting it would restore the
// finding it no longer has.
func TestAStaticcheckConfAboveAPackageIsInItsKey(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "staticcheck-conf"))
	configuration := filepath.Join(root, "staticcheck.conf")
	for _, state := range []struct {
		name     string
		contents string
		reported bool
	}{
		{name: "no staticcheck.conf", reported: true},
		{name: "one allowing the dot import", contents: "dot_import_whitelist = [\"strings\"]\n"},
		{name: "it removed again", reported: true},
	} {
		if state.contents == "" {
			if removeError := os.Remove(configuration); removeError != nil && !os.IsNotExist(removeError) {
				test.Fatalf("%s: removing %s: %v", state.name, configuration, removeError)
			}
		} else if writeError := os.WriteFile(configuration, []byte(state.contents), 0o644); writeError != nil {
			test.Fatalf("%s: writing %s: %v", state.name, configuration, writeError)
		}
		cached := runCachedEngine(test, root)
		uncached := runCachedEngine(test, root, "--no-cache")
		if cached.withoutCounts != uncached.withoutCounts {
			test.Fatalf("%s: cached\n%s\nunder --no-cache\n%s", state.name, cached.withoutCounts,
				uncached.withoutCounts)
		}
		if reported := strings.Contains(cached.stream, `"rule":"cohere-go/ST1001"`); reported != state.reported {
			test.Fatalf("%s: want ST1001 reported %t, got %t in\n%s", state.name, state.reported, reported,
				cached.stream)
		}
	}
}

// A `//lint:` directive is in its package's key, since U1000 reads the package's directives through its own analyzer
// and the findings cache is keyed per package: adding `//lint:ignore U1000` above the fixture's one unused function
// misses the package and drops U1000's finding, and removing it again brings the finding back, each run writing what a
// run under `--no-cache` writes. Were the directive outside the key, the run after adding it would restore the finding
// it no longer has.
func TestALintDirectiveIsInItsPackagesKey(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "lint-directive"))
	file := filepath.Join(root, "unused", "unused.go")
	original, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatalf("reading %s: %v", file, readError)
	}
	function := []byte("func kept() {}")
	ignored := bytes.Replace(original, function,
		append([]byte("//lint:ignore U1000 kept to show the directive keys the package\n"), function...), 1)
	if bytes.Equal(ignored, original) {
		test.Fatalf("%s holds no %s to put the directive above", file, function)
	}
	for _, state := range []struct {
		name     string
		contents []byte
		reported bool
	}{
		{name: "no directive", contents: original, reported: true},
		{name: "a //lint:ignore U1000 above the function", contents: ignored},
		{name: "the directive removed again", contents: original, reported: true},
	} {
		if writeError := os.WriteFile(file, state.contents, 0o644); writeError != nil {
			test.Fatalf("%s: writing %s: %v", state.name, file, writeError)
		}
		cached := runCachedEngine(test, root)
		uncached := runCachedEngine(test, root, "--no-cache")
		if cached.withoutCounts != uncached.withoutCounts {
			test.Fatalf("%s: cached\n%s\nunder --no-cache\n%s", state.name, cached.withoutCounts,
				uncached.withoutCounts)
		}
		if reported := strings.Contains(cached.stream, `"rule":"cohere-go/U1000"`); reported != state.reported {
			test.Fatalf("%s: want U1000 reported %t, got %t in\n%s", state.name, state.reported, reported,
				cached.stream)
		}
	}
}

// goEnvironmentIn is go env's answer in root under environment as the findings cache reads it
// (load.GoEnvironment.Values), failing the test when go env fails.
func goEnvironmentIn(test *testing.T, root string, environment []string) map[string]string {
	test.Helper()
	goEnvironment, environmentError := load.ReadGoEnvironment(root, environment, nil)
	if environmentError != nil {
		test.Fatalf("reading the build context under %q: %v", environment, environmentError)
	}
	return goEnvironment.Values()
}

// The build context names the process's own `GOOS`, `GOARCH` and `BURNTSUSHI_TOML_110`, set or unset, apart from what
// go env reports, since asmdecl and framepointer read the target from the process and staticcheck's TOML parser reads
// the last: a run under another value keys every package apart.
//
// Not parallel: it sets the process's own `GOARCH` and `BURNTSUSHI_TOML_110`, which every test in the process reads.
func TestTheBuildContextNamesTheProcessVariablesAnAnalyzerReads(test *testing.T) {
	root, absoluteError := filepath.Abs(filepath.Join("testdata", "vet-finding"))
	if absoluteError != nil {
		test.Fatalf("resolving the fixture: %v", absoluteError)
	}
	test.Setenv("GOARCH", "arm64")
	test.Setenv("BURNTSUSHI_TOML_110", "1")
	set := cacheBuild(goEnvironmentIn(test, root, nil))
	for _, expected := range []string{"process GOARCH=arm64", "process BURNTSUSHI_TOML_110=1"} {
		if !slices.Contains(set, expected) {
			test.Errorf("want %q in %q", expected, set)
		}
	}
	if unsetError := os.Unsetenv("BURNTSUSHI_TOML_110"); unsetError != nil {
		test.Fatalf("unsetting BURNTSUSHI_TOML_110: %v", unsetError)
	}
	unset := cacheBuild(goEnvironmentIn(test, root, nil))
	if !slices.Contains(unset, "process BURNTSUSHI_TOML_110 unset") {
		test.Errorf("want BURNTSUSHI_TOML_110 named unset in %q", unset)
	}
}

// A go flag no verdict reads stays out of the build context, so two runs that differ only in it key every package
// alike: cohere-dev's pool sets -p to each token's share, and a run under another share missed everything. A flag a
// verdict can read, as -tags, still keys apart.
func TestAFlagNoVerdictReadsStaysOutOfTheBuildContext(test *testing.T) {
	test.Parallel()

	root, absoluteError := filepath.Abs(filepath.Join("testdata", "vet-finding"))
	if absoluteError != nil {
		test.Fatalf("resolving the fixture: %v", absoluteError)
	}
	contextUnder := func(goFlags string) []string {
		test.Helper()
		return cacheBuild(goEnvironmentIn(test, root, []string{"GOFLAGS=" + goFlags}))
	}
	threeThreads := contextUnder("-trimpath -p=3 -tags=probe -buildvcs=false -v")
	if !slices.Contains(threeThreads, "GOFLAGS=-trimpath -tags=probe -buildvcs=false") {
		test.Errorf("want GOFLAGS keyed as -trimpath -tags=probe -buildvcs=false, got %q", threeThreads)
	}
	sameShareOtherThreads := contextUnder("-trimpath --p=9 -tags=probe -buildvcs=false -x -work")
	if !slices.Equal(threeThreads, sameShareOtherThreads) {
		test.Errorf("want -p, -v, -x and -work left out, got %q and %q", threeThreads, sameShareOtherThreads)
	}
	// -buildvcs can decide whether go builds a main package at all, so it keys apart, as -n would.
	withoutStamping := contextUnder("-trimpath -p=3 -tags=probe")
	if slices.Equal(threeThreads, withoutStamping) {
		test.Errorf("want -buildvcs=false to key apart from no -buildvcs, got %q for both", threeThreads)
	}
	otherTags := contextUnder("-trimpath -p=3 -tags=other")
	if slices.Equal(threeThreads, otherTags) {
		test.Errorf("want -tags to key apart, got %q for both", threeThreads)
	}
}

// The standard library's build context is the target, cgo, the toolchain and its experiments, and the go flags but
// those no verdict reads and those that change nothing of the standard library: -trimpath and the module flags drop
// out, while -n, -buildvcs, -tags, -race and every other flag stay and key apart. It names none of the process's own
// variables, and go env's `GOROOT`, which the standard library's keys name each file below, is read beside it and is
// in neither context.
//
// Not parallel: it sets the process's own `GOARCH`, which every test in the process reads.
func TestTheStandardLibraryBuildContextNarrowsOnlyWhatTheStandardLibraryCannotSee(test *testing.T) {
	root, absoluteError := filepath.Abs(filepath.Join("testdata", "vet-finding"))
	if absoluteError != nil {
		test.Fatalf("resolving the fixture: %v", absoluteError)
	}
	test.Setenv("GOARCH", "arm64")
	contextUnder := func(goFlags string) []string {
		test.Helper()
		return standardLibraryBuild(goEnvironmentIn(test, root, []string{"GOFLAGS=" + goFlags,
			"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1", "GOEXPERIMENT="}))
	}
	keptFlags := "-n -buildvcs=false -tags=probe -race -gcflags=all=-N"
	narrowed := contextUnder("-trimpath -mod=mod -modfile=other.mod -modcacherw -p=3 -v " + keptFlags)
	values := goEnvironmentIn(test, root, nil)
	want := []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1", "GOFLAGS=" + keptFlags,
		"GOVERSION=" + values["GOVERSION"], "GOEXPERIMENT="}
	if !slices.Equal(narrowed, want) {
		test.Errorf("want\n%q\ngot\n%q", want, narrowed)
	}
	if bare := contextUnder(keptFlags); !slices.Equal(narrowed, bare) {
		test.Errorf("want -trimpath, the module flags, -p and -v left out, got %q and %q", narrowed, bare)
	}
	for _, kept := range strings.Fields(keptFlags) {
		without := strings.Join(slices.DeleteFunc(strings.Fields(keptFlags), func(field string) bool {
			return field == kept
		}), " ")
		if slices.Equal(narrowed, contextUnder(without)) {
			test.Errorf("want %s to key apart, got %q for both", kept, narrowed)
		}
	}
	if values["GOROOT"] == "" || !filepath.IsAbs(values["GOROOT"]) {
		test.Errorf("want go env's GOROOT read, absolute, got %q", values["GOROOT"])
	}
	for _, entry := range narrowed {
		if strings.HasPrefix(entry, "process ") {
			test.Errorf("want no process variable in the standard library's build context, got %q", entry)
		}
	}
	for _, entry := range slices.Concat(narrowed, cacheBuild(values)) {
		if strings.HasPrefix(entry, "GOROOT=") {
			test.Errorf("want GOROOT in neither build context, got %q", entry)
		}
	}
}

// The inputs record, which the front door's replay keys on, is byte for byte the same whether the load asked go list
// or kept its answer from an earlier run: the cached run that keeps it writes the record a --no-cache run writes.
func TestTheInputsRecordIsTheSameWithAKeptListing(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "vet-finding"))
	inputsLine := func(run cachedRun) string {
		for line := range strings.Lines(run.stream) {
			if strings.HasPrefix(line, `{"kind":"inputs"`) {
				return line
			}
		}
		test.Fatalf("no inputs record in\n%s", run.stream)
		return ""
	}
	asked := inputsLine(runCachedEngine(test, root))
	kept := inputsLine(runCachedEngine(test, root))
	fresh := inputsLine(runCachedEngine(test, root, "--no-cache"))
	if kept != fresh || asked != fresh {
		test.Fatalf("the inputs record\nwith a kept listing %s\nasking go list     %s", kept, fresh)
	}
}

// A run under `--no-fix` keeps gofmt's verdict that the long-lines fixture is formatted, and a run that writes after it
// still wraps the file's long lines: gofmt's verdict says nothing of the wrap, so the run that wraps reads past it,
// and only a verdict the wrap gave is trusted again. Trusting the first would leave every long line of a file gofmt
// once found clean a finding for as long as its bytes stay the same.
func TestACleanVerdictWithoutTheWrapStillWraps(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "long-lines"))
	file := filepath.Join(root, "long_lines.go")
	original, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatalf("reading %s: %v", file, readError)
	}
	checked := runCachedEngine(test, root)
	if !checked.counts.On {
		test.Fatalf("want the findings cache on, so gofmt's verdict is kept, got %+v", checked.counts)
	}
	unwrapped, readError := os.ReadFile(file)
	if readError != nil || !bytes.Equal(unwrapped, original) {
		test.Fatalf("the --no-fix run changed %s (%v)", file, readError)
	}

	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	run([]string{"--contract", fmt.Sprint(contract.Version), "--root", root}, &standardOutput, &standardError,
		rules.All(), nil)
	if !strings.Contains(standardOutput.String(), `"fixedBy":{"cohere-go/consistency-no-long-line":3}`) {
		test.Fatalf("want the run that writes to wrap three lines after gofmt's verdict was kept, got\n%s\n%s",
			standardOutput.String(), standardError.String())
	}
	wrapped, readError := os.ReadFile(file)
	if readError != nil || bytes.Equal(wrapped, original) {
		test.Fatalf("the run that writes left %s as it was (%v)", file, readError)
	}
}

// A renaming run leaves the findings cache as a hand edit of the same bytes would. Its lint runs without the cache,
// whose keys are of the bytes it replaced, so it reads no entry and writes none for the renamed tree. The next cached
// run then hits on every package whose key the rename left alone and misses on the packages it wrote and on every
// package whose key reaches them: a package's key holds the keys of the packages it imports (see cache.Keys), so the
// command and the external tests, which import the renamed package, miss with it, and the second key can't restore
// them either, since the renamed package's export data now spells ParseUrl. A copy edited by hand to the renamed bytes
// hits and misses exactly as many, and the renamed copy's findings are a --no-cache run's.
func TestARenamingRunLeavesTheCacheAsAnEditWould(test *testing.T) {
	test.Parallel()

	renamed := copyFixture(test, filepath.Join("testdata", "rename"))
	edited := copyFixture(test, filepath.Join("testdata", "rename"))
	runCachedEngine(test, renamed)
	runCachedEngine(test, edited)

	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	run([]string{"--contract", fmt.Sprint(contract.Version), "--root", renamed}, &standardOutput, &standardError,
		rules.All(), nil)
	var record contract.FixRecord
	for line := range strings.Lines(standardOutput.String()) {
		if strings.HasPrefix(line, `{"kind":"fix"`) {
			if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
				test.Fatalf("reading the fix record %s: %v", line, decodeError)
			}
		}
	}
	if record.FilesRewritten == 0 {
		test.Fatalf("want the run to rename, got\n%s\n%s", standardOutput.String(), standardError.String())
	}
	for _, changed := range record.ChangedFiles {
		contents, readError := os.ReadFile(changed.File)
		if readError != nil {
			test.Fatalf("reading %s: %v", changed.File, readError)
		}
		relative, relativeError := filepath.Rel(renamed, changed.File)
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		if writeError := os.WriteFile(filepath.Join(edited, relative), contents, 0o644); writeError != nil {
			test.Fatalf("editing %s by hand: %v", relative, writeError)
		}
	}

	next := runCachedEngine(test, renamed)
	byHand := runCachedEngine(test, edited)
	if next.counts.Hit == 0 || next.counts.Missed == 0 || next.counts.Hit != byHand.counts.Hit ||
		next.counts.Missed != byHand.counts.Missed {
		test.Fatalf("want the run after the rename to hit and miss as the run after the same edit by hand, after the "+
			"rename %+v, by hand %+v", next.counts, byHand.counts)
	}
	uncached := runCachedEngine(test, renamed, "--no-cache")
	if next.withoutCounts != uncached.withoutCounts {
		test.Fatalf("after the rename, cached\n%s\nunder --no-cache\n%s", next.withoutCounts, uncached.withoutCounts)
	}
}
