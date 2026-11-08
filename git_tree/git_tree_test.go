package git_tree_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/system-inc/cohere/git_tree"
)

// Every answer here is checked against git itself, in a repository of each object format. Each mutant
// below must make a test fail:
//
//	the executable bit read as `&0o111`: TestModeOfIsWhatGitAddRecords
//	a gitlink sorted as a directory: TestTreeIdIsWhatGitWriteTreeWrites
//	a directory sorted by its bare name: TestTreeIdIsWhatGitWriteTreeWrites
//	a subtree's mode written "040000": TestTreeIdIsWhatGitWriteTreeWrites
//	HashObject's size check dropped: TestHashObjectRefusesAReaderOfAnotherSize
//	a link's target hashed with a newline: TestHashFileIsWhatGitHashObjectHashes
//	sha256 picked by anything but the name's length: TestCommitHashesAsGitHashesIt
//	the commit's tree line not checked against the listing: TestCheckCommitRefusesAnotherCommitsListing

// permissionModes are the permissions the fixture's files are written at, each beside the mode git
// records for it: the owner's executable bit decides, and the group's and others' change nothing.
var permissionModes = map[os.FileMode]git_tree.Mode{
	0o644: git_tree.ModeRegular,
	0o654: git_tree.ModeRegular,
	0o645: git_tree.ModeRegular,
	0o611: git_tree.ModeRegular,
	0o411: git_tree.ModeRegular,
	0o744: git_tree.ModeExecutable,
	0o700: git_tree.ModeExecutable,
	0o755: git_tree.ModeExecutable,
}

// permissionFile is the fixture's file written at permissions, one of permissionModes.
func permissionFile(permissions os.FileMode) string {
	return fmt.Sprintf("permission/%04o", uint32(permissions))
}

// gitlinkPath is the fixture's gitlink with no checkout, beside a directory and a file that sort
// between it and where it would sort as a directory.
const gitlinkPath = "sub"

// fixture is a repository of format holding every shape a tree's arithmetic must get right: an empty
// file, a 1 MiB file, a name with a tab and a newline, links (one dangling), siblings that sort on
// either side of a directory by git's rule, a gitlink with no checkout beside sub-x/ and sub.y, a
// checked-out submodule, and a file at each of permissionModes. It is added to the index, not
// committed.
func fixture(test *testing.T, format git_tree.ObjectFormat) *testRepository {
	test.Helper()
	repository := newTestRepository(test, format)
	repository.write("empty", "", 0o644)
	repository.write("hello", "hello\n", 0o644)
	repository.write("large", strings.Repeat("0123456789abcdef", 1<<16), 0o644)
	repository.write("tab\tand\nnewline", "an odd name\n", 0o644)
	repository.link("link", "hello")
	repository.link("dangling", "nowhere/at/all")
	// In a tree a directory sorts as if it ended in "/": after a-b and a.b, before a0 and a_b. Sorted
	// by its bare name it would come first.
	repository.write("a/inner", "inside a\n", 0o644)
	repository.write("a/deeper/most", "deepest\n", 0o644)
	for _, sibling := range []string{"a-b", "a.b", "a0", "a_b"} {
		repository.write(sibling, sibling+"\n", 0o644)
	}
	// A gitlink sorts as a file: before sub-x/ and sub.y. As a directory it would come after both.
	repository.write("sub-x/file", "beside the gitlink\n", 0o644)
	repository.write("sub.y", "beside the gitlink\n", 0o644)
	for permissions := range permissionModes {
		repository.write(permissionFile(permissions), "permission "+permissions.String()+"\n", permissions)
	}
	module := newTestRepository(test, format)
	module.write("module.txt", "a submodule's file\n", 0o644)
	module.commit("module")
	repository.git("submodule", "add", "-q", module.directory, "module")
	repository.git("add", "-A")
	// The gitlink with no checkout sits on disk as git leaves an uninitialized submodule: an empty
	// directory.
	if mkdirError := os.Mkdir(repository.path(gitlinkPath), 0o755); mkdirError != nil {
		test.Fatal(mkdirError)
	}
	pin := strings.Repeat("5", len(module.line("rev-parse", "HEAD")))
	repository.git("update-index", "--add", "--cacheinfo", "160000,"+pin+","+gitlinkPath)
	return repository
}

