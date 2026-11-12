package go_engine

import (
	"bytes"
	"encoding/gob"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// standardLibraryChildVariable marks a test binary a test started as a generator, or a generator started as its
// child, which TestMain runs as the verb its command line names rather than as the tests, under the engine tests'
// stamp, as the binary runs its own children under the go-engine verb.
const standardLibraryChildVariable = "COHERE_TEST_STANDARD_LIBRARY_CHILD"

// standardLibraryPackagesVariable carries testGenerator's probe packages, comma-separated, to a generator TestMain
// runs in a process of its own.
const standardLibraryPackagesVariable = "COHERE_TEST_STANDARD_LIBRARY_PACKAGES"

// generatedPackages are what a test's probe imports, the standard-library packages standardLibraryFixture imports, so
// a generated table holds every standard-library package a run over the fixture reaches, in a few seconds rather than
// the whole of the standard library's.
var generatedPackages = []string{"errors", "fmt", "log/slog", "strings", "unicode/utf8"}

func TestMain(suite *testing.M) {
	if os.Getenv(standardLibraryChildVariable) != "" {
		arguments := os.Args[1:]
		if !isStandardLibraryVerb(arguments) {
			os.Exit(refuse(os.Stderr, errors.New("a test binary started as a generator's child was given no verb "+
				"of one")))
		}
		os.Exit(runStandardLibraryVerb(arguments, os.Stdout, os.Stderr, rules.All(), nil, childGenerator()))
	}
	if os.Getenv(timingMarginChildVariable) != "" {
		os.Exit(runTimingMarginChild(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(suite.Run())
}

// childGenerator is testGenerator as a test binary TestMain runs as a generator rebuilds it from its environment, in
// which testGenerator's own environment is set: the same executable, stamp and probe packages, and children that keep
// the go flags and the proxy it was started with.
func childGenerator() standardLibraryGenerator {
	// Ignored: this process is the executable, which a test found with os.Executable to start it, and an empty name
	// fails the first child it starts, which says so.
	executable, _ := os.Executable()
	generator := standardLibraryGenerator{
		executable: executable,
		stamp:      engineTestsStamp,
		environment: []string{standardLibraryChildVariable + "=1", "GOPROXY=" + os.Getenv("GOPROXY"),
			"GOFLAGS=" + os.Getenv("GOFLAGS")},
	}
	if packages := os.Getenv(standardLibraryPackagesVariable); packages != "" {
		generator.packages = strings.Split(packages, ",")
	}
	return generator
}

// testGenerator is a generator whose children are this test binary, under the engine tests' stamp, whose probe
// imports generatedPackages, and which never reaches the network for a toolchain. Its children keep the go flags go
// env reports here, where a release's clear them, so the table it writes is keyed as the test's own runs are: a pool
// runs the tests with -buildvcs=false, which every standard-library key holds.
func testGenerator(test *testing.T) standardLibraryGenerator {
	test.Helper()
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatalf("finding the test binary: %v", executableError)
	}
	goFlags := goEnvironmentIn(test, copyFixture(test, standardLibraryFixture), nil)["GOFLAGS"]
	return standardLibraryGenerator{
		executable:  executable,
		stamp:       engineTestsStamp,
		environment: []string{standardLibraryChildVariable + "=1", "GOPROXY=off", "GOFLAGS=" + goFlags},
		packages:    generatedPackages,
	}
}

// localVariant is the variant the local toolchain computes for this machine, under its own `CGO_ENABLED`, unless
// cgoEnabled names another.
func localVariant(test *testing.T, cgoEnabled string) standardLibraryVariantRequest {
	test.Helper()
	values := goEnvironmentIn(test, copyFixture(test, standardLibraryFixture), nil)
	request := standardLibraryVariantRequest{goVersion: values["GOVERSION"], target: runtime.GOOS + "/" +
		runtime.GOARCH, cgoEnabled: values["CGO_ENABLED"]}
	if cgoEnabled != "" {
		request.cgoEnabled = cgoEnabled
	}
	return request
}

// generatedTable is the table testGenerator writes of request through the verb, run as a release runs it: the
// generator in a process of its own, which starts a variant child, then the canonical child. So two calls are two
// generator processes, as two releases are, sharing nothing but the test binary.
func generatedTable(test *testing.T, request standardLibraryVariantRequest) []byte {
	test.Helper()
	output := filepath.Join(test.TempDir(), "table")
	generator := testGenerator(test)
	command := exec.Command(generator.executable, standardLibraryTableVerb, "--out", output, "--target",
		request.target, "--versions", request.goVersion, "--cgo", request.cgoEnabled)
	command.Env = append(append(os.Environ(), generator.environment...),
		standardLibraryPackagesVariable+"="+strings.Join(generator.packages, ","))
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	command.Stdout = &standardOutput
	command.Stderr = &standardError
	if runError := command.Run(); runError != nil {
		test.Fatalf("generating %s: %v\n%s", variantLabel(request), runError, standardError.String())
	}
	table, readError := os.ReadFile(output)
	if readError != nil {
		test.Fatalf("reading the table: %v", readError)
	}
	if !strings.Contains(standardOutput.String(), "table: 1 variants") {
		test.Errorf("want the table's line, got\n%s", standardOutput.String())
	}
	return table
}

// sharedTable is the table of the local variant, generated once for every test that only reads it.
var sharedTable = struct {
	once  sync.Once
	table []byte
}{}

// localTable is sharedTable, generated on its first use.
func localTable(test *testing.T) []byte {
	test.Helper()
	sharedTable.once.Do(func() { sharedTable.table = generatedTable(test, localVariant(test, "")) })
	if sharedTable.table == nil {
		test.Fatal("the shared table was not generated, which the test that first asked for it says why")
	}
	return sharedTable.table
}

// A table the generator writes, as the release would, restores every standard-library package a run over the
// fixture reaches, and the run's records are the plain run's: the table says it holds the local variant, with as many
// packages as the run restores.
func TestAGeneratedTableIsReadByARun(test *testing.T) {
	test.Parallel()

	table := localTable(test)
	variants := cache.NewStandardLibraryTable(table).Variants()
	request := localVariant(test, "")
	if len(variants) != 1 || variants[0].GoVersion != request.goVersion || variants[0].Target != request.target ||
		variants[0].CgoEnabled != request.cgoEnabled || variants[0].Packages == 0 {
		test.Fatalf("want the table to hold %s alone, got %+v", variantLabel(request), variants)
	}
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache")
	read := runEngineWith(test, root, readingTable(table, engineTestsStamp), rules.All(), "--no-cache")
	if read.counts.FromBinary != variants[0].Packages {
		test.Fatalf("want all %d packages from the generated table, got %+v", variants[0].Packages, read.counts)
	}
	if read.withoutCounts != plain.withoutCounts {
		test.Fatalf("from the generated table\n%s\nplain\n%s", read.withoutCounts, plain.withoutCounts)
	}
}

// A table the generator writes carries the persisted reads of the packages it holds, and a host that restores the
// standard library from it keeps those packages for its extra builds as a host that checked them does (#pcv6eff): over
// the fixture with a file only another target compiles, that build checks from source exactly the packages it checks
// when the host computed the standard library itself, and the run's findings are that run's. Without the keep, the
// build checks every standard-library package of its graph from source.
func TestATableHitIsKeptForTheExtraBuilds(test *testing.T) {
	test.Parallel()

	table := localTable(test)
	entries, _ := tableParts(test, table)
	withReads := 0
	for _, entry := range entries {
		if entry.Reads != nil {
			withReads++
		}
	}
	if withReads == 0 {
		test.Fatalf("none of the table's %d entries carries persisted reads", len(entries))
	}
	root := copyFixture(test, standardLibraryFixture)
	// A file of another target than this machine's, so the run has one extra build.
	other := map[bool]string{true: "freebsd", false: "linux"}[runtime.GOOS == "linux"]
	if writeError := os.WriteFile(filepath.Join(root, "library_"+other+".go"), []byte("package library\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	computed := runCachedEngine(test, root, "--no-cache", "--other-builds")
	fromTable := runEngineWith(test, root, readingTable(table, engineTestsStamp), rules.All(), "--no-cache",
		"--other-builds")
	if fromTable.counts.FromBinary == 0 {
		test.Fatalf("want the host to restore the standard library from the table, got %+v", fromTable.counts)
	}
	computedBuild, tableBuild := extraBuildRecords(test, computed.stream)[0], extraBuildRecords(test,
		fromTable.stream)[0]
	if computedBuild.PackagesLoaded == 0 {
		test.Fatalf("the extra build %s loaded nothing, so it tells nothing", computedBuild.Target)
	}
	if tableBuild.CheckedFromSource != computedBuild.CheckedFromSource {
		test.Errorf("%s checked %d of %d packages from source with the host's standard library from the table, "+
			"want %d of %d, as with it computed (%d of the table's %d entries carry reads)", tableBuild.Target,
			tableBuild.CheckedFromSource, tableBuild.PackagesLoaded, computedBuild.CheckedFromSource,
			computedBuild.PackagesLoaded, withReads, len(entries))
	}
	findings := func(stream string) []string {
		var lines []string
		for line := range strings.Lines(stream) {
			if strings.Contains(line, `"kind":"finding"`) {
				lines = append(lines, line)
			}
		}
		return lines
	}
	if computedFindings, tableFindings := findings(computed.stream), findings(fromTable.stream); !slices.Equal(
		tableFindings, computedFindings) {
		test.Errorf("the findings with the table\n%s\nwant those with the standard library computed\n%s",
			strings.Join(tableFindings, ""), strings.Join(computedFindings, ""))
	}
}

// Two generations of the same variant, each a generator process of its own with children of their own, write the
// same bytes: the canonical child writes every entry again in one order, its facts sorted and numbered alike.
func TestAGeneratedTableIsTheSameEveryTime(test *testing.T) {
	test.Parallel()

	request := localVariant(test, "")
	first := generatedTable(test, request)
	if second := generatedTable(test, request); !bytes.Equal(first, second) {
		test.Fatalf("want two generations byte for byte alike, got %d and %d bytes", len(first), len(second))
	}
}

// The generator sets `CGO_ENABLED` explicitly for the variant it computes: a table of the variant this machine does
// not default to restores everything in a run under that value and nothing in a run under the other, since every key
// holds it.
//
// Not parallel: it sets the process's own `CGO_ENABLED`, which every go command the engine runs reads.
func TestAGeneratedVariantIsKeyedByItsCgo(test *testing.T) {
	local := localVariant(test, "").cgoEnabled
	other := map[string]string{"1": "0", "0": "1"}[local]
	table := generatedTable(test, localVariant(test, other))
	packages := cache.NewStandardLibraryTable(table).Variants()[0].Packages
	for _, reading := range []struct {
		cgoEnabled string
		fromBinary int
	}{{cgoEnabled: other, fromBinary: packages}, {cgoEnabled: local}} {
		test.Setenv("CGO_ENABLED", reading.cgoEnabled)
		root := copyFixture(test, standardLibraryFixture)
		read := runEngineWith(test, root, readingTable(table, engineTestsStamp), rules.All(), "--no-cache")
		if read.counts.FromBinary != reading.fromBinary {
			test.Errorf("CGO_ENABLED=%s: want %d from the table of cgo %s, got %+v", reading.cgoEnabled,
				reading.fromBinary, other, read.counts)
		}
	}
}

// tableEntry is the table's entry as gob reads it, field for field by name, so a test can change what an entry
// holds and write it back whole.
type tableEntry struct {
	PackagePath string
	Version     string
	Analyzers   []tableAnalysis
	Imports     []string
	Withheld    load.PackageWithheld
	Output      []byte
}

// tableAnalysis is one analyzer's part of a tableEntry.
type tableAnalysis struct {
	Slot     string
	Analysis plantedAnalysis
}

// tableParts are a table's entries and variants, to change and write again.
func tableParts(
	test *testing.T,
	table []byte,
) (map[cache.Key]cache.StandardLibraryEntry, []cache.StandardLibraryVariant) {
	test.Helper()
	read := cache.NewStandardLibraryTable(table)
	entries := map[cache.Key]cache.StandardLibraryEntry{}
	for _, key := range read.Keys() {
		entries[key], _ = read.Lookup(key)
	}
	if read.Problem() != nil || len(entries) == 0 {
		test.Fatalf("want a table holding entries, got %d (%v)", len(entries), read.Problem())
	}
	return entries, read.Variants()
}

// verifiedTable is the exit, stdout and stderr of the check over table, recomputed by testGenerator.
func verifiedTable(test *testing.T, table []byte) (int, string, string) {
	test.Helper()
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	exitCode := verifyStandardLibrary(nil, &standardOutput, &standardError, cache.NewStandardLibraryTable(table),
		testGenerator(test))
	return exitCode, standardOutput.String(), standardError.String()
}

// The check passes on the table the generator wrote, saying how many entries it checked, and fails on a table whose
// entry's output digest was changed, whose entry holds another package's export data, that lacks one entry, that holds
// an entry no variant computes, that holds nothing, that names a variant no toolchain here can recompute, that holds
// no variant for this machine's target, or whose entry holds another package's persisted reads.
func TestTheCheckRecomputesEveryEntryOfTheTable(test *testing.T) {
	test.Parallel()

	table := localTable(test)
	entries, variants := tableParts(test, table)
	exitCode, standardOutput, standardError := verifiedTable(test, table)
	if want := variantLabel(localVariant(test, "")) + ": " + strconv.Itoa(variants[0].Packages) +
		" checked, 0 differ"; exitCode != exitClean || !strings.Contains(standardOutput, want) {
		test.Fatalf("want the generated table to pass with %q, exit %d\n%s\n%s", want, exitCode, standardOutput,
			standardError)
	}
	// Two entries with export data and persisted reads, picked in key order so no case depends on map order: the
	// first, then the first after it whose export data and reads both differ from its own. Entries often share reads,
	// which the table holds once by digest, and a pair that shared them would swap nothing, so its case would pass the
	// check and fail here on about one run in 25 (#3eg0c9r).
	var firstKey cache.Key
	var secondKey cache.Key
	var firstFound bool
	var secondFound bool
	for _, key := range slices.SortedFunc(maps.Keys(entries), func(left cache.Key, right cache.Key) int {
		return bytes.Compare(left[:], right[:])
	}) {
		switch {
		case entries[key].ExportData == nil || entries[key].Reads == nil:
		case !firstFound:
			firstKey, firstFound = key, true
		case !bytes.Equal(entries[key].Reads, entries[firstKey].Reads) &&
			!bytes.Equal(entries[key].ExportData, entries[firstKey].ExportData):
			secondKey, secondFound = key, true
		}
		if secondFound {
			break
		}
	}
	if !secondFound {
		test.Fatalf("want two entries whose export data and persisted reads both differ, among %d", len(entries))
	}
	changed := func(change func(map[cache.Key]cache.StandardLibraryEntry, *[]cache.StandardLibraryVariant)) []byte {
		copied, copiedVariants := tableParts(test, table)
		change(copied, &copiedVariants)
		return cache.WriteStandardLibraryTable(copied, copiedVariants)
	}
	for name, failing := range map[string]struct {
		table []byte
		says  string
	}{
		"an output digest changed": {table: changed(func(copied map[cache.Key]cache.StandardLibraryEntry,
			_ *[]cache.StandardLibraryVariant) {
			var entry tableEntry
			if decodeError := gob.NewDecoder(bytes.NewReader(copied[firstKey].Entry)).Decode(
				&entry); decodeError != nil {
				test.Fatal(decodeError)
			}
			entry.Output[0] ^= 0xff
			var encoded bytes.Buffer
			if encodeError := gob.NewEncoder(&encoded).Encode(entry); encodeError != nil {
				test.Fatal(encodeError)
			}
			copied[firstKey] = cache.StandardLibraryEntry{Entry: encoded.Bytes(),
				ExportData: copied[firstKey].ExportData, Reads: copied[firstKey].Reads}
		}), says: "its output digest differs"},
		"another package's export data": {table: changed(func(copied map[cache.Key]cache.StandardLibraryEntry,
			_ *[]cache.StandardLibraryVariant) {
			copied[firstKey] = cache.StandardLibraryEntry{Entry: copied[firstKey].Entry,
				ExportData: copied[secondKey].ExportData, Reads: copied[firstKey].Reads}
		}), says: "1 differ"},
		"another package's persisted reads": {table: changed(func(copied map[cache.Key]cache.StandardLibraryEntry,
			_ *[]cache.StandardLibraryVariant) {
			copied[firstKey] = cache.StandardLibraryEntry{Entry: copied[firstKey].Entry,
				ExportData: copied[firstKey].ExportData, Reads: copied[secondKey].Reads}
		}), says: "its persisted reads differ"},
		"an entry missing": {table: changed(func(copied map[cache.Key]cache.StandardLibraryEntry,
			_ *[]cache.StandardLibraryVariant) {
			delete(copied, firstKey)
		}), says: "the table holds no entry under its key"},
		"an entry no variant computes": {table: changed(func(copied map[cache.Key]cache.StandardLibraryEntry,
			_ *[]cache.StandardLibraryVariant) {
			copied[cache.Key{0x01}] = copied[firstKey]
		}), says: "1 entries of the table no variant recomputed"},
		"nothing": {says: "carries none"},
		"a variant no toolchain here has": {table: changed(func(_ map[cache.Key]cache.StandardLibraryEntry,
			copiedVariants *[]cache.StandardLibraryVariant) {
			(*copiedVariants)[0].GoVersion = "go1.99.0"
		}), says: "not recomputed"},
		"no variant for this target": {table: changed(func(_ map[cache.Key]cache.StandardLibraryEntry,
			copiedVariants *[]cache.StandardLibraryVariant) {
			(*copiedVariants)[0].Target = "plan9/386"
		}), says: "holds no variant for " + runtime.GOOS + "/" + runtime.GOARCH},
	} {
		exitCode, standardOutput, standardError := verifiedTable(test, failing.table)
		if exitCode == exitClean || !strings.Contains(standardOutput+standardError, failing.says) {
			test.Errorf("%s: want the check to fail saying %q, exit %d\n%s\n%s", name, failing.says, exitCode,
				standardOutput, standardError)
		}
	}
}

// A binary with no release stamp refuses to generate a table, to compute a variant as a child, and to check a table,
// before any child starts or any file is written; and a command line that does not name what a table is of is refused
// as it is read.
func TestAnUnstampedBinaryRefusesToGenerate(test *testing.T) {
	test.Parallel()

	unstamped := testGenerator(test)
	unstamped.stamp = ""
	output := filepath.Join(test.TempDir(), "table")
	request := localVariant(test, "")
	generating := []string{"--out", output, "--target", request.target, "--versions", request.goVersion, "--cgo",
		request.cgoEnabled}
	for name, exitCode := range map[string]int{
		"generating": generateStandardLibraryTable(generating, &bytes.Buffer{}, &bytes.Buffer{}, unstamped),
		"a variant": runStandardLibraryVariant([]string{"--root", test.TempDir(), "--out", output}, &bytes.Buffer{},
			rules.All(), ""),
		"checking": verifyStandardLibrary(nil, &bytes.Buffer{}, &bytes.Buffer{},
			cache.NewStandardLibraryTable(localTable(test)), unstamped),
	} {
		if exitCode != exitRefused {
			test.Errorf("%s: want exit %d unstamped, got %d", name, exitRefused, exitCode)
		}
	}
	if _, statError := os.Stat(output); !os.IsNotExist(statError) {
		test.Errorf("want no table written unstamped, got %v", statError)
	}
	stamped := testGenerator(test)
	for name, arguments := range map[string][]string{
		"no --out":        generating[2:],
		"a bare target":   {"--out", output, "--target", "darwin", "--versions", "go1.27.0", "--cgo", "1"},
		"no Go version":   {"--out", output, "--target", "darwin/arm64", "--versions", "1.27", "--cgo", "1"},
		"cgo neither":     {"--out", output, "--target", "darwin/arm64", "--versions", "go1.27.0", "--cgo", "2"},
		"a variant twice": {"--out", output, "--target", "darwin/arm64", "--versions", "go1.27.0", "--cgo", "1,1"},
		"a positional":    append(slices.Clone(generating), "extra"),
		"a flag it lacks": append([]string{"--goos", "linux"}, generating...),
	} {
		if exitCode := generateStandardLibraryTable(arguments, &bytes.Buffer{}, &bytes.Buffer{},
			stamped); exitCode != exitRefused {
			test.Errorf("%s: want exit %d, got %d", name, exitRefused, exitCode)
		}
	}
	if exitCode := verifyStandardLibrary([]string{"--table"}, &bytes.Buffer{}, &bytes.Buffer{},
		cache.NewStandardLibraryTable(nil), stamped); exitCode != exitRefused {
		test.Errorf("checking with an argument: want exit %d, got %d", exitRefused, exitCode)
	}
}
