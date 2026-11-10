package main

import (
	goformat "go/format"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goSaveFiles is a Go module whose value.go is on disk formatted, type-clean and unlike every buffer saved over it, so
// an answer computed from the disk rather than the buffer can never pass for one computed from the buffer.
var goSaveFiles = map[string]string{
	"go.mod":   "module example.com/probe\n\ngo 1.22\n",
	"value.go": "package probe\n\n// Value is on disk only.\nfunc Value() int { return 1 }\n",
}

// goLongLineBuffer holds a call past the long-line rule's limit of 120 columns, which the wrap breaks one argument
// per line, in a module that type-checks, as the wrap requires.
const goLongLineBuffer = "package probe\n\nfunc report(context string, description string, first int, second int, " +
	"third int) {\n}\n\nfunc Report() {\n\treport(\"context\", \"the description of what went wrong here, in a " +
	"sentence, a much much longer one than before\", 1, 2, 3333333333)\n}\n"

// goSave runs a save of buffer at name in root, with arguments or `--fix --format` when none are given, and returns
// stdout, stderr and the exit code. Every save is held to writing nothing: each file of goSaveFiles reads the same
// after it as before.
func goSave(test *testing.T, binary string, root string, name string, buffer string, arguments ...string) (
	string, string, int,
) {
	test.Helper()
	before := map[string]string{}
	for file := range goSaveFiles {
		before[file] = readForTest(test, filepath.Join(root, file))
	}
	// The path first, since the flag package stops at the first argument that is not a flag, so a case naming a path
	// beside it still names the save.
	commandLine := append([]string{"--stdin-filepath", name}, arguments...)
	if len(arguments) == 0 {
		commandLine = append(commandLine, "--fix", "--format")
	}
	saved, stderr, code := runCohereWithStdin(test, binary, root, buffer, commandLine...)
	for file, contents := range before {
		if after := readForTest(test, filepath.Join(root, file)); after != contents {
			test.Fatalf("the save wrote %s:\n%s", file, after)
		}
	}
	return saved, stderr, code
}

// goGateWrites is what the gate, `cohere --fix --format <name>`, writes for buffer at name in root, which the save
// must answer byte for byte, or the editor and the gate disagree and every save leaves work for the next run.
func goGateWrites(test *testing.T, binary string, root string, name string, buffer string) string {
	test.Helper()
	writeTree(test, root, map[string]string{name: buffer})
	output, code := runCohere(test, binary, root, "--fix", "--format", name)
	if code != 0 {
		test.Fatalf("the gate exited %d:\n%s", code, output)
	}
	return readForTest(test, filepath.Join(root, name))
}

// A Go save prints gofmt's output of the buffer, not of the file on disk, which it leaves as it was, and that output is
// what the gate writes for the same text. The disk's file differs from the buffer in its body and its comment, so an
// answer read from the disk, by any reader of the run, fails here.
func TestAGoSaveFormatsTheBufferNotTheFile(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, goSaveFiles)
	buffer := "package probe\nfunc  Value( ) int {\n  return  2 }\n"
	want, formatError := goformat.Source([]byte(buffer))
	if formatError != nil {
		test.Fatal(formatError)
	}

	saved, stderr, code := goSave(test, binary, root, "value.go", buffer)
	if code != 0 || saved != string(want) {
		test.Fatalf("exit %d, want gofmt's output of the buffer:\n%s\ngot:\n%s\nstderr:\n%s", code, want, saved, stderr)
	}
	if written := goGateWrites(test, binary, root, "value.go", buffer); written != saved {
		test.Fatalf("the save and the gate disagree\n--- gate\n%s--- save\n%s", written, saved)
	}
}

// A Go save applies the house fix a lone file can take, the long-line wrap, over a load of the buffer, and the gate
// writes the same.
func TestAGoSaveWrapsALongLine(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, goSaveFiles)

	saved, stderr, code := goSave(test, binary, root, "value.go", goLongLineBuffer)
	wrapped := "\treport(\n\t\t\"context\",\n"
	if code != 0 || !strings.Contains(saved, wrapped) {
		test.Fatalf("exit %d, want the long call wrapped (%q) in:\n%s\nstderr:\n%s", code, wrapped, saved, stderr)
	}
	if written := goGateWrites(test, binary, root, "value.go", goLongLineBuffer); written != saved {
		test.Fatalf("the save and the gate disagree\n--- gate\n%s--- save\n%s", written, saved)
	}
}

