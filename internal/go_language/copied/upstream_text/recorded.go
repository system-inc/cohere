package upstream_text

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
)

// digestsFile is the recorded files' sha256 sums, one `<hex>  <path>` line each as sha256sum writes them, and
// readmeFile says what the directory is; neither is recorded text.
const (
	digestsFile = "SHA256SUMS"
	readmeFile  = "README.md"
)

// Recorded is the upstream text in a recorded tree, with each file's digest as SHA256SUMS lists it, by its path
// from the tree's root.
type Recorded struct {
	files   fs.FS
	digests map[string]string
}

// ReadRecorded reads files' SHA256SUMS. Each line is a lowercase hex sha256, two spaces and a clean slash-separated
// path, listed once.
func ReadRecorded(files fs.FS) (Recorded, error) {
	sums, readError := fs.ReadFile(files, digestsFile)
	if readError != nil {
		return Recorded{}, fmt.Errorf("reading %s/%s: %w", RecordedDirectory, digestsFile, readError)
	}
	upstream := Recorded{files: files, digests: map[string]string{}}
	lines := bufio.NewScanner(bytes.NewReader(sums))
	for number := 1; lines.Scan(); number++ {
		digest, listedPath, spelled := strings.Cut(lines.Text(), "  ")
		decoded, decodeError := hex.DecodeString(digest)
		if !spelled || decodeError != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != digest ||
			!fs.ValidPath(listedPath) || listedPath == "." {
			return Recorded{}, fmt.Errorf("%s/%s:%d: %q is not spelled <lowercase hex sha256>  <path>",
				RecordedDirectory, digestsFile, number, lines.Text())
		}
		if _, listed := upstream.digests[listedPath]; listed {
			return Recorded{}, fmt.Errorf("%s/%s:%d: lists %s a second time", RecordedDirectory, digestsFile,
				number, listedPath)
		}
		upstream.digests[listedPath] = digest
	}
	if scanError := lines.Err(); scanError != nil {
		return Recorded{}, fmt.Errorf("reading %s/%s: %w", RecordedDirectory, digestsFile, scanError)
	}
	return upstream, nil
}

// Listed is every path SHA256SUMS lists, sorted.
func (upstream Recorded) Listed() []string {
	return slices.Sorted(maps.Keys(upstream.digests))
}

// text is the recorded file at listedPath, which must be listed, once its digest is the one SHA256SUMS lists, so
// text edited by hand is never what a copy is held to.
func (upstream Recorded) text(listedPath string) ([]byte, error) {
	contents, readError := fs.ReadFile(upstream.files, listedPath)
	if readError != nil {
		return nil, fmt.Errorf("%s/%s: reading the recorded text: %w", RecordedDirectory, listedPath, readError)
	}
	sum := sha256.Sum256(contents)
	if digest := hex.EncodeToString(sum[:]); digest != upstream.digests[listedPath] {
		return nil, fmt.Errorf("%s/%s: its sha256 is %s, but %s lists %s: recorded text is upstream's and never "+
			"edited, so record it again with tools/generate", RecordedDirectory, listedPath, digest, digestsFile,
			upstream.digests[listedPath])
	}
	return contents, nil
}

// Verify is every problem with the recorded tree, in order of path: a listed file missing or whose digest is not
// the one listed, a file no line lists, and a source, the first directory below the module's, with no LICENSE.
func (upstream Recorded) Verify() []string {
	var problems []string
	for _, listedPath := range upstream.Listed() {
		if _, textError := upstream.text(listedPath); textError != nil {
			problems = append(problems, textError.Error())
		}
	}
	walkError := fs.WalkDir(upstream.files, ".", func(walkedPath string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() || walkedPath == digestsFile || walkedPath == readmeFile {
			return nil
		}
		if _, listed := upstream.digests[walkedPath]; !listed {
			problems = append(problems, fmt.Sprintf("%s/%s: no line of %s lists it, so no digest holds it",
				RecordedDirectory, walkedPath, digestsFile))
		}
		return nil
	})
	if walkError != nil {
		problems = append(problems, fmt.Sprintf("walking %s: %v", RecordedDirectory, walkError))
	}
	sources := map[string]bool{}
	for listedPath := range upstream.digests {
		sources[source(listedPath)] = true
	}
	for _, listedSource := range slices.Sorted(maps.Keys(sources)) {
		if _, licensed := upstream.digests[listedSource+"/LICENSE"]; !licensed {
			problems = append(problems, fmt.Sprintf("%s/%s: no LICENSE is recorded beside its files, as its "+
				"license asks", RecordedDirectory, listedSource))
		}
	}
	return problems
}

// source is the <module>@<version> directory listedPath is below, which is the first one whose name holds an @,
// since a module path's own elements never do.
func source(listedPath string) string {
	elements := strings.Split(listedPath, "/")
	for index, element := range elements[:len(elements)-1] {
		if strings.Contains(element, "@") {
			return strings.Join(elements[:index+1], "/")
		}
	}
	return strings.Join(elements[:len(elements)-1], "/")
}
