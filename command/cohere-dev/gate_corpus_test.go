//go:build unix

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/testing/corpus"
	"github.com/system-inc/cohere/internal/testing/needs"
)

// corpusFixtureGit runs git in directory with no configuration of the machine's, failing the test on an error.
func corpusFixtureGit(test *testing.T, directory string, arguments ...string) string {
	test.Helper()
	git := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com",
		"-c", "protocol.file.allow=always"}, arguments...)...)
	git.Dir = directory
	git.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	output, gitError := git.CombinedOutput()
	if gitError != nil {
		test.Fatalf("git %v: %v\n%s", arguments, gitError, output)
	}
	return strings.TrimSpace(string(output))
}

// longFixturePath is 228 characters, which a `USTAR` header holds, as git archive writes it, and which the 55 of a
// place's prefix push past the 255 it can split.
var longFixturePath = filepath.Join("modules", strings.Repeat("a", 60), strings.Repeat("c", 60),
	strings.Repeat("b", 95)+".md")

// corpusFixtures is a Structure checkout with two commits, and an ahra checkout whose one commit holds a data/
// directory and records Structure's first commit as its libraries/structure submodule, filled in from a clone of it.
type corpusFixtures struct {
	structure, structureFirst, structureSecond string
	ahra, ahraCommit                           string
}

func newCorpusFixtures(test *testing.T) corpusFixtures {
	test.Helper()
	fixtures := corpusFixtures{structure: test.TempDir(), ahra: test.TempDir()}
	corpusFixtureGit(test, fixtures.structure, "init", "-q")
	writeFixtureFile(test, filepath.Join(fixtures.structure, "source", "first.ts"), "export const first = 1;\n")
	corpusFixtureGit(test, fixtures.structure, "add", ".")
	corpusFixtureGit(test, fixtures.structure, "commit", "-q", "-m", "first")
	fixtures.structureFirst = corpusFixtureGit(test, fixtures.structure, "rev-parse", "HEAD")
	writeFixtureFile(test, filepath.Join(fixtures.structure, "source", "second.ts"), "export const second = 2;\n")
	corpusFixtureGit(test, fixtures.structure, "add", ".")
	corpusFixtureGit(test, fixtures.structure, "commit", "-q", "-m", "second")
	fixtures.structureSecond = corpusFixtureGit(test, fixtures.structure, "rev-parse", "HEAD")
	corpusFixtureGit(test, fixtures.ahra, "init", "-q")
	writeFixtureFile(test, filepath.Join(fixtures.ahra, "app", "theme.css"), "@import \"tailwindcss\";\n")
	// A path that fits a `USTAR` header alone, as git archive writes it, and not under a place's prefix.
	writeFixtureFile(test, filepath.Join(fixtures.ahra, longFixturePath), "long\n")
	writeFixtureFile(test, filepath.Join(fixtures.ahra, "data", "conversations", "private.md"), "never laid\n")
	corpusFixtureGit(test, fixtures.ahra, "add", ".")
	corpusFixtureGit(test, fixtures.ahra, "clone", "-q", fixtures.structure, filepath.Join("libraries", "structure"))
	corpusFixtureGit(test, filepath.Join(fixtures.ahra, "libraries", "structure"), "checkout", "-q",
		fixtures.structureFirst)
	corpusFixtureGit(test, fixtures.ahra, "update-index", "--add", "--cacheinfo",
		"160000,"+fixtures.structureFirst+",libraries/structure")
	corpusFixtureGit(test, fixtures.ahra, "commit", "-q", "-m", "ahra")
	fixtures.ahraCommit = corpusFixtureGit(test, fixtures.ahra, "rev-parse", "HEAD")
	return fixtures
}

