// Package repository_corpus holds the one check of format that reads cohere's whole tree on purpose: gofmt as format
// runs it, held to go/format.Source over every Go file of the repository, which is the largest body of real Go there
// is to hand. It has no code of its own. Its walk opens every directory of the repository, and go's test cache keys a
// result on each directory's listing, so an edit anywhere in cohere, a document format never reads included, reruns
// it. It lives apart from format's own tests, which read only the fixtures they write, so that such an edit reruns
// this check alone and format's tests replay from the cache (#r439a26).
package repository_corpus

import (
	"bytes"
	goformat "go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/format"
)

// A run's gofmt prints with go/format's settings copied, a private bit among them, so it is held to go/format.Source
// byte for byte over every Go file of this repository, its testdata included, that it formats: one whose package
// clause parses and that is not generated, which formatFile skips before gofmt is asked. The run reads each file's
// bytes as the test read them and keeps what it would write, so a file it would change reads go/format.Source's
// output, one it leaves as it is reads the same as go/format.Source's, and a file that does not parse fails alike in
// both, with the same error. The planted sources the repository holds none of are format's own test
// (TestGofmtSourceIsGoFormatsOutputWherePrintingDiffers).
func TestGofmtSourceIsGoFormatsOutputForEveryGoFile(test *testing.T) {
	test.Parallel()

	root := repositoryRoot(test)
	var files []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			// TypeScript is the checkout of another project the repository links, not this repository's code; a
			// worktree's is a link, which the walk does not follow, and the shared checkout's a directory.
			if path == filepath.Join(root, "TypeScript") || entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	// Thousands of files are the repository's; a walk that found few started somewhere else.
	if len(files) < 1000 {
		test.Fatalf("found %d Go files under %s, want the repository's thousands", len(files), root)
	}

	// formatFile never hands gofmt a file whose package clause does not parse, so those are left out here too.
	originals := map[string][]byte{}
	var compared []string
	for _, file := range files {
		original, readError := os.ReadFile(file)
		if readError != nil {
			test.Fatal(readError)
		}
		headerMode := parser.PackageClauseOnly | parser.ParseComments | parser.SkipObjectResolution
		if _, headerError := parser.ParseFile(token.NewFileSet(), "", original, headerMode); headerError != nil {
			continue
		}
		originals[file] = original
		compared = append(compared, file)
	}
	result := format.Run(compared, format.Options{
		KeepOutput: true,
		Held: func(file string) ([]byte, bool) {
			original, held := originals[file]
			return original, held
		},
	})

	answered := map[string]bool{}
	for _, changed := range result.Changed {
		answered[changed.File] = true
		want, wantError := goformat.Source(originals[changed.File])
		if wantError != nil {
			test.Errorf("%s: the run formatted it, and go/format.Source failed with %v", changed.File, wantError)
			continue
		}
		if !bytes.Equal(changed.Output, want) {
			test.Errorf("%s: the run's gofmt differs from go/format.Source:\n%s\nwant:\n%s", changed.File,
				changed.Output, want)
		}
	}
	failedAlike := 0
	for _, notFormatted := range result.NotFormatted {
		answered[notFormatted.File] = true
		_, wantError := goformat.Source(originals[notFormatted.File])
		if wantError == nil {
			test.Errorf("%s: the run did not format it, %s, and go/format.Source formats it", notFormatted.File,
				notFormatted.Reason)
			continue
		}
		// formatFailureReason's two spellings, each ending in gofmt's own error.
		if notFormatted.Reason != "it does not parse: "+wantError.Error() &&
			notFormatted.Reason != "gofmt could not print it: "+wantError.Error() {
			test.Errorf("%s: the run did not format it, %s, and go/format.Source failed with %v", notFormatted.File,
				notFormatted.Reason, wantError)
			continue
		}
		failedAlike++
	}
	for _, generated := range result.Generated {
		answered[generated] = true
	}
	for _, file := range compared {
		if answered[file] {
			continue
		}
		want, wantError := goformat.Source(originals[file])
		if wantError != nil {
			test.Errorf("%s: the run left it as it is, and go/format.Source failed with %v", file, wantError)
			continue
		}
		if !bytes.Equal(want, originals[file]) {
			test.Errorf("%s: the run left it as it is, and go/format.Source changes it:\n%s", file, want)
		}
	}
	test.Logf("compared %d of %d files, %d of them failing alike, %d skipped as generated",
		len(compared)-len(result.Generated), len(files), failedAlike, len(result.Generated))
}

// repositoryRoot is the directory holding the repository's go.work, found from the package's own directory, where a
// test runs, upward.
func repositoryRoot(test *testing.T) string {
	test.Helper()
	directory, directoryError := os.Getwd()
	if directoryError != nil {
		test.Fatal(directoryError)
	}
	for {
		if _, statError := os.Stat(filepath.Join(directory, "go.work")); statError == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			test.Fatal("no go.work above the package's directory")
		}
		directory = parent
	}
}
