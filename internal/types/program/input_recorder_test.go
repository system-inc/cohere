package program_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

// A path the compiler asked about with a typed question that answered no is still present if it is on
// disk in another form, and only a path genuinely not there is recorded absent.
//
// Module resolution asks whether `./Graph.css` is a directory, the answer is no, and the file is right
// there. Recording that no as absence made an untouched ahra tree miss on a four-month-old stylesheet,
// so the cache could never hit. Built through the real Build so the probe reaches the recorder the way
// the compiler sends it, not the way a test imagines it does.
func TestInputRecorderRecordsAFileProbedAsADirectoryAsPresent(t *testing.T) {
	root := t.TempDir()
	write := func(path, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"strict":true,"noEmit":true,"module":"esnext","moduleResolution":"bundler"},"include":["source"]}`)
	write(filepath.Join(root, "source", "index.ts"), "import './style.css';\nimport { a } from './missing';\nexport const b = a;\n")
	write(filepath.Join(root, "source", "style.css"), "body {}\n")

	recorder := program.NewInputRecorder()
	if _, err := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json"), CurrentDirectory: root, Inputs: recorder}); err != nil {
		t.Fatalf("build: %v", err)
	}
	present, absent := recorder.Inputs()

	style := filepath.Join(root, "source", "style.css")
	if slices.Contains(absent, style) {
		t.Errorf("%s is on disk and was recorded absent, so every check would report it appearing", style)
	}
	for _, path := range absent {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s is on disk and was recorded absent", path)
		}
	}

	// The control: the recorder does record absence, or the loop above passes on an empty list.
	if len(absent) == 0 {
		t.Fatal("nothing was recorded absent, though ./missing was looked for and does not exist: the check above proved nothing")
	}
	if !slices.Contains(present, filepath.Join(root, "source", "index.ts")) {
		t.Error("the source file the program read was not recorded present")
	}

	// And the whole recording replays as a hit on the untouched tree, which is the property all of
	// this serves.
	cache, err := program.RecordRunCache("k", present, nil, absent, nil, 0, time.Time{})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := cache.Check("k"); err != nil {
		t.Fatalf("the untouched tree missed: %v", err)
	}
}
