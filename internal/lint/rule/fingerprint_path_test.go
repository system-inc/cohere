package rule

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// directoryProgram is a Program that answers only what FingerprintPath reads.
type directoryProgram struct {
	Program
	directory     string
	caseSensitive bool
}

func (program directoryProgram) GetCurrentDirectory() string     { return program.directory }
func (program directoryProgram) UseCaseSensitiveFileNames() bool { return program.caseSensitive }

// tspathName is FingerprintPath's answer before it learned to slice: tspath's general relative path.
func tspathName(program directoryProgram, path string) string {
	return tspath.GetRelativePathFromDirectory(program.directory, path, caseSensitivity(program.caseSensitive))
}

// writtenTree is the tree TestFingerprintPathSlicesWhatTspathWouldJoin writes and walks, by slash path below its
// root, a directory where the path ends in a slash. The test walked every file and directory of this module, and
// Go's test cache keys a result on every directory listing a test opens, so an edit anywhere in cohere reran rule's
// tests on every landing (#0zdyk51). On 2026-11-27 that walk compared 35,372 names and sliced 35,370, all but the
// root's own two; its names were nested directories, mixed case, dot files and dot directories, and paths 13 deep
// and 193 bytes long, none with a space or a byte past ASCII. This holds every shape it reached, and the two it
// never met.
var writtenTree = []string{
	"source/Paths.ts",
	"source/translations/",
	"source/archive.tar.gz",
	"Libraries/Structure/Source/Utilities/ColorUtilities.ts",
	"UPPER/CASE.TS",
	".github/workflows/check.yml",
	".gitignore",
	"..config",
	"empty/",
	"documentation/Read Me.md",
	"Work Space/notes and plans/",
	"Zo\u00eb/Paths.ts",
	"source/Stra\u00dfe.ts",
	"\u0130stanbul/\u0131.ts",
	"\u65e5\u672c\u8a9e/\u30c6\u30b9\u30c8.tsx",
	strings.Repeat("Nested Directory Level/", 14) + "AnUnusuallyLongFileNameThatRunsOnAndOn.test.ts",
}

// FingerprintPath slices a descendant's name rather than having tspath split and join it (#9prvp67), and a
// fingerprint is only sound if the name is the same string either way: a different spelling of one file
// would move every fingerprint and replay nothing, or worse, collide. So the slice is held to tspath's
// answer over every file and directory of writtenTree, and over the shapes a prefix test gets wrong.
func TestFingerprintPathSlicesWhatTspathWouldJoin(test *testing.T) {
	test.Parallel()

	shapes := []struct {
		name      string
		directory string
		path      string
	}{
		{"a file under the directory", "/repository", "/repository/source/Paths.ts"},
		{"a directory under it", "/repository", "/repository/source/translations"},
		{"a sibling whose name extends the directory's", "/repository", "/repository-two/source/Paths.ts"},
		{"the directory itself", "/repository", "/repository"},
		{"a parent of the directory", "/repository/app", "/repository/Shared.ts"},
		{"a cousin", "/repository/app", "/repository/libraries/structure/Index.ts"},
		{"the filesystem root as the directory", "/", "/repository/Paths.ts"},
		{"a directory spelled with a trailing separator", "/repository/", "/repository/source/Paths.ts"},
		{
			"a mixed-case directory over a canonical, lowercased path",
			"/Work/Repository",
			"/work/repository/ahra/Paths.ts",
		},
		{"a lowercased sibling that extends the directory's name", "/Work/Repo", "/work/repository/Paths.ts"},
		{"a non-ASCII directory", "/Work/Zo\u00eb", "/work/zo\u00eb/Paths.ts"},
	}
	for _, shape := range shapes {
		for _, caseSensitive := range []bool{true, false} {
			program := directoryProgram{directory: shape.directory, caseSensitive: caseSensitive}
			if got, want := FingerprintPath(program, shape.path), tspathName(program, shape.path); got != want {
				test.Errorf(
					"%s (case-sensitive %v): FingerprintPath gave %q, tspath %q",
					shape.name,
					caseSensitive,
					got,
					want,
				)
			}
		}
	}

	// Every file and directory of writtenTree, named from its root, the way a fingerprint names a program's
	// files from the run's directory: as written on a case-sensitive host, and canonical (lowercased) under the
	// root's own spelling on a case-insensitive one, which is macOS's and where the slice first never matched.
	// The root is spelled in mixed case so the canonical names differ from it.
	root := filepath.Join(test.TempDir(), "Repository")
	for _, written := range writtenTree {
		path := filepath.Join(root, filepath.FromSlash(written))
		if strings.HasSuffix(written, "/") {
			if makeError := os.MkdirAll(path, 0o755); makeError != nil {
				test.Fatal(makeError)
			}
			continue
		}
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, nil, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	directory := tspath.NormalizePath(filepath.ToSlash(root))
	sensitive := directoryProgram{directory: directory, caseSensitive: true}
	insensitive := directoryProgram{directory: directory, caseSensitive: false}
	compared := 0
	sliced := 0
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		name := tspath.NormalizePath(filepath.ToSlash(path))
		canonical := string(tspath.CaseInsensitive.PathKey(tspath.RootedPath(name)))
		for _, probe := range []struct {
			program directoryProgram
			path    string
		}{{sensitive, name}, {insensitive, canonical}} {
			if got, want := FingerprintPath(
				probe.program,
				probe.path,
			), tspathName(probe.program, probe.path); got != want {
				test.Errorf(
					"%s (case-sensitive %v): FingerprintPath gave %q, tspath %q",
					probe.path,
					probe.program.caseSensitive,
					got,
					want,
				)
			}
			if _, under := fingerprintPathUnder(
				probe.program.directory,
				probe.path,
				probe.program.caseSensitive,
			); under {
				sliced++
			}
			compared++
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	// A walk that compared nothing would pass everything, and one that missed a name would hold less than it says.
	if compared < 2*(len(writtenTree)+1) {
		test.Fatalf("compared %d paths, fewer than the %d names of the written tree, both ways", compared,
			2*(len(writtenTree)+1))
	}
	// And the slice has to be what answered, both ways, or this proves tspath equal to itself: every name but the
	// root's own is a descendant of the root.
	if sliced != compared-2 {
		test.Fatalf("the slice answered %d of %d paths, so the fast path is not the one being held", sliced, compared)
	}
}