// layScriptHere is corpusLayScript with the pinned node replaced by a stand-in whose digest it is, so the script runs
// on a machine without Loom's tools; the stand-in installs nothing, since the corpora here install with none. Its free
// disk floor is floor GiB rather than corpusFreeFloor, so a test never reads how full the machine it runs on is: a
// land gate fills its box's disk with the Go cache it builds, and this test refused with 6 GiB free on Sun2 (Nov 23).
func layScriptHere(test *testing.T, floor string) string {
	test.Helper()
	node := filepath.Join(test.TempDir(), "node")
	writeFixtureFile(test, filepath.Join(node, "bin", "node"), "#!/bin/sh\nexit 1\n")
	if chmodError := os.Chmod(filepath.Join(node, "bin", "node"), 0o755); chmodError != nil {
		test.Fatal(chmodError)
	}
	contents, readError := os.ReadFile(filepath.Join(node, "bin", "node"))
	if readError != nil {
		test.Fatal(readError)
	}
	digest := sha256.Sum256(contents)
	script := strings.Replace(corpusLayScript, `node="`+macBoxNode+`"; nodeDigest="`+macBoxNodeDigest+`"`,
		`node="`+node+`"; nodeDigest="`+hex.EncodeToString(digest[:])+`"`, 1)
	if script == corpusLayScript {
		test.Fatal("the lay script's node line was not found to replace")
	}
	floorLine := "\nfloor=" + corpusFreeFloor + "\n"
	if strings.Count(script, floorLine) != 1 {
		test.Fatal("the lay script's free disk floor was not found to replace")
	}
	return strings.Replace(script, floorLine, "\nfloor="+floor+"\n", 1)
}

