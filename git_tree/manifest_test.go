package git_tree_test

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/git_tree"
)

// The golden record is Loom's own manifest fixture (builder/tracked_test.go's trackedTree: top.txt,
// a/b.txt, z.txt, a link to top.txt, and a submodule sub holding sub.txt and x/y.txt), committed as
// Loom's gitTree commits, by t <t@example.invalid> with message "tree", at a pinned date so its ids
// are the same everywhere. The gitlink is recorded by update-index rather than `submodule add`, whose
// .gitmodules would hold the fixture's temporary path. TestTheGoldenRecordIsGitsOwnOutput makes the
// fixture again and requires git print these bytes.
const (
	goldenHead   = "c99b49ffc53e7c8d793a92c07ca5b4dd65cc5f69"
	goldenCommit = "tree 509663580863ee780d4c1d704ac0d1a1523c520c\n" +
		"author t <t@example.invalid> 1700000000 +0000\n" +
		"committer t <t@example.invalid> 1700000000 +0000\n" +
		"\n" +
		"tree\n"
	goldenFiles = "100644 blob 61780798228d17af2d34fce4cfbdf35556832472\ta/b.txt\x00" +
		"120000 blob 84de2b3aebc02bf40694e7187d19864ac8ed1917\tlink\x00" +
		"160000 commit 8c3eccd1f1909c8a52c75169f630968c02a9c322\tsub\x00" +
		"100644 blob bf1a1fdefa3c7f4b0180a75a951e9574662a8bc8\ttop.txt\x00" +
		"100644 blob b68025345d5301abad4d9ec9166f455243a0d746\tz.txt\x00"
	goldenSubmoduleHead   = "8c3eccd1f1909c8a52c75169f630968c02a9c322"
	goldenSubmoduleCommit = "tree fbd1c495d4cd9b6292efc41c5ff1191a2d8e5148\n" +
		"author t <t@example.invalid> 1700000000 +0000\n" +
		"committer t <t@example.invalid> 1700000000 +0000\n" +
		"\n" +
		"tree\n"
	goldenSubmoduleFiles = "100644 blob 62e0af52c199ec731fe4ad230041cd3286192d49\tsub.txt\x00" +
		"100644 blob 975fbec8256d3e8a3797e7a3611380f27c49f4ac\tx/y.txt\x00"
)

// goldenRecord is one repository's record in the golden manifest: its path in the source and its
// three parts.
type goldenRecord struct {
	repository          string
	head, commit, files string
}

var goldenRecords = []goldenRecord{
	{"", goldenHead, goldenCommit, goldenFiles},
	{"sub", goldenSubmoduleHead, goldenSubmoduleCommit, goldenSubmoduleFiles},
}

// The golden record reads back bound, and Record writes it under the names and in the order Loom's
// builder archives it: the head, the commit and the files under .tracked, then the submodule's under
// .tracked/sub, the head with rev-parse's newline.
func TestTheGoldenRecordReadsAndWritesByteForByte(test *testing.T) {
	test.Parallel()
	var written []git_tree.ManifestFile
	for _, record := range goldenRecords {
		manifest, entries, parseError := git_tree.ParseManifest(
			[]byte(record.head+"\n"),
			[]byte(record.commit),
			[]byte(record.files),
		)
		if parseError != nil {
			test.Fatalf("%q: %v", record.repository, parseError)
		}
		if manifest.Head != record.head || len(entries) != strings.Count(record.files, "\x00") {
			test.Errorf("%q: read as %s with %d entries", record.repository, manifest.Head, len(entries))
		}
		written = append(written, manifest.Record(record.repository)...)
	}
	want := []git_tree.ManifestFile{
		{Name: ".tracked/HEAD", Content: []byte(goldenHead + "\n")},
		{Name: ".tracked/commit", Content: []byte(goldenCommit)},
		{Name: ".tracked/files", Content: []byte(goldenFiles)},
		{Name: ".tracked/sub/HEAD", Content: []byte(goldenSubmoduleHead + "\n")},
		{Name: ".tracked/sub/commit", Content: []byte(goldenSubmoduleCommit)},
		{Name: ".tracked/sub/files", Content: []byte(goldenSubmoduleFiles)},
	}
	if !slices.EqualFunc(written, want, func(left, right git_tree.ManifestFile) bool {
		return left.Name == right.Name && bytes.Equal(left.Content, right.Content)
	}) {
		test.Errorf("the record written is %q, not %q", written, want)
	}
	entries, parseError := git_tree.ParseTreeListing([]byte(goldenFiles))
	if parseError != nil {
		test.Fatal(parseError)
	}
	if entries[2].Mode != git_tree.ModeGitlink || entries[2].Object != goldenSubmoduleHead || entries[2].Path != "sub" {
		test.Errorf("the gitlink reads as %+v", entries[2])
	}
}