// formats runs check against a repository of each object format, in parallel.
func formats(test *testing.T, check func(test *testing.T, format git_tree.ObjectFormat)) {
	test.Helper()
	for _, format := range []git_tree.ObjectFormat{git_tree.Sha1, git_tree.Sha256} {
		test.Run(string(format), func(test *testing.T) {
			test.Parallel()
			check(test, format)
		})
	}
}

// stage is the repository's index, as ParseStageListing reads git's own listing of it.
func stage(test *testing.T, repository *testRepository) []git_tree.Entry {
	test.Helper()
	entries, parseError := git_tree.ParseStageListing(repository.git("ls-files", "--stage", "-z"))
	if parseError != nil {
		test.Fatal(parseError)
	}
	return entries
}

// committed is the repository's `HEAD`, as ParseTreeListing reads git's own listing of it, with -t
// when directories is set.
func committed(test *testing.T, repository *testRepository, directories bool) []git_tree.Entry {
	test.Helper()
	arguments := []string{"ls-tree", "-r", "-z", "--full-tree", "HEAD"}
	if directories {
		arguments = []string{"ls-tree", "-r", "-t", "-z", "--full-tree", "HEAD"}
	}
	entries, parseError := git_tree.ParseTreeListing(repository.git(arguments...))
	if parseError != nil {
		test.Fatal(parseError)
	}
	return entries
}

// ModeOf, given os.Lstat's information for each path, is the mode `git add` recorded for it.
func TestModeOfIsWhatGitAddRecords(test *testing.T) {
	test.Parallel()
	formats(test, func(test *testing.T, format git_tree.ObjectFormat) {
		repository := fixture(test, format)
		recorded := map[string]git_tree.Mode{}
		for _, entry := range stage(test, repository) {
			recorded[entry.Path] = entry.Mode
			if entry.Mode == git_tree.ModeGitlink {
				continue
			}
			information, lstatError := os.Lstat(repository.path(entry.Path))
			if lstatError != nil {
				test.Fatal(lstatError)
			}
			if mode, modeError := git_tree.ModeOf(information); modeError != nil || mode != entry.Mode {
				test.Errorf("%q at %v: ModeOf is %s (%v), and git recorded %s",
					entry.Path, information.Mode(), mode, modeError, entry.Mode)
			}
		}
		for permissions, want := range permissionModes {
			if recorded[permissionFile(permissions)] != want {
				test.Errorf("git recorded %s for a file at %04o, and the fixture expects %s",
					recorded[permissionFile(permissions)], uint32(permissions), want)
			}
		}
		if recorded["link"] != git_tree.ModeSymbolicLink || recorded["dangling"] != git_tree.ModeSymbolicLink {
			test.Errorf("the links were recorded as %s and %s", recorded["link"], recorded["dangling"])
		}
		information, lstatError := os.Lstat(repository.path("a"))
		if lstatError != nil {
			test.Fatal(lstatError)
		}
		if _, modeError := git_tree.ModeOf(information); modeError == nil {
			test.Error("a directory was given a mode")
		}
	})
}

// HashFile is `git hash-object --no-filters` for every file, and for every link the blob the index
// records, a dangling one included.
func TestHashFileIsWhatGitHashObjectHashes(test *testing.T) {
	test.Parallel()
	formats(test, func(test *testing.T, format git_tree.ObjectFormat) {
		repository := fixture(test, format)
		for _, entry := range stage(test, repository) {
			if entry.Mode == git_tree.ModeGitlink {
				continue
			}
			mode, object, hashError := git_tree.HashFile(format, repository.path(entry.Path))
			if hashError != nil || mode != entry.Mode || object != entry.Object {
				test.Errorf("%q: HashFile is %s %s (%v), and the index records %s %s",
					entry.Path, mode, object, hashError, entry.Mode, entry.Object)
			}
			if entry.Mode == git_tree.ModeSymbolicLink {
				continue
			}
			if hashed := repository.line("hash-object", "--no-filters", "--", entry.Path); hashed != object {
				test.Errorf("%q: HashFile is %s, and git hash-object %s", entry.Path, object, hashed)
			}
		}
	})
}