// A carry, laid by the box's own script: ahra's tree without its data/, Structure filled in at the commit ahra records,
// and Structure as a repository holding both its pins, checked out at its checkout pin. Each is read-only in its place.
// A second lay of the same commits lays nothing, and laying another commit of a corpus removes the one before it.
func TestTheCorporaAreLaidAsTheirPinsSay(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the lay script is a bash script")
	needs.Tool(test, "sha256sum", "the lay script checks the node it installs with by its digest")
	fixtures := newCorpusFixtures(test)
	corpora := []gateCorpus{
		{corpus: corpus.Ahra, pins: corpus.Pins{Checkout: fixtures.ahraCommit, Held: []string{fixtures.ahraCommit}},
			checkout: fixtures.ahra},
		{corpus: corpus.Structure, pins: corpus.Pins{Checkout: fixtures.structureSecond,
			Held: []string{fixtures.structureFirst, fixtures.structureSecond}}, checkout: fixtures.structure},
	}
	home := test.TempDir()
	// The laid corpora are read-only, so they are given write back before the temporary directory is removed.
	test.Cleanup(func() {
		if output, chmodError := exec.Command("chmod", "-R", "u+w", home).CombinedOutput(); chmodError != nil {
			test.Errorf("giving the laid corpora write back: %v %s", chmodError, output)
		}
	})
	script := layScriptHere(test, "0")
	lay := func(corpora []gateCorpus) string {
		test.Helper()
		directory := test.TempDir()
		carry := filepath.Join(directory, "corpora.tar.gz")
		specs, carryError := writeCorpusCarry(carry, directory, corpora)
		if carryError != nil {
			test.Fatal(carryError)
		}
		for index, spec := range specs {
			// Installed with none: an install fetches packages, which a test can't.
			fields := strings.Split(spec, "/")
			fields[2] = corpusInstallNone
			specs[index] = strings.Join(fields, "/")
		}
		bash := exec.Command("bash", append([]string{"-s", "--", carry}, specs...)...)
		bash.Dir, bash.Stdin = test.TempDir(), strings.NewReader(script)
		bash.Env = append(os.Environ(), "HOME="+home, "TMPDIR="+test.TempDir(), "GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=/dev/null")
		output, bashError := bash.CombinedOutput()
		if bashError != nil {
			test.Fatalf("the lay script failed: %v\n%s", bashError, output)
		}
		return string(output)
	}
	said := lay(corpora)
	laid := filepath.Join(home, "scratch", "system_cohere_tools", "corpora")
	ahra := filepath.Join(laid, filepath.FromSlash(corpora[0].place()))
	structure := filepath.Join(laid, filepath.FromSlash(corpora[1].place()))
	for _, want := range []string{corpusLaidMarker + "ahra at " + filepath.Base(ahra),
		corpusLaidMarker + "structure at " + filepath.Base(structure)} {
		if !strings.Contains(said, want) {
			test.Errorf("the lay script never said %q:\n%s", want, said)
		}
	}
	if _, statError := os.Stat(filepath.Join(ahra, "app", "theme.css")); statError != nil {
		test.Errorf("ahra's tree wasn't laid: %v", statError)
	}
	if _, statError := os.Stat(filepath.Join(ahra, longFixturePath)); statError != nil {
		test.Errorf("ahra's long path wasn't laid: %v", statError)
	}
	if _, statError := os.Stat(filepath.Join(ahra, "data")); !os.IsNotExist(statError) {
		test.Errorf("ahra's data/ was laid, which never leaves the lander: %v", statError)
	}
	if _, statError := os.Stat(filepath.Join(ahra, "libraries", "structure", "source", "first.ts")); statError != nil {
		test.Errorf("ahra's Structure submodule wasn't filled in at the commit it records: %v", statError)
	}
	if _, statError := os.Stat(filepath.Join(ahra, "libraries", "structure", "source", "second.ts")); statError == nil {
		test.Error("ahra's Structure submodule holds a file of a commit after the one ahra records")
	}
	if _, statError := os.Stat(filepath.Join(structure, "source", "second.ts")); statError != nil {
		test.Errorf("Structure isn't checked out at its checkout pin: %v", statError)
	}
	for _, commit := range []string{fixtures.structureFirst, fixtures.structureSecond} {
		corpusFixtureGit(test, structure, "cat-file", "-e", commit+"^{commit}")
	}
	if head := corpusFixtureGit(test, structure, "rev-parse", "HEAD"); head != fixtures.structureSecond {
		test.Errorf("Structure's laid repository is at %s, want %s", head, fixtures.structureSecond)
	}
	if information, statError := os.Stat(filepath.Join(ahra, "app", "theme.css")); statError != nil ||
		information.Mode().Perm()&0o222 != 0 {
		test.Errorf("ahra's laid tree is writable: %v %v", information, statError)
	}
	if again := lay(corpora); strings.Contains(again, corpusLaidMarker) {
		test.Errorf("a second lay of the same commits laid something:\n%s", again)
	}
	corpora[1].pins = corpus.Pins{Checkout: fixtures.structureFirst, Held: []string{fixtures.structureFirst}}
	moved := lay(corpora[1:])
	if _, statError := os.Stat(structure); !os.IsNotExist(statError) ||
		!strings.Contains(moved, corpusLaidMarker+"structure at "+filepath.Base(structure)+" removed") {
		test.Errorf("laying Structure at another commit left the one before it (%v):\n%s", statError, moved)
	}
	if _, statError := os.Stat(filepath.Join(laid, filepath.FromSlash(corpora[1].place()), "source",
		"first.ts")); statError != nil {
		test.Errorf("Structure wasn't laid at its new pin: %v", statError)
	}
}

// A box whose disk would be left under the floor refuses to lay, in the gate's refusal, naming what is free and the
// floor, before it unpacks anything. No disk holds a billion GiB free.
func TestALayRefusesADiskUnderItsFloor(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the lay script is a bash script")
	needs.Tool(test, "sha256sum", "the lay script checks the node it installs with by its digest")
	home := test.TempDir()
	bash := exec.Command("bash", "-s", "--", filepath.Join(test.TempDir(), "never-read.tar.gz"))
	bash.Dir, bash.Stdin = test.TempDir(), strings.NewReader(layScriptHere(test, "1000000000"))
	bash.Env = append(os.Environ(), "HOME="+home, "TMPDIR="+test.TempDir())
	output, bashError := bash.CombinedOutput()
	if bashError == nil || !strings.Contains(string(output), sharedBoxRefusedMarker+"only ") ||
		!strings.Contains(string(output), " GiB is free on this box, under the 1000000000 GiB the corpora leave") {
		test.Errorf("a lay under its floor wasn't refused (%v):\n%s", bashError, output)
	}
}