// The golden record is git's own output: the fixture made again prints these bytes for rev-parse,
// cat-file commit and ls-tree -r -z --full-tree.
func TestTheGoldenRecordIsGitsOwnOutput(test *testing.T) {
	test.Parallel()
	submodule := newTestRepository(test, git_tree.Sha1)
	submodule.write("sub.txt", "sub\n", 0o644)
	submodule.write("x/y.txt", "y\n", 0o644)
	submodule.commit("tree")
	top := newTestRepository(test, git_tree.Sha1)
	top.write("top.txt", "top\n", 0o644)
	top.write("a/b.txt", "b\n", 0o644)
	top.write("z.txt", "z\n", 0o644)
	top.link("link", "top.txt")
	top.git("add", "-A")
	top.git("update-index", "--add", "--cacheinfo", "160000,"+submodule.line("rev-parse", "HEAD")+",sub")
	top.git("commit", "-q", "-m", "tree")
	for index, repository := range []*testRepository{top, submodule} {
		for _, answer := range []struct {
			arguments []string
			want      string
		}{
			{[]string{"rev-parse", "HEAD"}, goldenRecords[index].head + "\n"},
			{[]string{"cat-file", "commit", "HEAD"}, goldenRecords[index].commit},
			{[]string{"ls-tree", "-r", "-z", "--full-tree", "HEAD"}, goldenRecords[index].files},
		} {
			if got := string(repository.git(answer.arguments...)); got != answer.want {
				test.Errorf("git %s prints %q, and the golden record holds %q",
					strings.Join(answer.arguments, " "), got, answer.want)
			}
		}
	}
}

// A record whose parts aren't one commit's is refused: the head without its newline or with more, a
// commit object edited, a listing of another commit, a listing cut short.
func TestARecordThatIsntOneCommitsIsRefused(test *testing.T) {
	test.Parallel()
	for name, record := range map[string]goldenRecord{
		"the head without its newline": {"", goldenHead, goldenCommit, goldenFiles},
		"the head with a second line":  {"", goldenHead + "\n\n", goldenCommit, goldenFiles},
		"a commit object edited": {
			"",
			goldenHead + "\n",
			strings.Replace(goldenCommit, "tree\n", "tre\n", 1),
			goldenFiles,
		},
		"another commit's listing": {"", goldenHead + "\n", goldenCommit, goldenSubmoduleFiles},
		"a listing cut short":      {"", goldenHead + "\n", goldenCommit, strings.TrimSuffix(goldenFiles, "\x00")},
	} {
		if _, _, parseError := git_tree.ParseManifest(
			[]byte(record.head),
			[]byte(record.commit),
			[]byte(record.files),
		); parseError == nil {
			test.Errorf("%s: read", name)
		}
	}
	_, _, parseError := git_tree.ParseManifest(
		[]byte(goldenHead+"\n"),
		[]byte(goldenCommit),
		[]byte(goldenSubmoduleFiles),
	)
	if !errors.Is(parseError, git_tree.ErrTreeNotListed) {
		test.Errorf("another commit's listing: %v", parseError)
	}
}

// A submodule can't have a record at a path through one of a record's file names, in any case.
func TestManifestCollisionFoldsCase(test *testing.T) {
	test.Parallel()
	for _, repository := range []string{"files", "deep/HEAD", "Commit", "head/x"} {
		if _, _, collides := git_tree.ManifestCollision(repository); !collides {
			test.Errorf("a submodule at %s has a record", repository)
		}
	}
	if part, taken, collides := git_tree.ManifestCollision("cohere/TypeScript"); collides {
		test.Errorf("cohere/TypeScript collides with %s at %s", taken, part)
	}
}
