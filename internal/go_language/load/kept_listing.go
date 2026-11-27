package load

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"go/parser"
	"go/token"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/go_language/timing"
)

// listingFormatVersion is hashed into every listing's key, so a listing kept in another shape is never read. 2 is 1's
// shape under the package's name, go-language where 1 said go-engine (#9gjmwgs), so a listing kept before it misses.
const listingFormatVersion = "go-language-listing-2"

// ListingStore is where the load keeps go list's answer between runs, by a sha256 key: the findings cache's store for
// a listing, and the system cache's for a build context (KeepContext).
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

// cachedList is list in goEnvironment's workspace under its whole environment, answered from listings when nothing
// go list reads has moved since it last answered.
//
// go list's answer is a function of the go environment, the patterns, the module files, and of each package
// directory's file names and each Go file's header: its build constraints, package clause and imports, which end
// the header, and its `//go:embed` lines, whose files a directory's listing names. The module cache and the
// toolchain do not move under a version, so only the directories outside them are read: each directory the patterns
// walk to find packages, and each one a package it listed lives in, which reaches the workspace's other modules. An
// edit below a file's imports, the edit nearly every edit is, moves none of it. When the fingerprint of all of that
// is the one the kept answer was read from, the answer is go list's own; any difference, or anything that cannot be
// read, asks go list. It reports whether the answer was kept. Every file held holds is read from its held bytes, by
// the fingerprint and the lister alike. The kept answer's steady fingerprint is read through fingerprints, and each
// pattern's walk through walks, once for the run's loads, when each is set (Fingerprints, PackageWalks), and
// every module file a go command of the run can rewrite is read again at each ask (listingFingerprint). The listing
// adds its CPU to collector; a kept answer's fingerprint adds nothing but what the caller measures on its own
// goroutine, since a run under `--timing` keeps no listing.
//
// Before any of that, a listing a load of the run already made or read kept under the same fingerprints and key is
// answered as it was, with whether it was kept (Fingerprints.remembered): an extra build lists twice, for its first
// load and again for a load after its first check, and with no store each was a listing of its own, 0.47 s of a
// Loom run under `--no-cache` (#32594zm). Whatever this ask lists or reads kept is remembered for the asks after it
// (Fingerprints.remember). A nil fingerprints remembers and answers nothing.
func cachedList(
	goEnvironment *GoEnvironment,
	patterns []string,
	listings ListingStore,
	held Held,
	shared *SharedReads,
	fingerprints *Fingerprints,
	walks *PackageWalks,
	collector *timing.Collector,
) (*listing, bool, error) {
	key := listingKey(goEnvironment, patterns)
	if listed, kept, remembered := fingerprints.remembered(key); remembered {
		return listed, kept, nil
	}
	listed, kept, listError := storedList(goEnvironment, key, patterns, listings, held, shared, fingerprints, walks,
		collector)
	if listError != nil {
		return nil, false, listError
	}
	fingerprints.remember(goEnvironment, key, listed, kept)
	return listed, kept, nil
}

