package go_engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_else_return"
)

// separatorPackage is the other-builds fixture's package with a file only windows builds, and labelPackage the one
// with a file only a build with the cohere_extra tag builds.
const (
	separatorPackage = "example.com/builds/internal/separator"
	labelPackage     = "example.com/builds/internal/label"
)

// runOver runs the engine over root with the registry's rules and the findings cache off, so two runs over copies of
// one fixture read alike, with extra after the root, and returns the pipeline it ran and its stream, normalized. The
// run must exit wantExit.
func runOver(test *testing.T, root string, wantExit int, extra ...string) (*pipeline, string) {
	test.Helper()
	return runOverWith(test, root, wantExit, append([]string{"--no-cache"}, extra...)...)
}

// runOverWith is runOver with the findings cache as extra leaves it, on and in root's own store by default.
func runOverWith(test *testing.T, root string, wantExit int, extra ...string) (*pipeline, string) {
	test.Helper()
	engine, stream := runOverRaw(test, root, wantExit, extra...)
	return engine, normalize(stream, root)
}

// runOverRaw is runOverWith's run with its stream as written, for a test that reads what normalize hides, such as
// the inputs outside the root.
func runOverRaw(test *testing.T, root string, wantExit int, extra ...string) (*pipeline, string) {
	test.Helper()
	return runOverConfigured(test, root, wantExit, nil, extra...)
}

// runOverConfigured is runOverRaw with configure, when not nil, given the pipeline before it runs, as a test sets what
// no switch sets.
func runOverConfigured(
	test *testing.T,
	root string,
	wantExit int,
	configure func(*pipeline),
	extra ...string,
) (*pipeline, string) {
	test.Helper()
	engine, stream, _ := runOverNoted(test, root, wantExit, configure, extra...)
	return engine, stream
}

