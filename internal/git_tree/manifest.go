package git_tree

import (
	"bytes"
	"fmt"
	"path"
	"strings"
)

// The `.tracked` manifest is how a source with no .git carries what git would answer about it. Loom's
// builder writes one into every source it chunks, since a chunk carries no .git, and Adamic reads it
// where git can't answer (#cyasrr4). Its layout is our protocol, not git's; each file in it is a git
// command's output byte for byte.
//
// It is one record directory per repository, at the repository's path under ManifestDirectory at the
// source's root: the tree's own at ManifestDirectory itself, a submodule's at
// ManifestDirectory/<its path>. Each record holds three files, run in that repository against the one
// commit `git rev-parse HEAD` named:
//
//	HEAD    git rev-parse HEAD
//	commit  git cat-file commit <the commit>
//	files   git ls-tree -r -z --full-tree <the commit>
//
// The three are bound as git binds them (CheckCommit): commit hashes to the commit, and its tree is the
// tree files rebuilds, so a listing of any other commit is refused. A submodule appears in its parent's
// files as a gitlink naming the commit its own record must hold.

// ManifestDirectory is where a source carries its manifest, at its root.
const ManifestDirectory = ".tracked"

// The files each record directory holds.
const (
	ManifestHeadFile   = "HEAD"
	ManifestCommitFile = "commit"
	ManifestFilesFile  = "files"
)

// ManifestFileNames are the files a record holds, in the order a writer emits them.
func ManifestFileNames() []string {
	return []string{ManifestHeadFile, ManifestCommitFile, ManifestFilesFile}
}

// A Manifest is one repository's record, each part git's output byte for byte.
type Manifest struct {
	// Head is the commit, `git rev-parse HEAD`'s output without its newline.
	Head string
	// Commit is `git cat-file commit <Head>`, the commit object's content.
	Commit []byte
	// Files is `git ls-tree -r -z --full-tree <Head>`.
	Files []byte
}

// A ManifestFile is one file of a record: its path in the source, slash-separated, and its bytes.
type ManifestFile struct {
	Name    string
	Content []byte
}

// NewManifest is the record a writer makes of git's three answers for one commit: head as rev-parse
// printed it with its newline trimmed, the commit object and the listing. It refuses three answers
// that aren't one commit's (CheckCommit), and returns the listing's entries beside it.
func NewManifest(head string, commit, files []byte) (Manifest, []Entry, error) {
	entries, parseError := ParseTreeListing(files)
	if parseError != nil {
		return Manifest{}, nil, parseError
	}
	if checkError := CheckCommit(head, commit, entries); checkError != nil {
		return Manifest{}, nil, checkError
	}
	return Manifest{Head: head, Commit: commit, Files: files}, entries, nil
}

// ParseManifest reads a record's three files as they stand on disk, bound as NewManifest binds them.
// headFile must be exactly what rev-parse prints, the commit's name and one newline.
func ParseManifest(headFile, commit, files []byte) (Manifest, []Entry, error) {
	head, found := bytes.CutSuffix(headFile, []byte("\n"))
	if !found || !ValidObjectName(string(head)) {
		return Manifest{}, nil, fmt.Errorf(
			"the record's %s is %q, not a commit's name and a newline",
			ManifestHeadFile,
			headFile,
		)
	}
	return NewManifest(string(head), commit, files)
}

// ManifestRecord is where the record of the repository at repository (its slash-separated path in the
// source, "" for the source's own) sits in the source.
func ManifestRecord(repository string) string {
	return path.Join(ManifestDirectory, repository)
}

// Record is the manifest's files as a writer puts them into the source for the repository at
// repository ("" for the source's own): the commit's name with rev-parse's newline put back, the
// commit object and the listing, under ManifestRecord(repository). These are the names and bytes
// Loom's builder archives.
func (manifest Manifest) Record(repository string) []ManifestFile {
	return []ManifestFile{
		{Name: ManifestRecord(repository) + "/" + ManifestHeadFile, Content: []byte(manifest.Head + "\n")},
		{Name: ManifestRecord(repository) + "/" + ManifestCommitFile, Content: manifest.Commit},
		{Name: ManifestRecord(repository) + "/" + ManifestFilesFile, Content: manifest.Files},
	}
}

// ManifestCollision reports whether a submodule at repository (its slash-separated path in the
// source) can't have a record: a part of its path is one of a record's file names, as a filesystem that
// folds case would take it, so its record directory would sit where its parent's file is. It returns
// the part and the file name it takes.
func ManifestCollision(repository string) (string, string, bool) {
	for _, part := range strings.Split(repository, "/") {
		for _, taken := range ManifestFileNames() {
			if strings.EqualFold(part, taken) {
				return part, taken, true
			}
		}
	}
	return "", "", false
}