// walk is the repository's work tree hashed file by file with HashFile, as a listing, beside the
// gitlinks of entries: what `git add -A` would record, read without git.
func walk(test *testing.T, repository *testRepository, entries []git_tree.Entry) []git_tree.Entry {
	test.Helper()
	var walked []git_tree.Entry
	gitlinks := map[string]bool{}
	for _, entry := range entries {
		if entry.Mode == git_tree.ModeGitlink {
			walked = append(walked, entry)
			gitlinks[entry.Path] = true
		}
	}
	visit := func(name string, walkEntry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		relative, relativeError := filepath.Rel(repository.directory, name)
		if relativeError != nil {
			return relativeError
		}
		if walkEntry.IsDir() {
			if filepath.ToSlash(relative) == ".git" || gitlinks[filepath.ToSlash(relative)] {
				return filepath.SkipDir
			}
			return nil
		}
		mode, object, hashError := git_tree.HashFile(repository.format, name)
		if hashError != nil {
			return hashError
		}
		walked = append(walked, git_tree.Entry{Mode: mode, Object: object, Path: filepath.ToSlash(relative)})
		return nil
	}
	if walkError := filepath.WalkDir(repository.directory, visit); walkError != nil {
		test.Fatal(walkError)
	}
	return walked
}

// TreeId rebuilds the tree `git write-tree` writes from the index's listing, `git rev-parse
// HEAD^{tree}` from the commit's (with and without -t), and the same tree again from the files on disk
// hashed one by one beside the index's gitlinks.
func TestTreeIdIsWhatGitWriteTreeWrites(test *testing.T) {
	test.Parallel()
	formats(test, func(test *testing.T, format git_tree.ObjectFormat) {
		repository := fixture(test, format)
		written := repository.line("write-tree")
		entries := stage(test, repository)
		if tree, treeError := git_tree.TreeId(format, entries); treeError != nil || tree != written {
			test.Errorf(
				"the index's listing rebuilds tree %s (%v), and git write-tree wrote %s",
				tree,
				treeError,
				written,
			)
		}
		if tree, treeError := git_tree.TreeId(
			format,
			walk(test, repository, entries),
		); treeError != nil || tree != written {
			test.Errorf("the files on disk rebuild tree %s (%v), and git write-tree wrote %s", tree, treeError, written)
		}
		repository.git("commit", "-q", "-m", "fixture")
		headTree := repository.line("rev-parse", "HEAD^{tree}")
		for _, directories := range []bool{false, true} {
			tree, treeError := git_tree.TreeId(format, committed(test, repository, directories))
			if treeError != nil || tree != headTree {
				test.Errorf("ls-tree (-t: %t) rebuilds tree %s (%v), and `HEAD^{tree}` is %s",
					directories, tree, treeError, headTree)
			}
		}
		empty := repository.line("hash-object", "-t", "tree", "--stdin")
		if tree, treeError := git_tree.TreeId(format, nil); treeError != nil || tree != empty {
			test.Errorf("no entries rebuild tree %s (%v), and git's empty tree is %s", tree, treeError, empty)
		}
	})
}

// A commit object hashes to the commit's id, as git hashes it, and CheckCommit binds it to its own
// listing and refuses it edited.
func TestCommitHashesAsGitHashesIt(test *testing.T) {
	test.Parallel()
	formats(test, func(test *testing.T, format git_tree.ObjectFormat) {
		repository := fixture(test, format)
		head := repository.commit("fixture")
		commit := repository.git("cat-file", "commit", "HEAD")
		if object, hashError := git_tree.HashBytes(
			format,
			git_tree.Commit,
			commit,
		); hashError != nil || object != head {
			test.Errorf("the commit object hashes to %s (%v), and `HEAD` is %s", object, hashError, head)
		}
		if found, formatError := git_tree.FormatOf(head); formatError != nil || found != format {
			test.Errorf("`HEAD` %s reads as format %s (%v)", head, found, formatError)
		}
		entries := committed(test, repository, false)
		if checkError := git_tree.CheckCommit(head, commit, entries); checkError != nil {
			test.Errorf("the head's own commit and listing: %v", checkError)
		}
		edited := bytes.Replace(commit, []byte("fixture"), []byte("fixturf"), 1)
		if checkError := git_tree.CheckCommit(
			head,
			edited,
			entries,
		); !errors.Is(checkError, git_tree.ErrCommitNotHead) {
			test.Errorf("an edited commit object: %v", checkError)
		}
	})
}

// A listing of another commit is refused beside the head's own commit object: the tree it rebuilds
// isn't the one the commit names.
func TestCheckCommitRefusesAnotherCommitsListing(test *testing.T) {
	test.Parallel()
	formats(test, func(test *testing.T, format git_tree.ObjectFormat) {
		repository := fixture(test, format)
		repository.commit("fixture")
		older := committed(test, repository, false)
		repository.write("hello", "hello again\n", 0o644)
		head := repository.commit("later")
		checkError := git_tree.CheckCommit(head, repository.git("cat-file", "commit", "HEAD"), older)
		if !errors.Is(checkError, git_tree.ErrTreeNotListed) {
			test.Errorf("the earlier commit's listing beside the head's commit: %v", checkError)
		}
	})
}