// The pin file is found in the tree being landed by its name, wherever it lives, never at the path the land's own code
// names, so a landing queued before the file moved still lays its corpora; a cohere tree, by its go.mod, with no pin
// file refuses the landing naming it, rather than laying no corpus and letting its corpus tests skip on a green gate
// (#3kpxrdt); two pin files refuse; and a tree that isn't cohere's lays none, as a test's fixture does.
func TestTheGateFindsItsPinsByNameAndRefusesACohereTreeWithout(test *testing.T) {
	test.Parallel()
	pinned := "package corpus\n\nconst AhraPin = \"0123456789012345678901234567890123456789\"\n"
	tree := func(files map[string]string) (string, string) {
		test.Helper()
		directory := test.TempDir()
		for name, contents := range files {
			writeFixtureFile(test, filepath.Join(directory, filepath.FromSlash(name)), contents)
		}
		for _, arguments := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=test", "-c",
			"user.email=test@example.com", "commit", "-q", "-m", "a tree"}} {
			git := exec.Command("git", arguments...)
			git.Dir, git.Env = directory, append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
			if output, gitError := git.CombinedOutput(); gitError != nil {
				test.Fatalf("git %v: %v\n%s", arguments, gitError, output)
			}
		}
		commit, gitError := gitOutput(directory, "rev-parse", "HEAD")
		if gitError != nil {
			test.Fatal(gitError)
		}
		return directory, commit
	}
	cohereModuleLine := "module " + cohereModule + "\n\ngo 1.27\n"
	for _, row := range []struct {
		name    string
		files   map[string]string
		found   string
		refusal string
	}{
		{"where the land's code looks", map[string]string{"go.mod": cohereModuleLine,
			corpus.PinnedFile: pinned}, corpus.PinnedFile, ""},
		{"moved somewhere the land's code never looked", map[string]string{"go.mod": cohereModuleLine,
			"internal/somewhere/else/" + coherePinnedName: pinned}, "internal/somewhere/else/" + coherePinnedName, ""},
		{"a cohere tree with none", map[string]string{"go.mod": cohereModuleLine},
			"", "is a cohere tree with no " + coherePinnedName + " anywhere"},
		{"two of them", map[string]string{"go.mod": cohereModuleLine, corpus.PinnedFile: pinned,
			"old/" + coherePinnedName: pinned}, "", "holds 2 files named " + coherePinnedName},
		{"a tree that isn't cohere's", map[string]string{"go.mod": "module example.com/fixture\n"}, "", ""},
	} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			directory, commit := tree(row.files)
			found, isFound, findError := pinnedFileIn(directory, commit)
			said := ""
			if findError != nil {
				said = findError.Error()
			}
			if found != row.found || isFound != (row.found != "") || (row.refusal == "") != (findError == nil) ||
				!strings.Contains(said, row.refusal) {
				test.Errorf("found %q (%t), %q; want %q and a refusal saying %q", found, isFound, said, row.found,
					row.refusal)
			}
		})
	}
}

// A gate that skips far more tests than main's last landing says so, loudly: more than twice as many and at least
// skipJumpFloor more. 196 against 10 is the jump #3kpxrdt's two landings printed and passed over.
func TestAGateSaysWhenItsSkipsJump(test *testing.T) {
	test.Parallel()
	for _, row := range []struct {
		now, before int
		jumps       bool
	}{
		{196, 10, true},
		{25, 0, true},
		{10, 10, false},
		{25, 10, false},
		{19, 0, false},
		{196, -1, false},
	} {
		if said := skipJump(row.now, row.before); (said != "") != row.jumps {
			test.Errorf("%d against %d reads %q, want a jump: %t", row.now, row.before, said, row.jumps)
		}
	}
}