// runOverNoted is runOverConfigured with what the run noted on stderr beside its stream, both as written.
func runOverNoted(
	test *testing.T,
	root string,
	wantExit int,
	configure func(*pipeline),
	extra ...string,
) (*pipeline, string, string) {
	test.Helper()
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root}, extra...)
	request, parseError := parseInvocation(arguments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	registered := rules.All()
	ruleSettings, settingsError := loadSettings(request, registered)
	if settingsError != nil {
		test.Fatal(settingsError)
	}
	if optionsError := configureRules(ruleSettings, registered); optionsError != nil {
		test.Fatal(optionsError)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	engine := newPipeline(request, described, ruleSettings, registered, &standardOutput, &standardError, nil)
	if configure != nil {
		configure(engine)
	}
	if exitCode := engine.run(); exitCode != wantExit {
		test.Fatalf("the run exited %d, want %d:\n%s\n%s", exitCode, wantExit, standardOutput.String(),
			standardError.String())
	}
	return engine, standardOutput.String(), standardError.String()
}

// Under `--other-builds` a module with a file only windows builds and one only the cohere_extra tag builds is
// checked under three builds, the host's first, and mergeBuildRuns is handed a run for each: every extra build
// analyzes every package its load holds, as the host's does, and has a say on every file it compiles, and every run
// carries the inputs its load read. Only windows's run finds the else after a return planted in its windows file.
// Without the switch nothing is handed over and the run is clean, as it was.
func TestOtherBuildsHandTheMergeARunPerBuild(test *testing.T) {
	test.Parallel()
	off, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "other-builds")), exitClean)
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	on, _ := runOver(test, root, exitFindings, "--other-builds")
	if off.buildRuns != nil {
		test.Errorf("without the switch the merge was handed %d runs, want none", len(off.buildRuns))
	}

	if len(on.buildRuns) != 3 {
		test.Fatalf("the merge was handed %d runs, want 3", len(on.buildRuns))
	}
	host := on.buildRuns[0]
	if slices.Contains(host.build.Tags, "cohere_extra") {
		test.Errorf("the host's build %s sets the fixture's tag", host.build)
	}
	if host.result == nil || len(host.said) == 0 {
		test.Errorf("the host's run has no result (%t) or says nothing (%d files)", host.result == nil,
			len(host.said))
	}
	windowsFile := filepath.Join(root, "internal", "separator", "separator_windows.go")
	taggedFile := filepath.Join(root, "internal", "label", "prefix_extra.go")
	if host.compiled[windowsFile] || host.compiled[taggedFile] {
		test.Errorf("the host compiled a file only another build compiles")
	}

	// The extra builds are in the order BuildsFor prefers them, which puts the host's own `GOOS` first, so the tagged
	// build comes before windows on any host but windows, and each is found by what it builds.
	windowsAt := slices.IndexFunc(
		on.buildRuns,
		func(run buildRun) bool { return run.build.OperatingSystem == "windows" },
	)
	taggedAt := slices.IndexFunc(on.buildRuns, func(run buildRun) bool {
		return slices.Contains(run.build.Tags, "cohere_extra")
	})
	if windowsAt < 1 || taggedAt < 1 || windowsAt == taggedAt {
		test.Fatalf("the extra builds are %s and %s, want one for windows and one with the cohere_extra tag",
			on.buildRuns[1].build, on.buildRuns[2].build)
	}
	windows, tagged := on.buildRuns[windowsAt], on.buildRuns[taggedAt]
	// Every build of the fixture loads the same three packages, so an extra build that analyzed every package its load
	// holds analyzed what the host's did.
	for _, extra := range on.buildRuns[1:] {
		if extra.result == nil || len(extra.typeErrors) > 0 {
			test.Errorf("%s ran no rules (%t) or found type errors: %v", extra.build, extra.result == nil,
				extra.typeErrors)
		}
		if !maps.Equal(extra.analyzed, host.analyzed) || !extra.analyzed[separatorPackage] ||
			!extra.analyzed[labelPackage] {
			test.Errorf("%s analyzed %s, want every package its load holds, %s", extra.build,
				sortedKeys(extra.analyzed), sortedKeys(host.analyzed))
		}
		for file := range extra.compiled {
			if !extra.said[file] {
				test.Errorf("%s compiles %s and has no say on it", extra.build, file)
			}
		}
	}
	if !windows.said[windowsFile] || windows.said[taggedFile] {
		test.Errorf("windows says on %s, want %s and not %s", sortedKeys(windows.said), windowsFile, taggedFile)
	}
	if !tagged.said[taggedFile] || tagged.said[windowsFile] {
		test.Errorf("%s says on %s, want %s and not %s", tagged.build, sortedKeys(tagged.said), taggedFile,
			windowsFile)
	}
	for _, run := range on.buildRuns {
		if len(run.inputs.Files) == 0 || len(run.inputs.Directories) == 0 {
			test.Errorf("%s's run carries no inputs: %+v", run.build, run.inputs)
		}
	}
	if !slices.Contains(windows.inputs.Files, windowsFile) {
		test.Errorf("windows's inputs %v leave out %s, which it compiles", windows.inputs.Files, windowsFile)
	}

	// The windows file's else after a return is found by windows's run and by no other, since no other build
	// compiles that file.
	elseReturn := consistency_no_else_return.Name
	foundIn := func(run buildRun) bool {
		return slices.ContainsFunc(run.result.Findings, func(finding check.Finding) bool {
			return finding.Rule == elseReturn && finding.File == windowsFile
		})
	}
	if !foundIn(windows) {
		test.Errorf("windows's run found %v, want %s in %s", windows.result.Findings, elseReturn, windowsFile)
	}
	if foundIn(host) || foundIn(tagged) {
		test.Errorf("a build that never compiles %s found %s in it", windowsFile, elseReturn)
	}
}