// After a commit, the working tree is changed every way a file can change, and Differs names exactly
// the paths `git diff-files` names, each by the right difference: the executable bit by the owner's
// alone (a chmod that moves only the group's or others' bit changes nothing), content of the same size,
// a link made a file, a file removed, and a file under a directory that became a file.
func TestDiffersFindsWhatGitDiffFilesFinds(test *testing.T) {
	test.Parallel()
	formats(test, func(test *testing.T, format git_tree.ObjectFormat) {
		repository := fixture(test, format)
		repository.commit("fixture")
		entries := stage(test, repository)
		want := map[string]git_tree.Difference{}
		// Each permission file moves to another permission. The four whose owner bit moves differ; the
		// rest don't. 0o100, which the owner can't read, is given to a 100644 file, which git and Differs
		// both find changed by its mode, before any read.
		moves := map[os.FileMode]os.FileMode{
			0o644: 0o654, 0o654: 0o645, 0o645: 0o744, 0o744: 0o700,
			0o700: 0o611, 0o611: 0o411, 0o411: 0o100, 0o755: 0o644,
		}
		for from, to := range moves {
			repository.chmod(permissionFile(from), to)
			if (permissionModes[from] == git_tree.ModeExecutable) != (to&0o100 != 0) {
				want[permissionFile(from)] = git_tree.AnotherExecutableBit
			}
		}
		repository.write("hello", "jello\n", 0o644)
		want["hello"] = git_tree.AnotherContent
		repository.remove("link")
		repository.write("link", "hello", 0o644)
		want["link"] = git_tree.AnotherKind
		repository.remove("empty")
		want["empty"] = git_tree.Missing
		repository.remove("a/deeper/most")
		repository.remove("a/deeper")
		repository.write("a/deeper", "a file now\n", 0o644)
		want["a/deeper/most"] = git_tree.Missing

		// diff-files reads the index's stat data, which a chmod alone makes stale; a refresh rehashes
		// those, so what diff-files names is what changed.
		repository.git("update-index", "-q", "--refresh")
		named := map[string]bool{}
		for name := range strings.SplitSeq(
			strings.TrimSuffix(string(repository.git("diff-files", "--name-only", "-z")), "\x00"),
			"\x00",
		) {
			named[name] = true
		}
		got := map[string]git_tree.Difference{}
		for _, entry := range entries {
			if entry.Mode == git_tree.ModeGitlink {
				continue
			}
			difference, differsError := git_tree.Differs(repository.path(entry.Path), entry)
			if differsError != nil {
				test.Fatalf("%q: %v", entry.Path, differsError)
			}
			if difference != git_tree.Unchanged {
				got[entry.Path] = difference
			}
			if (difference != git_tree.Unchanged) != named[entry.Path] {
				test.Errorf(
					"%q: Differs says %s, and git diff-files names it: %t",
					entry.Path,
					difference,
					named[entry.Path],
				)
			}
		}
		if !maps.Equal(got, want) {
			test.Errorf("Differs found %v, and the fixture changed %v", got, want)
		}
		if len(want) != 8 {
			test.Fatalf("the fixture no longer changes what it means to: %v", want)
		}
	})
}

// A reader holding fewer or more bytes than the size given is refused: a file that changed while it
// was read would otherwise hash to another file's id.
func TestHashObjectRefusesAReaderOfAnotherSize(test *testing.T) {
	test.Parallel()
	for _, size := range []int64{5, 7} {
		object, hashError := git_tree.HashObject(git_tree.Sha1, git_tree.Blob, strings.NewReader("hello\n"), size)
		if hashError == nil {
			test.Errorf("six bytes hashed as %d: %s", size, object)
		}
	}
	reader := iotest.OneByteReader(strings.NewReader("hello\n"))
	object, hashError := git_tree.HashObject(git_tree.Sha1, git_tree.Blob, reader, 6)
	if hashError != nil || object != "ce013625030ba8dba906f756967f9e9ca394464a" {
		test.Errorf("hello as a blob is %s (%v)", object, hashError)
	}
	if _, hashError := git_tree.HashBytes("md5", git_tree.Blob, nil); hashError == nil {
		test.Error("a format git doesn't know was hashed")
	}
}
