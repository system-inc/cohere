//go:build unix

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// fixtureGenerator stands in for the docs data generator: docs/data/out.txt is docs/one.txt and docs/two.txt in
// capitals, and -check exits 1 when it isn't. An unstable one also appends the count of runs it has made, so no
// two runs write the same file.
func fixtureGenerator(unstable bool) string {
	counted := ""
	if unstable {
		counted = "\n\tcount, _ := os.ReadFile(\"docs/data/count\")\n\tcount = append(count, 'x')\n" +
			"\t_ = os.WriteFile(\"docs/data/count\", count, 0o644)\n\tout += string(count)"
	}
	return "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n)\n\nfunc main() {\n" +
		"\tone, _ := os.ReadFile(\"docs/one.txt\")\n\ttwo, _ := os.ReadFile(\"docs/two.txt\")\n" +
		"\tout := strings.ToUpper(string(one) + string(two))\n" +
		"\tfor _, argument := range os.Args[1:] {\n\t\tif argument == \"-check\" {\n" +
		"\t\t\tcurrent, _ := os.ReadFile(\"docs/data/out.txt\")\n\t\t\tif string(current) != out {\n" +
		"\t\t\t\tfmt.Println(\"stale: docs/data/out.txt\")\n\t\t\t\tos.Exit(1)\n\t\t\t}\n\t\t\treturn\n\t\t}\n\t}\n" +
		"\t_ = os.MkdirAll(\"docs/data\", 0o755)" + counted + "\n" +
		"\t_ = os.WriteFile(\"docs/data/out.txt\", []byte(out), 0o644)\n}\n"
}

const docsGeneratorModule = "module example.com/root\n\ngo 1.21\n"

var docsDataGeneratorPath = strings.TrimPrefix(docsDataGenerator, "./") + "/main.go"

// generatedFixture is a land fixture whose main holds the generator and its sources, a's worktree merged with it,
// with the real go on PATH.
func generatedFixture(test *testing.T, unstable bool) (*landFixture, string) {
	test.Helper()
	fixture := newLandFixture(test)
	fixture.environment = append(fixture.environment, "PATH="+os.Getenv("PATH"))
	writeFiles(test, fixture.repository, map[string]string{
		"go.mod":              docsGeneratorModule,
		docsDataGeneratorPath: fixtureGenerator(unstable),
		"docs/one.txt":        "a\n",
		"docs/two.txt":        "b\n",
		"docs/data/out.txt":   "A\nB\n",
	})
	fixture.git(fixture.repository, "add", "go.mod", "internal", "docs")
	fixture.git(fixture.repository, "commit", "-q", "-m", "docs: a generator and its data (#"+fixtureTask+")")
	worktreeA := fixture.worktree("a")
	fixture.git(worktreeA, "merge", "-q", "--no-edit", "main")
	return fixture, worktreeA
}

// commitOn writes files in a directory and commits them with a house subject.
func (fixture *landFixture) commitOn(directory string, files map[string]string, subject string) {
	fixture.test.Helper()
	writeFiles(fixture.test, directory, files)
	for name := range files {
		fixture.git(directory, "add", name)
	}
	fixture.git(directory, "commit", "-q", "-m", subject+" (#"+fixtureTask+")")
}

func (fixture *landFixture) landWithMessage(worktree string) (string, error) {
	fixture.test.Helper()
	landing := exec.Command(fixture.wrapper, "land", "--message", "docs changed")
	landing.Dir, landing.Env = worktree, fixture.environment
	output, outputError := landing.CombinedOutput()
	return string(output), outputError
}

