package load

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"hash"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// listingFormatVersion is hashed into every listing's key, so a listing kept in another shape is never read.
const listingFormatVersion = "go-engine-listing-1"

// ListingStore is where the load keeps go list's answer between runs, the findings cache's store, by a sha256 key.
type ListingStore interface {
	Kept(key [sha256.Size]byte) ([]byte, bool)
	Keep(key [sha256.Size]byte, listing []byte) error
}

// keptListing is go list's answer as the store keeps it, with what it was read from: the directories it walked for
// packages, the directories of every package it listed outside the module cache and the toolchain, and the module
// files, so a later run can tell whether any of them moved without asking go list again.
type keptListing struct {
	Fingerprint [sha256.Size]byte
	Roots       []string
	Directories []string
	ModuleFiles []string

	Packages  []*listedPackage
	RootPaths []string
	TestOnly  []string
}

// cachedList is list, answered from listings when nothing go list reads has moved since it last answered.
//
// go list's answer is a function of the go environment, the patterns, the module files, and of each package
// directory's file names and each Go file's header: its build constraints, package clause and imports, which end
// the header, and its `//go:embed` lines, whose files a directory's listing names. The module cache and the
// toolchain do not move under a version, so only the directories outside them are read: each directory the patterns
// walk to find packages, and each one a package it listed lives in, which reaches the workspace's other modules. An
// edit below a file's imports, the edit nearly every edit is, moves none of it. When the fingerprint of all of that
// is the one the kept answer was read from, the answer is go list's own; any difference, or anything that cannot be
// read, asks go list. It reports whether the answer was kept.
func cachedList(
	workspace string,
	environment []string,
	patterns []string,
	listings ListingStore,
) (*listing, bool, error) {
	if listings == nil {
		listed, listError := list(workspace, environment, patterns)
		return listed, false, listError
	}
	key, goEnvironment, keyError := listingKey(workspace, environment, patterns)
	if keyError != nil {
		return nil, false, keyError
	}
	if contents, stored := listings.Kept(key); stored {
		var kept keptListing
		if gob.NewDecoder(bytes.NewReader(contents)).Decode(&kept) == nil &&
			listingFingerprint(kept.Roots, kept.Directories, kept.ModuleFiles) == kept.Fingerprint {
			return kept.listing(), true, nil
		}
	}
	listed, listError := list(workspace, environment, patterns)
	if listError != nil {
		return nil, false, listError
	}
	kept := keepListing(workspace, []string{goEnvironment["GOMODCACHE"], goEnvironment["GOROOT"]}, listed)
	var encoded bytes.Buffer
	if gob.NewEncoder(&encoded).Encode(kept) == nil {
		// Ignored: a listing that cannot be kept is asked of go list again next run, which is all its keeping saves.
		_ = listings.Keep(key, encoded.Bytes())
	}
	return listed, false, nil
}

// listingEnvironment are the go environment's variables go list's answer can depend on: the toolchain and where it
// and the module cache are, the target and its variants, cgo, the flags and their build tags, and how modules and
// the workspace resolve. `GOGCCFLAGS` is not among them: it names a temporary directory, new each run.
var listingEnvironment = []string{
	"GOVERSION", "GOROOT", "GOMODCACHE", "GOPATH", "GOOS", "GOARCH", "GOAMD64", "GOARM", "GOARM64", "GO386",
	"GOEXPERIMENT", "GOFIPS140", "GODEBUG", "CGO_ENABLED", "GOFLAGS", "GO111MODULE", "GOWORK", "GOMOD", "GOTOOLCHAIN",
	"GOPROXY", "GONOPROXY", "GONOSUMDB", "GOPRIVATE", "GOINSECURE", "GOSUMDB", "GOVCS",
}