// A rename spans every file that names what it renames, so a Go save never makes one: the names the naming rules ask
// to change come back as typed, and stderr names each rename, by its rule and both spellings, as left for a run over
// the module. The names stand where the disk's file holds other text, so a name spelled from the disk reads wrong.
func TestAGoSaveNamesARenameAndLeavesIt(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, goSaveFiles)
	buffer := "package probe\n\nfunc parseURL() int { return 1 }\n\nfunc Value() int {\n\tmsg := parseURL()\n" +
		"\treturn msg\n}\n"

	saved, stderr, code := goSave(test, binary, root, "value.go", buffer)
	if code != 0 || saved != buffer {
		test.Fatalf("exit %d, want the buffer back with its names as typed:\n%s\nstderr:\n%s", code, saved, stderr)
	}
	for _, named := range []string{
		"value.go:3:6: left for a run over the module: `cohere-go/consistency-no-uppercase-initialism` asks " +
			"`parseURL` to be `parseUrl`",
		"value.go:6:2: left for a run over the module: `cohere-go/consistency-no-abbreviated-identifier` asks " +
			"`msg` to be `message`",
	} {
		if !strings.Contains(stderr, named) {
			test.Errorf("stderr does not name the rename %q:\n%s", named, stderr)
		}
	}
}

// The quiet answers of a Go save, each the buffer back as typed with exit 0, since a save is not where a file the run
// leaves alone is reported: a file git ignores, which says why on stderr in the words a run's footer uses (#08cv2mp); a
// file no package of the module holds, here one under testdata; and a buffer that does not parse.
func TestAGoSaveDeclinesWhatTheRunLeavesAlone(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, goSaveFiles)
	writeTree(test, root, map[string]string{
		".gitignore":          "ignored.go\n",
		"ignored.go":          "package probe\n",
		"testdata/fixture.go": "package fixture\n",
	})
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if output, initializeError := initialize.CombinedOutput(); initializeError != nil {
		test.Fatalf("git init: %v\n%s", initializeError, output)
	}

	unformatted := "package probe\nfunc  Ignored( ) int { return 1 }\n"
	for _, testCase := range []struct {
		name   string
		buffer string
		says   string
	}{
		{name: "ignored.go", buffer: unformatted, says: "ignored by git (.gitignore:1)"},
		{name: "testdata/fixture.go", buffer: "package fixture\nfunc  Fixture( ) {}\n", says: "no package"},
		{name: "value.go", buffer: "package probe\n\nfunc Value( int {\n", says: "it does not parse"},
	} {
		saved, stderr, code := goSave(test, binary, root, testCase.name, testCase.buffer)
		if code != 0 || saved != testCase.buffer || !strings.Contains(stderr, testCase.says) {
			test.Errorf("%s: exit %d, want the buffer back unchanged and stderr saying %q:\n%s\nstderr:\n%s",
				testCase.name, code, testCase.says, saved, stderr)
		}
	}
	if after := readForTest(test, filepath.Join(root, "ignored.go")); after != "package probe\n" {
		test.Errorf("the save wrote the ignored file:\n%s", after)
	}
}

// A Go save answers what --fix writes, as a TypeScript one does, so without --fix, or with a path beside it, there is
// no question: refused on stderr, with nothing on stdout.
func TestAGoSaveNeedsFix(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, goSaveFiles)
	for _, arguments := range [][]string{
		{"--format"},
		{"--fix", "--no-fix"},
		{"--fix", "--lint"},
		{"--fix", "other.go"},
	} {
		saved, stderr, code := goSave(test, binary, root, "value.go", "package probe\n", arguments...)
		if code == 0 || saved != "" || !strings.Contains(stderr, "--stdin-filepath answers what --fix writes") {
			test.Errorf("%v: exit %d, want the refusal on stderr and nothing on stdout:\n%s\nstderr:\n%s",
				arguments, code, saved, stderr)
		}
	}
}

// A save keeps what it checked in the findings cache under the buffer's bytes, never the disk's: here the buffer holds
// a self-assignment the file on disk does not, with the same imports, none, so only the file's bytes can key the two
// apart, and a check of the disk after the save reports nothing. Keyed by the disk's bytes, the save's entry would be
// restored by that check as the file's, a finding on a line the file on disk does not have.
func TestAGoSaveKeepsNothingUnderTheFileOnDisk(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, goSaveFiles)
	buffer := "package probe\n\n// Value is the buffer's.\nfunc Value() int {\n\tvalue := 1\n\tvalue = value\n" +
		"\treturn value\n}\n"

	if saved, stderr, code := goSave(test, binary, root, "value.go", buffer); code != 0 || saved != buffer {
		test.Fatalf("exit %d, want the buffer back as typed:\n%s\nstderr:\n%s", code, saved, stderr)
	}
	if output, code := runCohere(test, binary, root, "--no-fix"); code != 0 || strings.Contains(output, "assign") {
		test.Fatalf("a check of the file on disk after the save exited %d, want 0 and no self-assignment:\n%s",
			code, output)
	}
}
