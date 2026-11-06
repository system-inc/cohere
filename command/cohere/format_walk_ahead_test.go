package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/format/native"
)

// countingEngine is a format engine whose walks are counted, so a test can tell a walk taken from ahead from
// one made on the spot.
type countingEngine struct {
	*native.Resolving
	walks int
}

func (engine *countingEngine) Enumerate(root string) (format_files.Enumeration, error) {
	engine.walks++
	return engine.Resolving.Enumerate(root)
}

// Not parallel: it sets aheadFormatWalk, the package-level walk the fix phase takes, which a run in this
// package beside it could take or replace.
//
// The format walk begun beside the graph build is the walk the fix phase would have made (#679s763): taken
// for the same engine and root, it equals a walk made on the spot, and it is taken once. A walk of another
// root, or by another engine, is made on the spot.
func TestTheFormatWalkBegunAheadIsTheOneTheFixPhaseWouldMake(test *testing.T) {
	root := test.TempDir()
	for name, contents := range map[string]string{
		"a.ts":          "export const a = 1;\n",
		"notes.md":      "# Notes\n",
		"ignored/b.ts":  "export const b = 2;\n",
		".gitignore":    "ignored/\n",
		"styles/c.css":  "a { color: red; }\n",
		"data/d.json":   "{}\n",
		"other/e.ts":    "export const e = 3;\n",
		"other/f.yaml":  "f: 1\n",
		"other/g.txt":   "not formatted\n",
		"nested/h.tsx":  "export const h = <div />;\n",
		"nested/i.scss": "a { b: c; }\n",
	} {
		path := filepath.Join(root, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	resolving, newResolvingError := native.NewResolving(root)
	if newResolvingError != nil {
		test.Fatal(newResolvingError)
	}
	test.Cleanup(func() { aheadFormatWalk = nil })

	direct, enumerateError := resolving.Enumerate(root)
	if enumerateError != nil {
		test.Fatal(enumerateError)
	}
	if len(direct.Files) < 5 {
		test.Fatalf("the fixture's walk found %d files, too few for a match to prove anything", len(direct.Files))
	}

	engine := &countingEngine{Resolving: resolving}
	startFormatWalkAhead(engine, root, nil)
	taken, enumerateFormatTreeError := enumerateFormatTree(engine, root)
	if enumerateFormatTreeError != nil {
		test.Fatal(enumerateFormatTreeError)
	}
	if engine.walks != 1 {
		test.Fatalf("the engine walked %d times for one walk begun ahead and taken", engine.walks)
	}
	if !reflect.DeepEqual(taken, direct) {
		test.Fatalf("the walk begun ahead differs from a direct walk\nahead:  %+v\ndirect: %+v", taken, direct)
	}
	if _, enumerateFormatTreeError := enumerateFormatTree(
		engine,
		root,
	); enumerateFormatTreeError != nil || engine.walks != 2 {
		test.Errorf("a second call took the same walk again (%d walks)", engine.walks)
	}

	startFormatWalkAhead(engine, root, nil)
	// Waited for, so the counts below never race the walk's goroutine.
	<-aheadFormatWalk.done
	other := &countingEngine{Resolving: resolving}
	if _, enumerateFormatTreeError := enumerateFormatTree(
		other,
		root,
	); enumerateFormatTreeError != nil || other.walks != 1 {
		test.Errorf("another engine took the walk begun for this one (%d walks of its own)", other.walks)
	}
	if _, enumerateFormatTreeError := enumerateFormatTree(
		engine,
		filepath.Join(root, "other"),
	); enumerateFormatTreeError != nil || engine.walks != 4 {
		test.Errorf("a walk of another root was taken from the walk begun ahead (%d walks)", engine.walks)
	}
}
