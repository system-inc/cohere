package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// An installed declaration replaced the way a package manager replaces one, a new file renamed into place
// with the same size and the old modification time, reaches the next run through the content pack: a
// warm run reports what a cold one does (#j9d5nm6).
//
// `--lint` keeps the run cache out of it, since that run is never recorded or replayed, so the pack is
// the only thing between the disk and the compiler here. The rule is type-aware, so the declaration's
// bytes decide the finding.
func TestTheContentPackServesAReplacedDeclarationFresh(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("the content pack serves nothing on this platform")
	}
	binary := buildCohere(t)
	root := t.TempDir()
	write := func(name string, contents string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The same size both ways, so only the bytes differ.
	const withoutFinding = "export type Event = number;\n"
	const withFinding = "export type Event = 'abcd';\n"
	write("tsconfig.json", `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler"},"include":["source"]}`)
	write("CohereSettings.json", `{"rules":{"@typescript-eslint/no-redundant-type-constituents":"error"}}`)
	write("package.json", `{"name":"fixture","private":true,"type":"module"}`)
	write(".gitignore", ".cache/\nnode_modules/\n")
	write("source/Use.ts", "import type { Event } from 'events-package';\nexport type Either = string | Event;\n")
	write("node_modules/events-package/package.json", `{"name":"events-package","types":"index.d.ts"}`)
	declaration := filepath.Join(root, "node_modules", "events-package", "index.d.ts")
	write("node_modules/events-package/index.d.ts", withoutFinding)

	finding := regexp.MustCompile(`(?m)^\S+:\d+:\d+ - .*$`)
	findings := func(output string) []string { return finding.FindAllString(output, -1) }
	run := func(arguments ...string) string {
		t.Helper()
		output, _ := runCohere(t, binary, root, append([]string{"--no-fix", "--lint"}, arguments...)...)
		return output
	}

	run()
	if _, err := os.Stat(filepath.Join(root, ".cache", "cohere", "contents.index")); err != nil {
		t.Fatalf("the first run wrote no content pack, so the runs below prove nothing: %v", err)
	}
	for _, step := range []struct {
		replacement string
		want        int
	}{{withFinding, 1}, {withoutFinding, 0}} {
		information, err := os.Stat(declaration)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		staged := declaration + ".staged"
		if err := os.WriteFile(staged, []byte(step.replacement), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(staged, information.ModTime(), information.ModTime()); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(staged, declaration); err != nil {
			t.Fatal(err)
		}
		warm, cold := run(), run("--no-cache")
		if strings.Join(findings(warm), "\n") != strings.Join(findings(cold), "\n") || len(findings(cold)) != step.want {
			t.Fatalf("after the declaration became %q, warm reports %d findings and cold %d, want %d\n--- warm\n%s\n--- cold\n%s",
				strings.TrimSpace(step.replacement), len(findings(warm)), len(findings(cold)), step.want, warm, cold)
		}
	}
}
