package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A run replays over an index git rewrote without changing what it decides, and runs again over one that newly
// tracks a file the ignore rules exclude (#m1daxxk). Since #08cv2mp a tracked file is never ignored, so the index is
// an input of the format walk; keyed on its bytes, a `git status` that refreshed one entry's stat data kept every
// later run from replaying.
//
// scratch.ts sits outside the program and is excluded by `.gitignore`, so the format walk passes it by; written
// unformatted, the walk reports it once `git add -f` tracks it. README.txt is tracked and read by no check, so
// touching it moves only the index, which `git status` rewrites.
func TestARunReplaysThroughAGitStatusAndNotThroughAGitAddForce(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	writeTree(test, root, map[string]string{
		"tsconfig.json": `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext",` +
			`"moduleResolution":"bundler"},"include":["source"]}`,
		"CohereSettings.json": `{"rules":{"no-debugger":"error"}}` + "\n",
		".gitignore":          ".cache/\nnode_modules/\nscratch.ts\n",
		"source/index.ts":     "export const value = 1;\n",
		"scratch.ts":          "export   const   scratch=1\n",
		"README.txt":          "Read me.\n",
	})
	git := func(arguments ...string) {
		test.Helper()
		command := exec.Command("git", append([]string{"-c", "user.email=fixture@example.com", "-c",
			"user.name=fixture", "-c", "core.excludesFile=/dev/null"}, arguments...)...)
		command.Dir = root
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if output, outputError := command.CombinedOutput(); outputError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, outputError, output)
		}
	}
	git("init", "--quiet")
	git("add", ".gitignore", "tsconfig.json", "CohereSettings.json", "source", "README.txt")
	git("commit", "--quiet", "-m", "project")
	index := filepath.Join(root, ".git", "index")
	run := func() string {
		test.Helper()
		output, _ := runCohere(test, binary, root, "--no-fix")
		return output
	}

	run()
	if replayed := run(); !strings.HasPrefix(replayed, "cached: ") {
		test.Fatalf("an unchanged tree did not replay, so nothing below proves anything:\n%s", replayed)
	}

	before, readError := os.ReadFile(index)
	if readError != nil {
		test.Fatal(readError)
	}
	later := time.Now().Add(2 * time.Second)
	if changeTimesError := os.Chtimes(filepath.Join(root, "README.txt"), later, later); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
	git("status", "--porcelain")
	after, readError := os.ReadFile(index)
	if readError != nil {
		test.Fatal(readError)
	}
	if bytes.Equal(before, after) {
		test.Fatal("`git status` left the index as it was, so this case proves nothing")
	}
	if replayed := run(); !strings.HasPrefix(replayed, "cached: ") {
		test.Errorf("a `git status` that rewrote the index and decided nothing new kept the run from replaying:\n%s",
			replayed)
	}

	git("add", "-f", "scratch.ts")
	warm := run()
	if strings.HasPrefix(warm, "cached: ") {
		test.Fatalf("a run replayed over an index that newly tracks scratch.ts, which the walk now formats:\n%s", warm)
	}
	cold, _ := runCohere(test, binary, root, "--no-fix", "--no-cache")
	for name, output := range map[string]string{"warm": warm, "cold": cold} {
		if !strings.Contains(output, "scratch.ts") {
			test.Errorf("the %s run after `git add -f` does not report scratch.ts, now tracked and unformatted:\n%s",
				name, output)
		}
	}
}