// The project record is written before any build runs, so under `--other-builds` describeProject reads the builds
// and names each file the host's build constraints leave out and an extra build compiles as checked by that build, in
// the build's own words, which the front door counts as checked (#72n9mq0): the windows file by windows's build, the
// tagged file by the cohere_extra one, each the build the lint phase ran. A file no build compiles, one whose
// constraint names `ignore`, stays a gap for the selection's reason, that no build sets `ignore` (#ntamp9e), and adds
// no build. Without the switch every one of the three keeps the constraint's reason.
func TestOtherBuildsNameTheBuildThatChecksAConstrainedFile(test *testing.T) {
	test.Parallel()
	const constraintPrefix = "excluded by build constraints on "
	windowsFile := filepath.Join("internal", "separator", "separator_windows.go")
	taggedFile := filepath.Join("internal", "label", "prefix_extra.go")
	ignoredFile := filepath.Join("internal", "label", "generate.go")
	// copyWithIgnored is a copy of the fixture with a file whose constraint names `ignore`, which no build sets.
	copyWithIgnored := func() string {
		root := copyFixture(test, filepath.Join("testdata", "other-builds"))
		ignored := []byte("//go:build ignore\n\npackage label\n")
		if writeError := os.WriteFile(filepath.Join(root, ignoredFile), ignored, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		return root
	}
	// reasons is the project record's reason for each file it left out, by its path below root.
	reasons := func(engine *pipeline, root string) map[string]string {
		byFile := map[string]string{}
		for _, excluded := range engine.described.record.Excluded {
			relative, relativeError := filepath.Rel(root, excluded.File)
			if relativeError != nil {
				test.Fatal(relativeError)
			}
			byFile[relative] = excluded.Reason
		}
		return byFile
	}

	offRoot := copyWithIgnored()
	off, _ := runOver(test, offRoot, exitClean)
	offReasons := reasons(off, offRoot)
	for _, file := range []string{windowsFile, taggedFile, ignoredFile} {
		if !strings.HasPrefix(offReasons[file], constraintPrefix) {
			test.Errorf("without the switch %s is left out for %q, want the constraint's reason", file,
				offReasons[file])
		}
	}

	onRoot := copyWithIgnored()
	on, _ := runOver(test, onRoot, exitFindings, "--other-builds")
	if len(on.buildRuns) != 3 {
		test.Fatalf("the merge was handed %d runs, want 3: the file no build compiles adds none", len(on.buildRuns))
	}
	onReasons := reasons(on, onRoot)
	for file, isItsBuild := range map[string]func(build project.Build) bool{
		windowsFile: func(build project.Build) bool { return build.OperatingSystem == "windows" },
		taggedFile:  func(build project.Build) bool { return slices.Contains(build.Tags, "cohere_extra") },
	} {
		at := slices.IndexFunc(on.buildRuns[1:], func(run buildRun) bool { return isItsBuild(run.build) })
		if at < 0 {
			test.Fatalf("no extra build compiles %s", file)
		}
		want := project.CheckedByBuild(on.buildRuns[1+at].build)
		if onReasons[file] != want {
			test.Errorf("under the switch %s is left out for %q, want %q", file, onReasons[file], want)
		}
	}
	// Why no build compiles it, from the selection, which says more than the host's constraint (#ntamp9e).
	if want := "its constraint names `ignore`, which no build sets"; onReasons[ignoredFile] != want {
		test.Errorf("under the switch %s, which no build compiles, is left out for %q, want %q", ignoredFile,
			onReasons[ignoredFile], want)
	}
}

// With the findings cache on, each extra build keeps its own entries, keyed on its own target, cgo and flags: a first
// run over the fixture hits nothing another build wrote and writes an entry for every package of every build, and a
// second over the same tree restores every package of every build from them, writing nothing, as the host's build
// does. Its verdict is the first run's. The extra builds restore nothing from the host's run here: a package a build
// restores from it gets no entry in the build's own store, which restoring a warm build from is the next unit's
// (#b2t9xmf).
func TestOtherBuildsHitTheirOwnCacheWarm(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	restoringNothing := func(engine *pipeline) { engine.keepsHostRestoreOff = true }
	cold, coldRaw := runOverConfigured(test, root, exitFindings, restoringNothing, "--other-builds")
	warm, warmRaw := runOverConfigured(test, root, exitFindings, restoringNothing, "--other-builds")
	coldStream, warmStream := normalize(coldRaw, root), normalize(warmRaw, root)
	if len(cold.buildRuns) != 3 || len(warm.buildRuns) != 3 {
		test.Fatalf("the runs handed the merge %d and %d runs, want 3 each", len(cold.buildRuns), len(warm.buildRuns))
	}
	for index, warmRun := range warm.buildRuns {
		coldCounts, warmCounts := cold.buildRuns[index].result.Cache, warmRun.result.Cache
		// The first run's builds run one after another into one store, so a build whose keys met another's would hit
		// what that build wrote.
		if !coldCounts.On || coldCounts.Hit != 0 || coldCounts.Missed == 0 || coldCounts.Written != coldCounts.Missed {
			test.Errorf("%s's first run hit %d and wrote %d of %d misses (on %t), want none hit and every one written",
				warmRun.build, coldCounts.Hit, coldCounts.Written, coldCounts.Missed, coldCounts.On)
		}
		if warmCounts.Hit != coldCounts.Hit+coldCounts.Missed || warmCounts.Missed != 0 || warmCounts.Written != 0 {
			test.Errorf("%s's second run hit %d, missed %d and wrote %d, want every one of %d hit and none written",
				warmRun.build, warmCounts.Hit, warmCounts.Missed, warmCounts.Written,
				coldCounts.Hit+coldCounts.Missed)
		}
	}
	if withoutCache(coldStream) != withoutCache(warmStream) {
		test.Errorf("the warm run's stream differs from the cold run's:\ncold\n%s\nwarm\n%s", coldStream, warmStream)
	}
}

// Under `--other-builds` each extra build's keys read the digests of the files the host's keys hashed, so a run hashes
// every file of the host's build once, and of each extra build only the files its load holds that the host's does
// not. The fixture gains a package importing os, so windows reaches standard-library packages the host's build never
// does, and has files of its own to hash.
//
// Not parallel: it reads cache.FilesHashed, which every test in the process that keys a package moves.
func TestOtherBuildsHashOnlyTheFilesTheHostsDidNot(test *testing.T) {
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	reach := filepath.Join(root, "internal", "reach")
	if makeError := os.MkdirAll(reach, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	reachFile := "// Package reach reaches os, whose files differ by platform.\npackage reach\n\nimport \"os\"\n\n" +
		"// Name is the program's name.\nfunc Name() string { return os.Args[0] }\n"
	if writeError := os.WriteFile(filepath.Join(reach, "reach.go"), []byte(reachFile), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	before := cache.FilesHashed()
	engine, _ := runOverWith(test, root, exitFindings, "--other-builds")
	hashed := cache.FilesHashed() - before
	if len(engine.buildRuns) != 3 {
		test.Fatalf("the merge was handed %d runs, want 3", len(engine.buildRuns))
	}
	hostFiles := map[string]bool{}
	ownFiles := 0
	for file := range engine.buildRuns[0].files.Paths() {
		hostFiles[file] = true
	}
	want := int64(len(hostFiles))
	for _, extra := range engine.buildRuns[1:] {
		shared, own := 0, 0
		for file := range extra.files.Paths() {
			if hostFiles[file] {
				shared++
				continue
			}
			own++
		}
		if shared == 0 {
			test.Fatalf("%s keyed no file the host's did, so there was nothing to read rather than hash", extra.build)
		}
		ownFiles += own
		want += int64(own)
	}
	if ownFiles == 0 {
		test.Fatal("no extra build keyed a file the host's did not, so nothing tells hashing those from not")
	}
	if hashed != want {
		test.Errorf("want the host's %d files and each extra build's own hashed, %d in all, got %d hashed",
			len(hostFiles), want, hashed)
	}
}

// describeFile is a file of the other-builds fixture's label package, which every build compiles, with a line past
// the limit that the long-line wrap breaks after a comma.
const describeFile = "package label\n\nimport \"fmt\"\n\n" +
	"// Describe says where a delivery goes and when, as one sentence a reader can follow.\n" +
	"func Describe(sender string, recipient string, street string, city string, weekday string) string {\n" +
	"\treturn fmt.Sprintf(\"%s sends a parcel to %s, who lives at %s in %s, and it arrives on %s\", " +
	"sender, recipient, street, city, weekday)\n}\n"

// When the long-line wrap writes a file in a run under `--other-builds`, the load the lint reads is a second one, whose
// Restore keys every file again, and each extra build reads the digests of that Restore, of the bytes on disk after
// the wrap, never the first's, of the bytes it replaced: an extra build keyed on the replaced bytes would save entries
// under keys of a file no longer on disk. The digests are what this holds every build to. Once the wrapped file is put
// back as it was, a cached `--no-fix` run also finds what a `--no-cache` one does, though the merge's verdict alone
// does not tell the stale digests apart: the host's finding on the file stands whatever its extra builds restore.
func TestOtherBuildsReadTheDigestsOfTheBytesTheWrapWrote(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	file := filepath.Join(root, "internal", "label", "describe.go")
	if writeError := os.WriteFile(file, []byte(describeFile), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	engine, stream := runOverWith(test, root, exitFindings, "--other-builds")
	if !strings.Contains(stream, `"fixedBy":{"cohere-go/consistency-no-long-line":1}`) {
		test.Fatalf("want the wrap to break the long line of %s, got\n%s", file, stream)
	}
	wrapped, readError := os.ReadFile(file)
	if readError != nil || string(wrapped) == describeFile {
		test.Fatalf("the run left %s as it was (%v)", file, readError)
	}
	if len(engine.buildRuns) != 3 {
		test.Fatalf("the merge was handed %d runs, want 3", len(engine.buildRuns))
	}
	onDisk := sha256.Sum256(wrapped)
	for _, run := range engine.buildRuns {
		digest, held := run.files.Digest(file)
		if !held || digest != onDisk {
			test.Errorf("%s keyed %s at %x (held %t), want the wrapped bytes on disk, %x", run.build, file, digest,
				held, onDisk)
		}
	}

	if writeError := os.WriteFile(file, []byte(describeFile), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	_, cached := runOverWith(test, root, exitFindings, "--other-builds", "--no-fix")
	_, uncached := runOverWith(test, root, exitFindings, "--other-builds", "--no-fix", "--no-cache")
	if withoutCache(cached) != withoutCache(uncached) {
		test.Errorf("over the bytes the wrap replaced, the cached run's stream differs from a --no-cache run's:\n"+
			"cached\n%s\nwithout the cache\n%s", cached, uncached)
	}
}

// withoutCache is a normalized stream with its lint record's cache counts left out, the one field a warm run writes
// otherwise than a cold one, and each build's counts of what it restored from the host's run, which a warm host's
// run, restoring from its own store, keeps nothing for (withoutHostRestore).
func withoutCache(stream string) string {
	lines := strings.Split(withoutHostRestore(stream), "\n")
	for index, line := range lines {
		if start := strings.Index(line, `"cache":{`); start >= 0 {
			end := strings.Index(line[start:], "}")
			lines[index] = line[:start] + line[start+end+1:]
		}
	}
	return strings.Join(lines, "\n")
}

// hostRestoreCounts are a build record's counts of what the host's run kept for the extra builds and what each restored
// from it (contract.BuildRecord), each written only when not 0, with the packages an extra build checked from source,
// which are what it did not restore.
var hostRestoreCounts = regexp.MustCompile(
	`,"(entriesKept|restoredFromHost|declinedAtLoad|declinedAtCheck|reloadRounds|checkedFromSource)":[0-9]+` +
		`|,"(declinedBy|reloadRoundsBy)":\{[^}]*\}`)

// withoutHostRestore is stream with every build record's counts of the restore from the host's run left out, the one
// thing a run that restores writes otherwise than one that checks every package of every build.
func withoutHostRestore(stream string) string {
	return hostRestoreCounts.ReplaceAllString(stream, "")
}

// A module whose every file the host builds needs no other build, so `--other-builds` hands the merge nothing, and the
// run writes the stream it writes without the switch, byte for byte, with no builds in its lint record.
func TestOtherBuildsOverOneBuildRunNothingExtra(test *testing.T) {
	test.Parallel()
	clean := filepath.Join("testdata", "clean")
	engine, onStream := runOver(test, copyFixture(test, clean), exitClean, "--other-builds")
	if engine.buildRuns != nil {
		test.Errorf("the merge was handed %d runs over a module one build covers", len(engine.buildRuns))
	}
	_, offStream := runOver(test, copyFixture(test, clean), exitClean)
	if onStream != offStream {
		test.Errorf("the switch changed the stream over one build:\nwith it\n%s\nwithout it\n%s", onStream,
			offStream)
	}
}

// Under `--other-builds` the lint phase writes the merge of every build's findings (#72n9mq0): the windows file's else
// after a return, which only windows's run finds, is written as a finding, the run exits with findings, and the lint
// record names the three builds, the host's first, with windows's the one that added it. Without the switch the same
// tree reads clean, since the host never compiles that file.
func TestOtherBuildsWriteTheMergedVerdict(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	engine, stream := runOver(test, root, exitFindings, "--other-builds")
	var lint contract.LintRecord
	var found []contract.FindingRecord
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		switch {
		case strings.Contains(line, `"kind":"lint"`):
			if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
				test.Fatal(decodeError)
			}
		case strings.Contains(line, `"kind":"finding"`):
			var finding contract.FindingRecord
			if decodeError := json.Unmarshal([]byte(line), &finding); decodeError != nil {
				test.Fatal(decodeError)
			}
			found = append(found, finding)
		}
	}
	windowsFile := "/project/internal/separator/separator_windows.go"
	if len(found) != 1 || found[0].Rule != consistency_no_else_return.Name || found[0].File != windowsFile {
		test.Errorf("the run wrote %+v, want the one %s in %s", found, consistency_no_else_return.Name, windowsFile)
	}
	if len(lint.Builds) != 3 || !lint.Builds[0].Host || lint.Builds[1].Host || lint.Builds[2].Host {
		test.Fatalf("the lint record names the builds %+v, want three, the host's first", lint.Builds)
	}
	// The stream reads every target as `target` (normalize), so each record is known by the run it came from, which
	// the merge writes in the order it was handed the runs.
	for index, build := range lint.Builds {
		wantAdded := 0
		if engine.buildRuns[index].build.OperatingSystem == "windows" {
			wantAdded = 1
		}
		if build.AnalyzedPackages != len(engine.buildRuns[0].analyzed) {
			test.Errorf("the build %s analyzed %d packages, want every one of the %d its load holds",
				engine.buildRuns[index].build, build.AnalyzedPackages, len(engine.buildRuns[0].analyzed))
		}
		if build.FindingsAdded != wantAdded || build.FindingsDropped != 0 {
			test.Errorf("the build %s added %d and dropped %d, want %d and 0", engine.buildRuns[index].build,
				build.FindingsAdded, build.FindingsDropped, wantAdded)
		}
	}
}

// A //cohere:ignore in a file only another build compiles is read and honored: the merge indexes the directives of a
// file the host's build constraints leave out, from its contents as the run read them, so the windows file's else
// after a return, ignored with a reason on the line above, is withheld and counted, the run is clean, and no build
// calls the directive unused.
func TestOtherBuildsHonorADirectiveInAFileOnlyAnotherBuildCompiles(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	windowsFile := filepath.Join(root, "internal", "separator", "separator_windows.go")
	source, readError := os.ReadFile(windowsFile)
	if readError != nil {
		test.Fatal(readError)
	}
	lines := strings.Split(string(source), "\n")
	elseAt := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "} else {") })
	if elseAt < 0 {
		test.Fatalf("%s has no else to ignore", windowsFile)
	}
	directive := "\t//cohere:ignore " + consistency_no_else_return.Name + " the fixture's planted finding, ignored here"
	lines = slices.Insert(lines, elseAt, directive)
	if writeError := os.WriteFile(windowsFile, []byte(strings.Join(lines, "\n")), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	_, stream := runOver(test, root, exitClean, "--other-builds")
	if !strings.Contains(stream, `"withheldBySuppression":{"`+consistency_no_else_return.Name+`":1}`) {
		test.Errorf("the run withheld nothing of %s:\n%s", consistency_no_else_return.Name, stream)
	}
}

// Under `--other-builds` the inputs record names what every build's load read, not the host's alone (#e4b08k7), so
// the front door's replay misses when a file only another build reads moves. A windows-only file of the fixture's
// imports os, which nothing the host's build compiles imports, so the standard library's os/file_windows.go is read
// by windows's load alone, and the record must name it.
func TestOtherBuildsNameEveryBuildsInputs(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	console := "package separator\n\nimport \"os\"\n\n" +
		"// Console is where a write is discarded, as windows names it.\nvar Console = os.DevNull\n"
	consoleFile := filepath.Join(root, "internal", "separator", "console_windows.go")
	if writeError := os.WriteFile(consoleFile, []byte(console), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	_, stream := runOverRaw(test, root, exitFindings, "--no-cache", "--other-builds")
	var inputs contract.InputsRecord
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		if strings.Contains(line, `"kind":"inputs"`) {
			if decodeError := json.Unmarshal([]byte(line), &inputs); decodeError != nil {
				test.Fatal(decodeError)
			}
		}
	}
	windowsOnly := filepath.Join("src", "os", "file_windows.go")
	named := slices.ContainsFunc(inputs.Files, func(file string) bool {
		return strings.HasSuffix(file, string(filepath.Separator)+windowsOnly)
	})
	if !named {
		test.Errorf("the inputs record names %d files and not the standard library's %s, which only windows's load "+
			"read", len(inputs.Files), windowsOnly)
	}
}

// sortedKeys is a set's members, sorted and joined, for a message.
func sortedKeys(set map[string]bool) string {
	return strings.Join(slices.Sorted(maps.Keys(set)), ", ")
}

// A type error only another build finds is a finding of the run, written in the lint phase as a compiler finding, and
// the lint record counts it in typeErrors beside its findings, so the phase's finding records are what its record
// says (#rw6tg3a): before, the record counted the rule findings alone, and the front door refused the run as an
// engine that broke its contract. The fixture's windows-only file names a constant no build declares.
func TestAnOtherBuildsTypeErrorIsCountedInTheLintRecord(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	broken := "//go:build windows\n\npackage separator\n\n// Broken names a constant no build declares.\nconst Broken = Missing\n"
	if writeError := os.WriteFile(filepath.Join(root, "internal", "separator", "broken_windows.go"), []byte(broken),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	_, stream := runOverRaw(test, root, exitFindings, "--no-cache", "--other-builds")
	inLint := false
	compilerRecords, ruleRecords := 0, 0
	var lint contract.LintRecord
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		var kind struct {
			Kind   string `json:"kind"`
			Source string `json:"source"`
			Name   string `json:"name"`
		}
		if decodeError := json.Unmarshal([]byte(line), &kind); decodeError != nil {
			test.Fatalf("%v: %s", decodeError, line)
		}
		switch {
		case kind.Kind == "phase" && kind.Name == "types":
			inLint = true
		case kind.Kind == "finding" && inLint && kind.Source == "compiler":
			compilerRecords++
		case kind.Kind == "finding" && inLint:
			ruleRecords++
		case kind.Kind == "lint":
			if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
				test.Fatal(decodeError)
			}
			inLint = false
		}
	}
	if compilerRecords == 0 {
		test.Fatalf("the windows build's type error was written as no compiler finding:\n%s", stream)
	}
	if lint.TypeErrors != compilerRecords || lint.Findings != ruleRecords {
		test.Errorf("the lint record counts %d findings and %d type errors, and the phase wrote %d rule findings and "+
			"%d compiler findings", lint.Findings, lint.TypeErrors, ruleRecords, compilerRecords)
	}
}

