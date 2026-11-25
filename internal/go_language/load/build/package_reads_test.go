package build

import (
	"bytes"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// unlikeBeyondTags are the fields of a build context sameBeyondTags compares, each by name with a change that makes a
// context unlike build.Default in that field alone.
var unlikeBeyondTags = map[string]func(context *build.Context){
	"GOROOT":        func(context *build.Context) { context.GOROOT = filepath.Join(context.GOROOT, "elsewhere") },
	"GOPATH":        func(context *build.Context) { context.GOPATH = filepath.Join(context.GOPATH, "elsewhere") },
	"Dir":           func(context *build.Context) { context.Dir = filepath.Join(context.Dir, "elsewhere") },
	"UseAllFiles":   func(context *build.Context) { context.UseAllFiles = !context.UseAllFiles },
	"Compiler":      func(context *build.Context) { context.Compiler = "gccgo" },
	"InstallSuffix": func(context *build.Context) { context.InstallSuffix = "race" },
}

// Two build contexts unlike in any field a Package reads beyond what matchTag answers are never alike, whichever of the
// two read the directory and whichever takes the read, so a read is never taken across them (#y8cf1wf). Contexts
// unlike only in what matchTag answers are alike here, which the tags then decide (selectsAlike).
func TestEveryFieldBeyondTheTagsKeepsAReadFromBeingTaken(test *testing.T) {
	test.Parallel()
	base := build.Default
	for field, unlike := range unlikeBeyondTags {
		other := base
		unlike(&other)
		if sameBeyondTags(base, other) {
			test.Errorf("%s: a read made under the base context is taken under one unlike it", field)
		}
		if sameBeyondTags(other, base) {
			test.Errorf("%s: a read made under the unlike context is taken under the base one", field)
		}
	}
	tagsOnly := base
	tagsOnly.GOOS, tagsOnly.GOARCH, tagsOnly.CgoEnabled = "windows", "amd64", !base.CgoEnabled
	tagsOnly.BuildTags = []string{"race"}
	if !sameBeyondTags(base, tagsOnly) || !sameBeyondTags(base, base) {
		test.Error("contexts unlike only in what matchTag answers are refused before their tags are asked")
	}
}

// A file the run held when a build read its directory, and holds no longer when a later build lists it, is read again
// from disk by that build, never taken from the read of the bytes it held (#y8cf1wf). cohere-go drops a file's hold
// when a write's reread fails (pipeline.reread); a write it rereads holds the new bytes instead, which the bytes'
// comparison refuses. A build holding the same bytes takes the read, so the refusal is no read that is never taken.
func TestAFileNoLongerHeldIsReadAgainFromDisk(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	plain := filepath.Join(directory, "plain.go")
	// Two imports, one on disk and one in the held bytes, so the read's imports say which bytes it read.
	onDisk := []byte("package plain\n\nimport _ \"text/scanner\"\n")
	if writeError := os.WriteFile(plain, onDisk, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	heldBytes := []byte("package plain\n\nimport _ \"text/template/parse\"\n")
	holding := func(path string) ([]byte, bool) { return heldBytes, path == plain }
	holdingNothing := func(string) ([]byte, bool) { return nil, false }
	context := build.Default
	context.CgoEnabled = false
	reads := NewPackageReads()
	headers := NewHeaders()
	for _, step := range []struct {
		name      string
		held      func(path string) ([]byte, bool)
		taken     bool
		importing string
	}{
		{name: "held, read first", held: holding, taken: false, importing: "text/template/parse"},
		{name: "held alike, taken", held: holding, taken: true, importing: "text/template/parse"},
		{name: "no longer held", held: holdingNothing, taken: false, importing: "text/scanner"},
	} {
		// The build opens a file from the bytes held holds, as the lister's context opens it (load's Held).
		context.OpenFile = openingHeld(step.held)
		built, taken, importError := reads.ImportDirectory(context, directory, step.held, headers)
		if importError != nil {
			test.Fatalf("%s: %v", step.name, importError)
		}
		if taken != step.taken || !slices.Equal(built.Imports, []string{step.importing}) {
			test.Errorf("%s: taken %t with imports %v, want taken %t importing %s", step.name, taken, built.Imports,
				step.taken, step.importing)
		}
	}
	// The last read is go/build's own of the disk.
	context.OpenFile = openingHeld(holdingNothing)
	built, _, importError := reads.ImportDirectory(context, directory, holdingNothing, headers)
	expected, expectedError := context.ImportDir(directory, 0)
	if importError != nil || expectedError != nil {
		test.Fatalf("read with %v, go/build with %v", importError, expectedError)
	}
	if !reflect.DeepEqual(built, expected) {
		test.Errorf("with plain.go no longer held, read\n%+v\nwhere go/build reads\n%+v", built, expected)
	}
}

// openingHeld is a build context's OpenFile over held: a held file is opened from its bytes, any other from disk.
func openingHeld(held func(path string) ([]byte, bool)) func(path string) (io.ReadCloser, error) {
	return func(path string) (io.ReadCloser, error) {
		if contents, isHeld := held(path); isHeld {
			return io.NopCloser(bytes.NewReader(contents)), nil
		}
		return os.Open(path)
	}
}