// storedList is cachedList once the run remembers no listing for key, listingKey's of goEnvironment and patterns: list,
// answered from listings when nothing go list reads has moved since it last answered, and kept there for the next run.
// It reports whether the answer was kept.
func storedList(
	goEnvironment *GoEnvironment,
	key [sha256.Size]byte,
	patterns []string,
	listings ListingStore,
	held Held,
	shared *SharedReads,
	fingerprints *Fingerprints,
	walks *PackageWalks,
	collector *timing.Collector,
) (*listing, bool, error) {
	if listings == nil {
		listed, listError := list(goEnvironment, patterns, held, shared, walks, collector)
		return listed, false, listError
	}
	if contents, stored := listings.Kept(key); stored {
		var kept keptListing
		if gob.NewDecoder(bytes.NewReader(contents)).Decode(&kept) == nil {
			fingerprint, certain := listingFingerprint(kept.Roots, kept.Directories, kept.ModuleFiles, held,
				fingerprints)
			if certain && fingerprint == kept.Fingerprint {
				return kept.listing(), true, nil
			}
		}
	}
	listed, listError := list(goEnvironment, patterns, held, shared, walks, collector)
	if listError != nil {
		return nil, false, listError
	}
	kept, keepable := keepListing(goEnvironment.workspace, goEnvironment.value("GOMODCACHE"),
		goEnvironment.value("GOROOT"), listed, held, fingerprints)
	var encoded bytes.Buffer
	if keepable && gob.NewEncoder(&encoded).Encode(kept) == nil {
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
// listingEnvironment), so another target, toolchain or flag never reads it, but for the flags VerdictFlags leaves out,
// which change only what go prints. The go environment is goEnvironment's, as go env reports it under the whole
// environment go list runs with, `GOFLAGS` with its -pgo=off, so the key is the one a go env of its own asked there
// made (see GoEnvironment.value).
func listingKey(goEnvironment *GoEnvironment, patterns []string) [sha256.Size]byte {
	digest := sha256.New()
	writeParts(digest, []string{listingFormatVersion, goEnvironment.workspace})
	for _, name := range listingEnvironment {
		value := goEnvironment.value(name)
		// What go list answers never depends on the flags VerdictFlags leaves out, -p among them, which cohere-dev's
		// pool sets to each token's share: with them in the key, a run under another share missed every listing.
		if name == "GOFLAGS" {
			value = VerdictFlags(value)
		}
		writeParts(digest, []string{name, value})
	}
	writeParts(digest, patterns)
	return [sha256.Size]byte(digest.Sum(nil))
}

// keepListing is listed as the store keeps it: what it was read from (listingInputs), and their fingerprint as it is
// now. It reports whether the listing may be kept at all: one with an error in it, an import that does not resolve,
// say, or a fingerprint with anything unreadable, never is. Each file held holds is fingerprinted from its held bytes,
// and the fingerprint read as listingFingerprint reads it, through fingerprints when it is set.
func keepListing(
	workspace string,
	moduleCache string,
	toolchainRoot string,
	listed *listing,
	held Held,
	fingerprints *Fingerprints,
) (keptListing, bool) {
	kept, keepable := listingInputs(workspace, moduleCache, toolchainRoot, listed)
	if !keepable {
		return keptListing{}, false
	}
	fingerprint, certain := listingFingerprint(kept.Roots, kept.Directories, kept.ModuleFiles, held, fingerprints)
	kept.Fingerprint = fingerprint
	return kept, certain
}

// listingInputs is listed as the store keeps it but for its fingerprint: the roots the patterns walk, every directory
// of a listed package that can move, and the module files of the workspace and of every module such a package is in,
// vendor's modules.txt among them. It reports whether the listing may be kept, or remembered for the run
// (Fingerprints.remember), at all: one with an error in it, an import that does not resolve, say, which another ask
// could answer otherwise, never is.
//
// A package's directory is left out only when it cannot move: one in the module cache whose module has a version,
// which the cache never rewrites under that version, and one in the toolchain's root that is in no module, the
// standard library, which moves only with the toolchain's version the key hashes. Anything else there, a replace
// into the module cache by path, say, is read like any directory of the workspace.
func listingInputs(workspace string, moduleCache string, toolchainRoot string, listed *listing) (keptListing, bool) {
	roots := []string{workspace}
	directories := map[string]bool{}
	moduleFiles := map[string]bool{}
	for _, name := range []string{"go.work", "go.work.sum", "go.mod", "go.sum", "vendor/modules.txt"} {
		moduleFiles[filepath.Join(workspace, filepath.FromSlash(name))] = true
	}
	for _, listedPackage := range listed.packages {
		if listedPackage.Error != nil {
			return keptListing{}, false
		}
		module := listedPackage.Module
		versioned := module != nil && module.Version != "" && (module.Replace == nil || module.Replace.Version != "")
		immutable := isUnder(listedPackage.Directory, []string{moduleCache}) && versioned ||
			isUnder(listedPackage.Directory, []string{toolchainRoot}) && module == nil
		if listedPackage.Directory == "" || immutable {
			continue
		}
		directories[listedPackage.Directory] = true
		if module != nil && module.Dir != "" {
			for _, name := range []string{"go.mod", "go.sum", "vendor/modules.txt"} {
				moduleFiles[filepath.Join(module.Dir, filepath.FromSlash(name))] = true
			}
			if listed.roots[listedPackage.ImportPath] && !slices.Contains(roots, module.Dir) {
				roots = append(roots, module.Dir)
			}
		}
	}
	return keptListing{
		Roots:       roots,
		Directories: sortedKeys(directories),
		ModuleFiles: sortedKeys(moduleFiles),
		Packages:    listed.packages,
		RootPaths:   sortedKeys(listed.roots),
		TestOnly:    sortedKeys(listed.testOnly),
	}, true
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
// roots hold, walked as go walks a pattern, and the listed ones. It reports whether it read all of it: a module
// file that is absent is read as absent, but anything that could not be read, a directory, a Go file or a module
// file, is a doubt, and a doubt is never taken for nothing having moved.
//
// It is the steady fingerprint of every input no go command of the run can rewrite (steadyFingerprint), read through
// fingerprints, which a nil one reads again (Fingerprints), and then each module file one can (rewritableByGo), read
// from disk by every caller, so a build asking after another build's go list rewrote one reads
// the bytes it left. A go.work root's every listing names its go.work.sum, so each ask there reads that one file again
// and the walk, the directories and the other module files once a run.
func listingFingerprint(
	roots []string,
	directories []string,
	moduleFiles []string,
	held Held,
	fingerprints *Fingerprints,
) ([sha256.Size]byte, bool) {
	var steadyFiles []string
	var rewritableFiles []string
	for _, file := range moduleFiles {
		if rewritableByGo(file) {
			rewritableFiles = append(rewritableFiles, file)
			continue
		}
		steadyFiles = append(steadyFiles, file)
	}
	steady, certain := fingerprints.steady(roots, directories, steadyFiles, held)
	digest := sha256.New()
	writeParts(digest, []string{"steady", string(steady[:])})
	for _, file := range rewritableFiles {
		fileDigest, absent, readable := moduleFileDigest(file)
		certain = hashModuleFile(digest, file, fileDigest, absent, readable) && certain
	}
	return [sha256.Size]byte(digest.Sum(nil)), certain
}

// rewritableByGo reports whether a go command of a run can rewrite file, a module file a kept listing names, while
// the run's builds read it: a go.work.sum, which go writes in workspace mode whatever -mod says and a build's go list
// writes in the tree when the workspace's guard cannot run it beside a side copy of the go.work (workspaceSumGuard).
// go.mod and go.sum it never writes: every go command of a load runs at -mod=readonly or -mod=vendor, a user's
// -mod=mod included (ReadGoEnvironment, #ycf4240).
func rewritableByGo(file string) bool {
	return filepath.Base(file) == "go.work.sum"
}

// steadyFingerprint is listingFingerprint of every input it names but the module files a go command of the run can
// rewrite, which moduleFiles never holds: each module file's digest, each root's walk and each directory's own
// fingerprint is read through fingerprints, which a nil one reads again (Fingerprints). A Go file held holds is read
// from its held bytes, which are the disk's.
func steadyFingerprint(
	roots []string,
	directories []string,
	moduleFiles []string,
	held Held,
	fingerprints *Fingerprints,
) ([sha256.Size]byte, bool) {
	if fingerprints != nil {
		fingerprints.listingsComputed.Add(1)
	}
	digest := sha256.New()
	certain := true
	for _, file := range moduleFiles {
		fileDigest, absent, readable := fingerprints.moduleFile(file)
		certain = hashModuleFile(digest, file, fileDigest, absent, readable) && certain
	}
	walked := map[string]bool{}
	for _, directory := range directories {
		walked[directory] = true
	}
	for _, root := range roots {
		writeParts(digest, []string{"root", root})
		rootDirectories, walkedWhole := fingerprints.root(root)
		certain = certain && walkedWhole
		for _, directory := range rootDirectories {
			walked[directory] = true
		}
	}
	ordered := sortedKeys(walked)
	parts := make([][sha256.Size]byte, len(ordered))
	readWhole := make([]bool, len(ordered))
	var group sync.WaitGroup
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	for index, directory := range ordered {
		group.Add(1)
		slots <- struct{}{}
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			parts[index], readWhole[index] = fingerprints.directory(directory, held)
		}()
	}
	group.Wait()
	for index, directory := range ordered {
		writeParts(digest, []string{directory, string(parts[index][:])})
		certain = certain && readWhole[index]
	}
	return [sha256.Size]byte(digest.Sum(nil)), certain
}

// moduleFileDigest is the sha256 of one module file's bytes, read from disk. It reports whether the file is absent,
// and whether it could be read: a file that is absent was read as absent.
func moduleFileDigest(file string) ([sha256.Size]byte, bool, bool) {
	contents, readError := os.ReadFile(file)
	if errors.Is(readError, fs.ErrNotExist) {
		return [sha256.Size]byte{}, true, true
	}
	if readError != nil {
		return [sha256.Size]byte{}, false, false
	}
	return sha256.Sum256(contents), false, true
}

// hashModuleFile hashes one module file into digest by its name and its fileDigest, or as absent, and reports whether
// it could be read: one that could not is hashed as nothing, a doubt.
func hashModuleFile(
	digest hash.Hash,
	file string,
	fileDigest [sha256.Size]byte,
	absent bool,
	readable bool,
) bool {
	if absent {
		writeParts(digest, []string{file, "absent"})
		return true
	}
	if !readable {
		return false
	}
	writeParts(digest, []string{file, string(fileDigest[:])})
	return true
}

// packageDirectories is every directory below root that go would look in for a package matching root's `/...`: it
// leaves out what go leaves out, a directory whose name starts with a dot or an underscore, testdata, and one that
// holds a module of its own, and, as go does, it never follows a symbolic link, which its directory's entries name.
// It reports whether it walked all of it.
func packageDirectories(root string) ([]string, bool) {
	var directories []string
	whole := true
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			whole = false
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root {
			if walkSkips(entry.Name()) {
				return filepath.SkipDir
			}
			if _, moduleError := os.Stat(filepath.Join(path, "go.mod")); moduleError == nil {
				return filepath.SkipDir
			}
		}
		directories = append(directories, path)
		return nil
	})
	return directories, whole && walkError == nil
}

