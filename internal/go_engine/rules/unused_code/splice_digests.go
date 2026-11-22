package unused_code

import (
	"crypto/sha256"

	"golang.org/x/tools/go/analysis"
)

// spliceDigests are the digests a build compares a record's files by: the host's, which its side answers by the name
// the host parsed each file under, nil for a side that answers none, and the build's own, read through the pass once
// a file as asked, with the names the pass parsed.
type spliceDigests struct {
	pass              *analysis.Pass
	host              FileDigestHost
	packageIdentifier string
	own               map[string]ownDigest
	parsed            map[string]bool
}

// ownDigest is the sha256 of a file as the build's pass reads it, and whether the pass could read it at all.
type ownDigest struct {
	digest [sha256.Size]byte
	isRead bool
}

// ownMatches is whether the build reads the file parsed under filename with the bytes whose digest is want.
func (digests *spliceDigests) ownMatches(filename string, want [sha256.Size]byte) bool {
	own, isAsked := digests.own[filename]
	if !isAsked {
		if digests.pass.ReadFile != nil {
			if contents, readError := digests.pass.ReadFile(filename); readError == nil {
				own = ownDigest{digest: sha256.Sum256(contents), isRead: true}
			}
		}
		digests.own[filename] = own
	}
	return own.isRead && own.digest == want
}

// allMatch is whether every one of files, a record's, is a file the pass parsed under that name, whose digest the
// host's side answers, and which the build reads with the host's bytes. The record's own file, parsedFile, is answered
// with the record, as parsedDigest. A file the side digests no bytes of cannot be checked, so it fails as one read
// otherwise.
func (digests *spliceDigests) allMatch(
	files []string,
	parsedFile string,
	parsedDigest [sha256.Size]byte,
) bool {
	for _, file := range files {
		want, isDigested := parsedDigest, file == parsedFile
		if !isDigested && digests.host != nil {
			want, isDigested = digests.host.FileDigest(digests.packageIdentifier, file)
		}
		if !digests.parsed[file] || !isDigested || !digests.ownMatches(file, want) {
			return false
		}
	}
	return true
}