// listingKey is where a workspace's listing is kept: one per workspace, patterns and go environment (see
// listingEnvironment), so another target, toolchain or flag never reads it. It returns the go environment too.
func listingKey(
	workspace string,
	environment []string,
	patterns []string,
) ([sha256.Size]byte, map[string]string, error) {
	command := exec.Command("go", append([]string{"env", "-json"}, listingEnvironment...)...)
	command.Dir = workspace
	command.Env = environment
	output, runError := command.Output()
	if runError != nil {
		return [sha256.Size]byte{}, nil, fmt.Errorf("reading the go environment of %s for its listing: %w",
			workspace, runError)
	}
	goEnvironment := map[string]string{}
	if decodeError := json.Unmarshal(output, &goEnvironment); decodeError != nil {
		return [sha256.Size]byte{}, nil, fmt.Errorf("reading the go environment of %s for its listing: %w",
			workspace, decodeError)
	}
	digest := sha256.New()
	writeParts(digest, []string{listingFormatVersion, workspace})
	for _, name := range listingEnvironment {
		writeParts(digest, []string{name, goEnvironment[name]})
	}
	writeParts(digest, patterns)
	return [sha256.Size]byte(digest.Sum(nil)), goEnvironment, nil
}

// keepListing is listed as the store keeps it: the roots the patterns walk, every directory of a listed package
// outside immutable, the module cache and the toolchain, the module files of the workspace and of every module such
// a package is in, and their fingerprint as it is now.
func keepListing(workspace string, immutable []string, listed *listing) keptListing {
	roots := []string{workspace}
	directories := map[string]bool{}
	moduleFiles := map[string]bool{}
	for _, name := range []string{"go.work", "go.work.sum", "go.mod", "go.sum"} {
		moduleFiles[filepath.Join(workspace, name)] = true
	}
	for _, listedPackage := range listed.packages {
		if listedPackage.Directory == "" || isUnder(listedPackage.Directory, immutable) {
			continue
		}
		directories[listedPackage.Directory] = true
		if module := listedPackage.Module; module != nil && module.Dir != "" {
			moduleFiles[filepath.Join(module.Dir, "go.mod")] = true
			moduleFiles[filepath.Join(module.Dir, "go.sum")] = true
			if listed.roots[listedPackage.ImportPath] && !slices.Contains(roots, module.Dir) {
				roots = append(roots, module.Dir)
			}
		}
	}
	kept := keptListing{
		Roots:       roots,
		Directories: sortedKeys(directories),
		ModuleFiles: sortedKeys(moduleFiles),
		Packages:    listed.packages,
		RootPaths:   sortedKeys(listed.roots),
		TestOnly:    sortedKeys(listed.testOnly),
	}
	kept.Fingerprint = listingFingerprint(kept.Roots, kept.Directories, kept.ModuleFiles)
	return kept
}

// listing is the kept answer as list gives it.
func (kept keptListing) listing() *listing {
	restored := &listing{
		packages: kept.Packages,
		byPath:   map[string]*listedPackage{},
		roots:    map[string]bool{},
		testOnly: map[string]bool{},
	}
	for _, listedPackage := range kept.Packages {
		restored.byPath[listedPackage.ImportPath] = listedPackage
	}
	for _, path := range kept.RootPaths {
		restored.roots[path] = true
	}
	for _, path := range kept.TestOnly {
		restored.testOnly[path] = true
	}
	return restored
}