// Each build's record says what it alone covered, and what it compiled that no rule ran on (#ytnvt8k). Over the
// other-builds fixture the windows build alone compiles separator_windows.go and lints it. With a windows-only type
// error beside it, the windows build analyzes no package, so it lints nothing alone, and both windows-only files
// are named as compiled and not linted, with the type error as why.
func TestEachBuildSaysWhatItAloneCoveredAndWhatNoRuleRanOn(test *testing.T) {
	test.Parallel()
	windowsRecord := func(root string, wantExit int) contract.BuildRecord {
		test.Helper()
		_, stream := runOverRaw(test, root, wantExit, "--no-cache", "--other-builds")
		for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
			var lint contract.LintRecord
			if !strings.HasPrefix(line, `{"kind":"lint"`) {
				continue
			}
			if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
				test.Fatal(decodeError)
			}
			for _, build := range lint.Builds {
				if strings.HasPrefix(build.Target, "windows/") {
					return build
				}
			}
		}
		test.Fatalf("no lint record names a windows build:\n%s", stream)
		return contract.BuildRecord{}
	}

	clean := windowsRecord(copyFixture(test, filepath.Join("testdata", "other-builds")), exitFindings)
	if clean.FilesAlone != 1 || len(clean.NotLinted) != 0 {
		test.Errorf("the windows build alone linted %d files and left %v unlinted, want 1 and none", clean.FilesAlone,
			clean.NotLinted)
	}

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	broken := filepath.Join(root, "internal", "separator", "broken_windows.go")
	if writeError := os.WriteFile(broken, []byte("//go:build windows\n\npackage separator\n\n"+
		"// Broken names a constant no build declares.\nconst Broken = Missing\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	failed := windowsRecord(root, exitFindings)
	var names []string
	for _, file := range failed.NotLinted {
		names = append(names, filepath.Base(file))
	}
	if failed.FilesAlone != 0 || !slices.Equal(names, []string{"broken_windows.go", "separator_windows.go"}) ||
		!strings.Contains(failed.NotLintedReason, "found a type error, so it analyzed no package") {
		test.Errorf("the windows build alone linted %d files and left %v unlinted for %q, want 0 and both its files "+
			"for its type error", failed.FilesAlone, names, failed.NotLintedReason)
	}
}

// A file two extra builds compile is linted when either analyzes it, so it is never named not linted because the
// first of them is broken (#ytnvt8k). Beside the other-builds fixture: a file every platform but the host's compiles,
// a type error only the first extra build compiles, so that build analyzes no package, and a file only the second
// compiles, so both builds run. The shared file is linted by the second; only the first build's own file is named.
func TestAFileTwoBuildsCompileIsLintedWhenEitherAnalyzesIt(test *testing.T) {
	test.Parallel()
	var others []string
	for _, operatingSystem := range []string{"linux", "windows", "darwin", "freebsd"} {
		if operatingSystem != runtime.GOOS {
			others = append(others, operatingSystem)
		}
	}
	first, second := others[0], others[1]
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	files := map[string]string{
		"shared.go": "// Package shared is compiled beside the fixture's.\npackage shared\n",
		"shared_elsewhere.go": "//go:build !" + runtime.GOOS +
			"\n\npackage shared\n\n// Elsewhere is one.\nconst Elsewhere = 1\n",
		"broken_first.go": "//go:build " + first +
			"\n\npackage shared\n\n// Broken is undeclared.\nconst Broken = Missing\n",
		"only_second.go": "//go:build " + second + "\n\npackage shared\n\n// Second is two.\nconst Second = 2\n",
	}
	for name, contents := range files {
		path := filepath.Join(root, "internal", "shared", name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	_, stream := runOverRaw(test, root, exitFindings, "--no-cache", "--other-builds")
	var lint contract.LintRecord
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		if strings.HasPrefix(line, `{"kind":"lint"`) {
			if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
				test.Fatal(decodeError)
			}
		}
	}
	notLinted := map[string]string{}
	for _, build := range lint.Builds {
		for _, file := range build.NotLinted {
			notLinted[filepath.Base(file)] = build.Target
		}
	}
	if target, named := notLinted["shared_elsewhere.go"]; named {
		test.Errorf("shared_elsewhere.go, which the %s build lints, is named not linted under %s", second, target)
	}
	if target := notLinted["broken_first.go"]; !strings.HasPrefix(target, first+"/") {
		test.Errorf("broken_first.go is named not linted under %q, want the %s build's: %v", target, first, notLinted)
	}
}

// A target the settings name that go tool dist list does not is refused when the project is described, before any
// record, since the settings asked for a check that can never be made (#ntamp9e), and one it does target is
// accepted.
func TestATargetTheToolchainDoesNotListIsRefusedAtLoad(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	describe := func(targets []string) (*describedProject, error) {
		test.Helper()
		request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
			"--no-cache", "--other-builds"})
		if parseError != nil {
			test.Fatal(parseError)
		}
		request.targets = targets
		return describeProject(request)
	}
	if _, refused := describe([]string{"plan9/arm64"}); refused == nil ||
		!strings.Contains(refused.Error(), "the go block's targets name plan9/arm64, which go tool dist list does "+
			"not target") {
		test.Errorf("an unknown target described as %v, want it refused by name", refused)
	}
	// A target the toolchain lists is accepted. It widens what selection may choose and forces no build: this
	// fixture holds no file only wasm compiles, so selection still needs none there.
	if _, describeError := describe([]string{"wasip1/wasm"}); describeError != nil {
		test.Errorf("a target the toolchain lists was refused: %v", describeError)
	}
}