// The gate's Mac script unsets every corpus's variable, so the list it unsets is every corpus's.
func TestTheGatesMacScriptUnsetsEveryCorpusVariable(test *testing.T) {
	test.Parallel()
	variables := []string{}
	for _, each := range corpus.All {
		variables = append(variables, each.Variable)
	}
	if macBoxCorpusVariables != strings.Join(variables, " ") {
		test.Errorf("the script unsets %q, want every corpus's variable, %q", macBoxCorpusVariables,
			strings.Join(variables, " "))
	}
}

// The gate's Mac script sets each corpus's variable to its laid copy, and a corpus that isn't laid there is named, the
// script testing nothing: it never reaches the toolchain, which this box lacks. A variable its environment carries is
// never what a test reads.
func TestTheGatesMacScriptNamesTheCorporaItLacks(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the box's script is a bash script")
	source := test.TempDir()
	mirrors := test.TempDir()
	corpusFixtureGit(test, source, "init", "-q")
	writeFixtureFile(test, filepath.Join(source, "go.mod"), "module example.com/module\n\ngo 1.27\n")
	corpusFixtureGit(test, source, "add", ".")
	corpusFixtureGit(test, source, "commit", "-q", "-m", "a module")
	commit := corpusFixtureGit(test, source, "rev-parse", "HEAD")
	corpusFixtureGit(test, mirrors, "init", "-q", "--bare", "cohere.git")
	corpusFixtureGit(test, source, "push", "-q", filepath.Join(mirrors, "cohere.git"), "HEAD:refs/heads/main")
	noGo := filepath.Join(test.TempDir(), "no-go")
	script := strings.Replace(macBoxScript, macBoxMirrorLine, "mirrors="+mirrors+"; tries=1; pause=0", 1)
	script = strings.Replace(script, `goroot="`+macBoxGoroot+`"`, `goroot="`+noGo+`"`, 1)
	// The toolchain's refusal names the variable, so the test sees what the script set before it.
	script = strings.Replace(script, `refuse "there is no go at $goroot"`,
		`refuse "there is no go at $goroot, ahra at ${COHERE_CORPUS_AHRA:-none}, temporary at $TMPDIR"`, 1)
	home := test.TempDir()
	run := func(corpora string) string {
		test.Helper()
		bash := exec.Command("bash", "-s", "--", commit, "", "", corpora, "./...")
		bash.Dir, bash.Stdin = test.TempDir(), strings.NewReader(script)
		bash.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_NOSYSTEM=1", "COHERE_CORPUS_AHRA=/elsewhere")
		// Ignored: what the script did is read from what it said.
		output, _ := bash.CombinedOutput()
		return string(output)
	}
	laid := filepath.Join(home, "scratch", "system_cohere_tools", "corpora", "ahra", "abc")
	if said := run("COHERE_CORPUS_AHRA=ahra/abc,COHERE_CORPUS_STRUCTURE=structure/def"); !strings.Contains(said,
		corpusUnlaidMarker+"ahra/abc structure/def\n") || strings.Contains(said, "there is no go") {
		test.Errorf("a box lacking both corpora didn't name them and stop:\n%s", said)
	}
	if mkdirError := os.MkdirAll(laid, 0o755); mkdirError != nil {
		test.Fatal(mkdirError)
	}
	if said := run("COHERE_CORPUS_AHRA=ahra/abc"); !strings.Contains(said, "ahra at "+laid) {
		test.Errorf("a box holding ahra didn't set its variable to the laid copy:\n%s", said)
	}
	said := run("-")
	if !strings.Contains(said, "ahra at none") || strings.Contains(said, corpusUnlaidMarker) {
		test.Errorf("a landing pinning no corpus wasn't tested with none set:\n%s", said)
	}
	// The tests' temporary directory is under /tmp, which macOS reaches through a link, and it goes with the run.
	_, temporary, found := strings.Cut(strings.TrimSpace(said), "temporary at ")
	if !found || !strings.HasPrefix(temporary, "/tmp/cohere-gate.") {
		test.Fatalf("the tests' temporary directory wasn't made under /tmp:\n%s", said)
	}
	if _, statError := os.Stat(temporary); !os.IsNotExist(statError) {
		test.Errorf("the tests' temporary directory %s outlived the run: %v", temporary, statError)
	}
}

