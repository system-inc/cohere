package rule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/vfs/osvfs"
)

// A recording file system notes every path it is asked about, present or absent, and refuses to write.
// The absences are the half a cache most easily forgets: a design system that looked for an entry
// point and missed it changes when that file is created (#pyhm2t2).
func TestARecordingFileSystemNotesEveryPathAndRefusesWrites(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.ToSlash(filepath.Join(root, "project"))
	if err := os.MkdirAll(project+"/source", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project+"/source/theme.css", []byte("@import 'tailwindcss';"), 0o644); err != nil {
		t.Fatal(err)
	}
	recording := NewRecordingFS(osvfs.FS())

	recording.FileExists(project + "/source/theme.css")
	recording.FileExists(project + "/app/globals.css")
	recording.ReadFile(project + "/source/theme.css")
	recording.Stat(project + "/missing.css")
	recording.DirectoryExists(project + "/source")

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
		t.Errorf("recorded:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	for name, write := range map[string]func(){
		"WriteFile":  func() { recording.WriteFile(project+"/out.css", "") },
		"AppendFile": func() { recording.AppendFile(project+"/out.css", "") },
		"Remove":     func() { recording.Remove(project + "/source/theme.css") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s was allowed through a recording file system", name)
				}
			}()
			write()
		}()
	}
}