// isUnder reports whether path is one of roots or below one.
func isUnder(path string, roots []string) bool {
	for _, root := range roots {
		if root == "" {
			continue
		}
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// listingFingerprint is the sha256 of everything go list's answer is read from outside the go environment: each
// module file's bytes, and each directory's entries with each Go file's header, the directories being those the
// roots hold, walked as go walks a pattern, and the listed ones.
func listingFingerprint(roots []string, directories []string, moduleFiles []string) [sha256.Size]byte {
	digest := sha256.New()
	for _, file := range moduleFiles {
		contents, readError := os.ReadFile(file)
		if readError != nil {
			writeParts(digest, []string{file, "unreadable"})
			continue
		}
		fileDigest := sha256.Sum256(contents)
		writeParts(digest, []string{file, string(fileDigest[:])})
	}
	walked := map[string]bool{}
	for _, directory := range directories {
		walked[directory] = true
	}
	for _, root := range roots {
		writeParts(digest, []string{"root", root})
		for _, directory := range packageDirectories(root) {
			walked[directory] = true
		}
	}
	ordered := sortedKeys(walked)
	parts := make([][sha256.Size]byte, len(ordered))
	var group sync.WaitGroup
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	for index, directory := range ordered {
		group.Add(1)
		slots <- struct{}{}
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			parts[index] = directoryFingerprint(directory)
		}()
	}
	group.Wait()
	for index, directory := range ordered {
		writeParts(digest, []string{directory, string(parts[index][:])})
	}
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint
}

// packageDirectories is every directory below root that go would look in for a package matching root's `/...`: it
// leaves out what go leaves out, a directory whose name starts with a dot or an underscore, testdata, and one that
// holds a module of its own, and it never follows a symbolic link.
func packageDirectories(root string) []string {
	var directories []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			directories = append(directories, path)
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" {
				return filepath.SkipDir
			}
			if _, moduleError := os.Stat(filepath.Join(path, "go.mod")); moduleError == nil {
				return filepath.SkipDir
			}
		}
		directories = append(directories, path)
		return nil
	})
	if walkError != nil {
		directories = append(directories, root)
	}
	return directories
}

// directoryFingerprint is the sha256 of one directory's entries, each by name and kind, and of each Go file's header.
// A directory with a `//go:embed` line has every entry below it hashed too, since the files a pattern embeds are
// listed, and a pattern can reach into a subdirectory and into testdata. What cannot be read is hashed as such.
func directoryFingerprint(directory string) [sha256.Size]byte {
	digest := sha256.New()
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		writeParts(digest, []string{"unreadable", readError.Error()})
		return [sha256.Size]byte(digest.Sum(nil))
	}
	embeds := false
	for _, entry := range entries {
		writeParts(digest, []string{entry.Name(), entry.Type().String()})
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		header, embedding := goFileHeader(filepath.Join(directory, entry.Name()))
		writeParts(digest, []string{header})
		embeds = embeds || embedding
	}
	if embeds {
		// Ignored: an entry that cannot be walked is left out of the hash, and a run that cannot read it cannot read
		// the file it would embed either, which go list then reports.
		_ = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, pathError error) error {
			if pathError == nil && path != directory {
				writeParts(digest, []string{"embedded", path, entry.Type().String()})
			}
			return nil
		})
	}
	return [sha256.Size]byte(digest.Sum(nil))
}

// goFileHeader is what go list reads of one Go file: its bytes up to the end of its imports, which hold its build
// constraints, its package clause and its imports, cgo's preamble included, and its `//go:embed` lines, and whether
// it has any. A file that does not parse that far is hashed whole, since go list's error about it can depend on
// any of it.
func goFileHeader(file string) (string, bool) {
	contents, readError := os.ReadFile(file)
	if readError != nil {
		return "unreadable: " + readError.Error(), false
	}
	embeds := bytes.Contains(contents, []byte("//go:embed"))
	var header strings.Builder
	fileSet := token.NewFileSet()
	parsed, parseError := parser.ParseFile(fileSet, file, contents, parser.ImportsOnly)
	if parseError != nil || parsed == nil {
		header.Write(contents)
	} else {
		end := parsed.Name.End()
		if count := len(parsed.Decls); count > 0 {
			end = parsed.Decls[count-1].End()
		}
		header.Write(contents[:fileSet.Position(end).Offset])
	}
	if embeds {
		for line := range strings.Lines(string(contents)) {
			if strings.Contains(line, "//go:embed") {
				header.WriteString(line)
			}
		}
	}
	return header.String(), embeds
}

// sortedKeys is a set's members, sorted.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// writeParts hashes each of parts prefixed by its length, so no two lists of parts hash alike.
func writeParts(digest hash.Hash, parts []string) {
	for _, part := range parts {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		// Ignored: a hash.Hash never returns an error from Write.
		_, _ = digest.Write(length[:])
		// Ignored: as above.
		_, _ = io.WriteString(digest, part)
	}
}