// A landing whose tree pins corpora lays on the gate box what she says she lacks, in a gap carrying them, then asks
// her again and lands on her verdict, naming on its line each corpus laid at its commit and each it couldn't lay. The
// carry holds ahra's tree without data/. A lay the box refuses refuses the landing, in her words, and never falls back.
func TestALandingLaysTheCorporaItsBoxLacks(test *testing.T) {
	test.Parallel()
	fixtures := newCorpusFixtures(test)
	connectedPin, forkPin := strings.Repeat("c", 40), strings.Repeat("f", 40)
	pinned := "package corpus\n\n" +
		"const AhraPin = \"" + fixtures.ahraCommit + "\"\n" +
		"const StructurePin = \"" + fixtures.structureSecond + "\"\n" +
		"const StructureCountPin = \"" + fixtures.structureFirst + "\"\n" +
		"const ConnectedPin = \"" + connectedPin + "\"\n" +
		"const PrettierForkPin = \"" + forkPin + "\"\n"
	configuration := filepath.Join(test.TempDir(), "corpora.json")
	writeFixtureFile(test, configuration, `{"ahra": "`+fixtures.ahra+`", "structure": "`+fixtures.structure+`"}`)
	// The run's own corpus variables are blanked, so the landing reads only this corpora file.
	environment := []string{corpus.ConfigVariable + "=" + configuration}
	for _, each := range corpus.All {
		environment = append(environment, each.Variable+"=")
	}
	ahraPlace := gateCorpus{corpus: corpus.Ahra, pins: corpus.Pins{Checkout: fixtures.ahraCommit}}.place()
	structurePlace := gateCorpus{
		corpus: corpus.Structure,
		pins:   corpus.Pins{Checkout: fixtures.structureSecond},
	}.place()
	newFixture := func() *macBoxFixture {
		test.Helper()
		fixture := newMacBoxFixture(test, false)
		worktree := fixture.worktree("a")
		writeFixtureFile(test, filepath.Join(worktree, filepath.FromSlash(corpus.PinnedFile)), pinned)
		fixture.git(worktree, "add", ".")
		fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("the corpora's pins"))
		fixture.reply("Sun2.run", macBoxPassing)
		fixture.reply("Sun2.unlaid", corpusUnlaidMarker+ahraPlace+" "+structurePlace+"\n")
		return fixture
	}
	test.Run("laid, then tested", func(test *testing.T) {
		test.Parallel()
		fixture := newFixture()
		landed, output, calls, _ := fixture.landing(environment...)
		if !landed {
			test.Fatalf("the landing did not land:\n%s", output)
		}
		lines := strings.Split(strings.TrimSpace(calls), "\n")
		gaps := []string{}
		for _, line := range lines {
			if strings.Contains(line, " -- ssh Sun2 ") {
				gaps = append(gaps, strings.Fields(line)[6])
			}
		}
		if strings.Join(gaps, " ") != "-- --carry --" {
			test.Errorf("Sun2 was asked %q, want a test, a lay carrying the corpora, and a test again:\n%s", gaps,
				calls)
		}
		remote := string(readMaybeMissing(test, filepath.Join(fixture.replies, "Sun2.remote")))
		if !strings.Contains(remote, " 'COHERE_CORPUS_AHRA="+ahraPlace+",COHERE_CORPUS_STRUCTURE="+structurePlace+
			"' ") {
			test.Errorf("the script wasn't handed the laid corpora's places:\n%s", remote)
		}
		lay := string(readMaybeMissing(test, filepath.Join(fixture.replies, "Sun2.lay")))
		for _, want := range []string{ahraPlace + "/pnpm/ ", structurePlace + "/none/" + fixtures.structureSecond} {
			if !strings.Contains(lay+" ", want) {
				test.Errorf("the lay's remote lacks %q:\n%s", want, lay)
			}
		}
		listing, listError := exec.Command("tar", "-t", "-z", "-f",
			filepath.Join(fixture.replies, "Sun2.carry")).CombinedOutput()
		if listError != nil {
			test.Fatalf("listing the carry: %v\n%s", listError, listing)
		}
		if !strings.Contains(string(listing), ahraPlace+"/app/theme.css") ||
			strings.Contains(string(listing), "/data/") {
			test.Errorf("the carry isn't ahra's tree without data/:\n%s", listing)
		}
		body := fixture.git(fixture.repository, "log", "-1", "--format=%B", "main")
		for _, want := range []string{
			"the corpora laid at their pins: ahra at " + shortCommit(fixtures.ahraCommit) + ", structure at " +
				shortCommit(fixtures.structureSecond),
			"connected isn't laid, since " + configuration + " names no checkout of it to lay from, so its tests " +
				"run nowhere",
			"prettier-fork isn't laid",
		} {
			if !strings.Contains(body, want) {
				test.Errorf("the landing's body lacks %q:\n%s", want, body)
			}
		}
	})
	test.Run("a lay the box refuses", func(test *testing.T) {
		test.Parallel()
		fixture := newFixture()
		fixture.reply("Sun2.lay.fails", sharedBoxRefusedMarker+"only 3 GiB is free on this box\n")
		landed, output, _, goCalls := fixture.landing(environment...)
		if landed || !strings.Contains(output, "laying it failed: Sun2 refused to lay the corpora: only 3 GiB") {
			test.Errorf("a lay Sun2 refused didn't refuse the landing in her words (landed %v):\n%s", landed, output)
		}
		if strings.Contains(goCalls, "test") {
			test.Errorf("Kirk's Mac ran a go test: %s", goCalls)
		}
	})
}

