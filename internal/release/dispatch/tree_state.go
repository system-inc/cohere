package dispatch

import (
	"path/filepath"
	"strings"
)

// A binary built from the working tree says whether that tree had uncommitted changes: the Swift engine
// through its manifest, the `--dev` cohere through its stamps. Both are cached, and a cache keyed on the
// sources alone serves a binary built in one state to a tree in the other (#50wtb2s). A clean tree got an
// engine built while an unrelated file was dirty and was refused at land, and a dirty tree could get one
// built clean and claim a clean build that never happened. So the state is read fresh on every resolve,
// keyed into the binary's name, and handed to the build, and the binary reports what its name holds.

// workingTreeModified reports whether repository has uncommitted changes, tracked or untracked, ignored
// files never counting, under pathspecs when any are given and in the whole repository otherwise.
//
// Each binary's bit covers its own inputs, the files its hash reads. The `--dev` cohere's are the whole
// module, as Go's `vcs.modified` was, so it asks with no pathspec. The Swift engine's are swiftEngineInputs,
// so a member's edit elsewhere in the repository never marks the engine modified (#50wtb2s).
//
// Submodules are left out, for the reason land.go gives: most of the house's worktrees have TypeScript as
// a symlink to another checkout's, and git status refuses that tree outright. A caller whose binary holds
// a submodule's code asks about it apart (see developmentTreeState).
//
// A git that cannot answer reads as modified. Nothing then vouches for the tree, and that is the answer
// the manifest gives a package with no git context at all.
//
// Measured on 2026-11-12, median of five on a warm index: the whole repository took 116 ms on cohere's
// checkout and 72 ms on ahra's, and the Swift engine's inputs 14 ms on cohere's.
//
// No optional locks, so a run never takes the index lock from a member staging a commit beside it.
func workingTreeModified(repository string, pathspecs ...string) bool {
	arguments := []string{
		"--no-optional-locks",
		"status",
		"--porcelain=v1",
		"--untracked-files=normal",
		"--ignore-submodules=all",
	}
	if len(pathspecs) > 0 {
		arguments = append(append(arguments, "--"), pathspecs...)
	}
	status, gitOutputError := gitOutput(repository, arguments...)
	return gitOutputError != nil || strings.TrimSpace(status) != ""
}

// developmentTreeState is the state a `--dev` cohere is stamped with: the commit `HEAD` names, empty when it
// cannot be read, and whether anything compiled in could differ from that commit.
//
// The vendored compiler is compiled in, so it counts here where workingTreeModified leaves submodules out: a
// checkout at another commit than the one `HEAD` pins, or with changes of its own. These are the facts Go's
// `vcs.revision` and `vcs.modified` stamped before the build turned VCS stamping off, which it did so the
// binary reports what its name holds (ResolveWorkingTree). The three questions run at once: measured on
// 2026-11-12, cohere's tree took 116 ms and the compiler's 165 ms, so asking in turn would cost their sum.
func developmentTreeState(moduleDirectory string, compilerCommit string) (string, bool) {
	commit, readHeadError := readHead(moduleDirectory)
	if readHeadError != nil {
		commit = ""
	}

	treeModified := make(chan bool, 1)
	go func() { treeModified <- workingTreeModified(moduleDirectory) }()
	compilerChanged := make(chan bool, 1)
	go func() { compilerChanged <- workingTreeModified(filepath.Join(moduleDirectory, "TypeScript")) }()
	pinned, gitOutputError := gitOutput(moduleDirectory, "rev-parse", "HEAD:TypeScript")
	compilerMoved := gitOutputError != nil || strings.TrimSpace(pinned) != compilerCommit

	treeIsModified, compilerIsModified := <-treeModified, <-compilerChanged
	return commit, readHeadError != nil || compilerMoved || treeIsModified || compilerIsModified
}
