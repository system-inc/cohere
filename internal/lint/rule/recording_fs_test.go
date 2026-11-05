package rule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs/osvfs"
)

// A recording file system notes every path it is asked about, present or absent, and refuses to write.
// The absences are the half a cache most easily forgets: a design system that looked for an entry
// point and missed it changes when that file is created (#pyhm2t2).
func TestARecordingFileSystemNotesEveryPathAndRefusesWrites(test *testing.T) {
	test.Parallel()
	root, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatal(err)
	}
	project := filepath.ToSlash(filepath.Join(root, "project"))
	if createDirectoryError := os.MkdirAll(project+"/source", 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(project+"/source/theme.css", []byte("@import 'tailwindcss';"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	recording := NewRecordingFs(osvfs.FS())

	recording.FileExists(tspath.RootedFilePath(project + "/source/theme.css"))
	recording.FileExists(tspath.RootedFilePath(project + "/app/globals.css"))
	recording.ReadFile(tspath.RootedFilePath(project + "/source/theme.css"))
	recording.Stat(tspath.RootedPath(project + "/missing.css"))
	recording.DirectoryExists(tspath.RootedDirectoryPath(project + "/source"))

	got := []string{}
	for _, read := range recording.Reads() {
		state := "absent"
		if read.Present {
			state = "present"
		}
		got = append(got, strings.TrimPrefix(read.Path, root)+" "+state)
	}
	want := []string{
		"/project/app/globals.css absent",
		"/project/missing.css absent",
		"/project/source present",
		"/project/source/theme.css present",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		test.Errorf("recorded:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A present file keeps the size and modification time it had when read, so a cache hashing it
	// later can tell it moved in between; an absent one has neither.
	info, err := os.Stat(project + "/source/theme.css")
	if err != nil {
		test.Fatal(err)
	}
	for _, read := range recording.Reads() {
		switch {
		case strings.HasSuffix(read.Path, "/source/theme.css"):
			if read.Size != info.Size() || read.ModifiedNanoseconds != info.ModTime().UnixNano() {
				test.Errorf("the stylesheet recorded size %d and time %d, want %d and %d",
					read.Size, read.ModifiedNanoseconds, info.Size(), info.ModTime().UnixNano())
			}
		case !read.Present:
			if read.Size != 0 || read.ModifiedNanoseconds != 0 {
				test.Errorf("absent %s recorded a size or a time", read.Path)
			}
		}
	}

	for name, write := range map[string]func(){
		// Ignored: the call panics before returning an error, which the deferred recover below checks.
		"WriteFile": func() { recording.WriteFile(tspath.RootedFilePath(project+"/out.css"), "") },
		// Ignored: the call panics before returning an error.
		"AppendFile": func() { recording.AppendFile(tspath.RootedFilePath(project+"/out.css"), "") },
		// Ignored: the call panics before returning an error.
		"Remove": func() { recording.Remove(tspath.RootedPath(project + "/source/theme.css")) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					test.Errorf("%s was allowed through a recording file system", name)
				}
			}()
			write()
		}()
	}
}
