package gitignore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree makes files under root, creating their directories. A name ending in "/" is a directory.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// decide asks the matcher about one path, entering its directory first.
func decide(t *testing.T, matcher *Matcher, relativePath string, isDirectory bool) (bool, Source) {
	t.Helper()
	directory := filepath.ToSlash(filepath.Dir(relativePath))
	if directory == "." {
		directory = ""
	}
	scope, err := matcher.Enter(directory)
	if err != nil {
		t.Fatalf("entering %q: %v", directory, err)
	}
	return scope.Ignored(relativePath, isDirectory)
}

// Each rule gitignore(5) states, decided on one small tree, with the deciding line named.
func TestTheMatcherFollowsGitignoreSemantics(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".git/info/exclude": "# a comment\nper-repo\nkept-by-gitignore\n",
		".gitignore": strings.Join([]string{
			"*.log",
			"!keep.log",
			"/anchored",
			"build/",
			"docs/generated",
			"\\#literal",
			"\\!bang",
			"trailing   ",
			"escaped\\ ",
			"**/deep/x",
			"vendor/",
			"!vendor/kept",
			"!kept-by-gitignore",
		}, "\n") + "\n",
		"sub/.gitignore":       "!*.log\nlocal\n/rooted\n",
		"sub/inner/.gitignore": "*.tmp\n",
		"build/":               "",
		"sub/build/":           "",
		"vendor/kept":          "",
		"docs/":                "",
		"sub/inner/":           "",
	})
	matcher, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path        string
		directory   bool
		wantIgnored bool
		wantSource  string
	}{
		{"app.log", false, true, ".gitignore:1:*.log"},
		{"keep.log", false, false, ".gitignore:2:!keep.log"},
		{"sub/app.log", false, false, "sub/.gitignore:1:!*.log"},
		{"anchored", false, true, ".gitignore:3:/anchored"},
		{"sub/anchored", false, false, ""},
		{"build", true, true, ".gitignore:4:build/"},
		{"build", false, false, ""},
		{"sub/build", true, true, ".gitignore:4:build/"},
		{"docs/generated", false, true, ".gitignore:5:docs/generated"},
		{"sub/docs/generated", false, false, ""},
		{"#literal", false, true, ".gitignore:6:\\#literal"},
		{"!bang", false, true, ".gitignore:7:\\!bang"},
		{"trailing", false, true, ".gitignore:8:trailing"},
		{"escaped ", false, true, ".gitignore:9:escaped\\ "},
		{"deep/x", false, true, ".gitignore:10:**/deep/x"},
		{"a/b/deep/x", false, true, ".gitignore:10:**/deep/x"},
		{"vendor/kept", false, true, ".gitignore:11:vendor/"},
		{"sub/local", false, true, "sub/.gitignore:2:local"},
		{"sub/inner/local", false, true, "sub/.gitignore:2:local"},
		{"sub/rooted", false, true, "sub/.gitignore:3:/rooted"},
		{"sub/inner/rooted", false, false, ""},
		{"sub/inner/a.tmp", false, true, "sub/inner/.gitignore:1:*.tmp"},
		{"sub/a.tmp", false, false, ""},
		{"per-repo", false, true, ".git/info/exclude:2:per-repo"},
		{"sub/per-repo", false, true, ".git/info/exclude:2:per-repo"},
		{"kept-by-gitignore", false, false, ".gitignore:13:!kept-by-gitignore"},
	}
	for _, testCase := range cases {
		ignored, source := decide(t, matcher, testCase.path, testCase.directory)
		gotSource := ""
		if !source.IsZero() {
			gotSource = source.String()
		}
		if ignored != testCase.wantIgnored || gotSource != testCase.wantSource {
			t.Errorf("%s (directory %v): got %v by %q, want %v by %q",
				testCase.path, testCase.directory, ignored, gotSource, testCase.wantIgnored, testCase.wantSource)
		}
	}
}

// A file's byte order mark and each line's carriage return are not part of a pattern.
func TestAByteOrderMarkAndCarriageReturnsAreNotPatternText(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{".gitignore": "\xEF\xBB\xBFfirst\r\nsecond\r\n"})
	matcher, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second"} {
		if ignored, _ := matcher.Ignored(name, false); !ignored {
			t.Errorf("%s is not ignored", name)
		}
	}
}

// What cannot be read faithfully is refused by name, never skipped.
func TestAnIgnoreFileThatCannotBeReadIsRefused(t *testing.T) {
	t.Parallel()
	t.Run("a symbolic link in the tree", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeTree(t, root, map[string]string{"patterns": "x\n", "sub/": ""})
		if err := os.Symlink("../patterns", filepath.Join(root, "sub", ".gitignore")); err != nil {
			t.Fatal(err)
		}
		matcher, err := New(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := matcher.Enter("sub"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("entering a directory whose .gitignore is a link: %v", err)
		}
	})
	t.Run("a NUL byte", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeTree(t, root, map[string]string{".gitignore": "a\x00b\n"})
		if _, err := New(root); err == nil || !strings.Contains(err.Error(), ".gitignore:1") {
			t.Fatalf("a NUL byte: %v", err)
		}
	})
	t.Run("a nested repository", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeTree(t, root, map[string]string{"nested/.git/HEAD": "ref: refs/heads/main\n"})
		matcher, err := New(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := matcher.Enter("nested"); !errors.Is(err, ErrNestedRepository) {
			t.Fatalf("entering a nested repository: %v", err)
		}
	})
}

// Asking through the wrong directory's matcher would silently leave out the ignore files between.
func TestAskingOutsideTheMatchersDirectoryPanics(t *testing.T) {
	t.Parallel()
	matcher, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("asking about a/b through the root's matcher did not panic")
		}
	}()
	matcher.Ignored("a/b", false)
}