// A merge with main that conflicts only in docs/data, which the docs data generator writes whole, is resolved by
// regenerating it from the merged sources (#y84h1te), not refused to the back of the land line: a changed docs/one
// and main changed docs/two, and each regenerated out.txt, whose adjacent lines conflict. The landing holds both
// sources and the data they make, and its body says how the conflict was resolved.
func TestALandingConflictingOnlyInGeneratedFilesIsRegenerated(test *testing.T) {
	test.Parallel()
	fixture, worktreeA := generatedFixture(test, false)
	fixture.commitOn(worktreeA, map[string]string{"docs/one.txt": "x\n", "docs/data/out.txt": "X\nB\n"},
		"docs: one reads x")
	fixture.commitOn(fixture.repository, map[string]string{"docs/two.txt": "y\n", "docs/data/out.txt": "A\nY\n"},
		"docs: two reads y")
	worktrees := strings.Count(fixture.git(fixture.repository, "worktree", "list"), "\n")

	output, landError := fixture.landWithMessage(worktreeA)
	if landError != nil {
		test.Fatalf("the landing failed (%v):\n%s", landError, output)
	}
	for path, want := range map[string]string{"docs/one.txt": "x", "docs/two.txt": "y", "docs/data/out.txt": "X\nY"} {
		if got := fixture.git(fixture.repository, "show", "main:"+path); got != want {
			test.Errorf("main's %s reads %q, want %q", path, got, want)
		}
	}
	body := fixture.git(fixture.repository, "log", "-1", "--format=%b", "main")
	if !strings.Contains(body, "Its conflicts with main were all in docs/data/") ||
		!strings.Contains(body, "a second run changed nothing: docs/data/out.txt") {
		test.Errorf("the landing's body does not say how its conflict was resolved:\n%s", body)
	}
	if after := fixture.git(fixture.repository, "worktree", "list"); strings.Count(after, "\n") != worktrees {
		test.Errorf("the worktree the conflict was resolved in was left behind:\n%s", after)
	}
}

// A conflict in any file the generator doesn't write is refused, naming it, and so is a generator that never
// reaches a fixed point: in both, main and the worktree are as they were, and no scratch worktree is left.
func TestALandingConflictingInAHandWrittenFileOrUnstableDataIsRefused(test *testing.T) {
	test.Parallel()
	for _, refusal := range []struct {
		name     string
		unstable bool
		ours     map[string]string
		theirs   map[string]string
		want     string
	}{
		{
			name:   "a hand-written file",
			ours:   map[string]string{"docs/one.txt": "x\n", "docs/data/out.txt": "X\nB\n"},
			theirs: map[string]string{"docs/one.txt": "z\n", "docs/data/out.txt": "Z\nB\n"},
			want:   "these are written by hand: docs/one.txt",
		},
		{
			name:     "a generator with no fixed point",
			unstable: true,
			ours:     map[string]string{"docs/one.txt": "x\n", "docs/data/out.txt": "X\nB\n"},
			theirs:   map[string]string{"docs/two.txt": "y\n", "docs/data/out.txt": "A\nY\n"},
			want:     "did not reach a fixed point",
		},
	} {
		test.Run(refusal.name, func(test *testing.T) {
			test.Parallel()
			fixture, worktreeA := generatedFixture(test, refusal.unstable)
			fixture.commitOn(worktreeA, refusal.ours, "docs: ours")
			fixture.commitOn(fixture.repository, refusal.theirs, "docs: theirs")
			mainBefore := fixture.git(fixture.repository, "rev-parse", "main")
			headBefore := fixture.git(worktreeA, "rev-parse", "HEAD")
			worktrees := fixture.git(fixture.repository, "worktree", "list")
			output, landError := fixture.landWithMessage(worktreeA)
			if landError == nil || !strings.Contains(output, refusal.want) {
				test.Fatalf("the landing was not refused with %q (%v):\n%s", refusal.want, landError, output)
			}
			if after := fixture.git(fixture.repository, "rev-parse", "main"); after != mainBefore {
				test.Errorf("main moved on a refused landing")
			}
			if after := fixture.git(worktreeA, "rev-parse", "HEAD"); after != headBefore {
				test.Errorf("the worktree moved on a refused landing")
			}
			if after := fixture.git(fixture.repository, "worktree", "list"); after != worktrees {
				test.Errorf("a scratch worktree was left behind:\n%s", after)
			}
		})
	}
}