// The fork's lay builds exactly the bundles cohere loads from a fork's dist/prettier, BundleFiles, so a laid fork
// can stand where Kirk's checkout stands.
func TestTheForksLayBuildsEveryBundleTheFormatterLoads(test *testing.T) {
	test.Parallel()
	want := ""
	for _, name := range prettier.BundleFiles {
		want += " --file prettier/" + name
	}
	if corpusForkBundleFlags != want {
		test.Errorf("the fork's lay builds%s, want%s", corpusForkBundleFlags, want)
	}
}

// A corpus's place carries the digest of how it is laid, so changing the lay script or a corpus's recipe lays it again
// rather than trusting a copy laid the old way.
func TestACorpusLaidAnotherWayHasAnotherPlace(test *testing.T) {
	test.Parallel()
	fork := gateCorpus{corpus: corpus.PrettierFork, pins: corpus.Pins{Checkout: corpus.PrettierForkPin}}
	ahra := gateCorpus{corpus: corpus.Ahra, pins: corpus.Pins{Checkout: corpus.AhraPin}}
	if fork.place() != "prettier-fork/"+corpus.PrettierForkPin+"-"+corpusRecipeDigest(corpus.PrettierFork.Name) ||
		fork.place() == ahra.place() {
		test.Fatalf("the fork's place is %s and ahra's %s", fork.place(), ahra.place())
	}
	before := corpusRecipeDigest(corpus.PrettierFork.Name)
	recipe := corpusRecipes[corpus.PrettierFork.Name]
	recipe.install = corpusInstallNone
	if digest := recipeDigestOf(corpusLayScript, recipe); digest == before {
		test.Errorf("the fork laid with another install keeps its digest, %s", digest)
	}
	if digest := recipeDigestOf(corpusLayScript+"\n", corpusRecipes[corpus.PrettierFork.Name]); digest == before {
		test.Errorf("the fork laid by another lay script keeps its digest, %s", digest)
	}
}