// walkSkips reports whether go's walk for a pattern's packages leaves out a directory named name, and everything below
// it: a name that starts with a dot or an underscore, and testdata. It is go's own rule, read in go1.27.0's
// cmd/go/internal/modload/search.go:105 (walkPkgs) and :234 (walkFromIndex), and cmd/go/internal/search/search.go:151.
func walkSkips(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata"
}

// directoryFingerprint is the sha256 of one directory's entries, each by name and kind, and of each Go file's header.
// A directory with a `//go:embed` line has every entry below it hashed too, since the files a pattern embeds are
// listed, and a pattern can reach into a subdirectory and into testdata. It reports whether it read all of it. A Go
// file held holds is read from its held bytes.
//
// A subdirectory go's walk leaves out (walkSkips) is left out of the entries: no pattern's walk reaches it, and a
// package's own listing never reads a subdirectory, since go/build's Import passes over every entry that is a
// directory (go1.27.0's go/build/build.go:887). So the findings store's `.cache`, which the store's first write makes
// in the root after the first run's fingerprint was read, moves no kept listing (#78whs52). The embed walk below still
// hashes it, since `//go:embed` can name a dot directory and `all:` reaches one.
func directoryFingerprint(directory string, held Held) ([sha256.Size]byte, bool) {
	digest := sha256.New()
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		return [sha256.Size]byte{}, false
	}
	whole := true
	embeds := false
	for _, entry := range entries {
		if entry.IsDir() && walkSkips(entry.Name()) {
			continue
		}
		writeParts(digest, []string{entry.Name(), entry.Type().String()})
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		header, embedding, readable := goFileHeader(filepath.Join(directory, entry.Name()), held)
		writeParts(digest, []string{header})
		embeds = embeds || embedding
		whole = whole && readable
	}
	if embeds {
		walkError := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, pathError error) error {
			if pathError != nil {
				whole = false
				return nil
			}
			if path != directory {
				writeParts(digest, []string{"embedded", path, entry.Type().String()})
			}
			return nil
		})
		whole = whole && walkError == nil
	}
	return [sha256.Size]byte(digest.Sum(nil)), whole
}

// goFileHeader is what go list reads of one Go file, as go/build's readGoInfo reads it: its bytes up to the end of
// its imports, which hold its build constraints, its package clause and its imports, cgo's preamble and its #cgo
// lines included, and its `//go:embed` lines. Nothing below the imports reaches go list's answer, not even a parse
// error there, so an edit to a function's body moves no listing. readGoInfo is the reason this is enough, so a Go
// whose readGoInfo reads more of a file is a reason to look here again. A file that does not parse that far is hashed
// whole, since go list's error about it can depend on any of it. It returns whether the file embeds, and whether it
// could be read. A file held holds is read from its held bytes, and any other from disk.
func goFileHeader(file string, held Held) (string, bool, bool) {
	contents, readError := held.read(file)
	if readError != nil {
		return "", false, false
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
	return header.String(), embeds, true
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
