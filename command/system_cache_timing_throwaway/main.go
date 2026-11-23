// Command system_cache_timing_throwaway times reading one build's standard library from the engine's system cache
// through system_cache's fork-typed reader against reading the same packages' compiler export data from go list
// through the fork's lazy gcimporter (#hmepe1g). It is a throwaway measurement, never to land.
//
//	go run ./command/cohere-dev build -o <folder>/system-cache-timing ./command/system_cache_timing_throwaway
//	<folder>/system-cache-timing --engine <root>/.cache/cohere/bin/cohere-darwin-arm64-<hash> --goos windows
//
// It reads the system cache and writes nothing to it. Which build a pack is, and which package each of its records
// is, nothing in the system cache says: a pack is named by a hash of its build's context, and a package's export data
// writes its own path empty. So each pack's records are matched to go list's standard library for the build asked
// by the package's name, from its export data's manifest, and the imports its record's body holds, and the pack that
// matches best is the build's. A record whose name and imports two packages share is decoded as each in turn, and is
// the one whose declarations sit in that package's directory.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/system-inc/go-types/gcimporter"
	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/variational_types/system_cache"
	"github.com/system-inc/cohere/internal/variational_types/system_cache/export_data"
)

// listedPackage is what go list says of one package.
type listedPackage struct {
	ImportPath string
	Name       string
	Imports    []string
	Export     string
}

// packRecord is what the command reads of a record: its format, its body, inline or by the key of the record that
// holds it, and its export data's digest.
type packRecord struct {
	Version    string
	Inline     []byte
	BodyAt     []byte
	ExportData []byte
}

// packBody is what the command reads of an entry's body: the paths of the packages its types import.
type packBody struct {
	Imports []string
}

// identifiedPack is one pack's records matched to the build's packages: each matched package's key by path, the
// records two or more packages share a name and imports with, by key with the paths they could be, and how many
// records matched none.
type identifiedPack struct {
	name      string
	keys      map[string]cache.Key
	ambiguous map[cache.Key][]string
	unmatched int
}

func main() {
	engine := flag.String("engine", "", "the cohere binary whose system cache entries are read")
	goos := flag.String("goos", "windows", "the build's GOOS, on arm64")
	cgo := flag.String("cgo", "0", "the build's CGO_ENABLED")
	flag.Parse()
	if *engine == "" {
		// Ignored: a measurement's line on the terminal, whose loss costs only the line.
		fmt.Fprintln(os.Stderr, "--engine is required")
		os.Exit(2)
	}
	if runError := run(*engine, *goos, *cgo); runError != nil {
		// Ignored: a measurement's line on the terminal, whose loss costs only the line.
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
}

// run measures the build of goos and cgo on arm64 as the system cache of engine holds it.
func run(engine string, goos string, cgo string) error {
	contents, readError := os.ReadFile(engine)
	if readError != nil {
		return readError
	}
	identityDigest := sha256.Sum256(contents)
	identity := hex.EncodeToString(identityDigest[:])
	root, notKept := cache.SystemCacheRoot()
	if notKept != nil {
		return errors.New(notKept.Reason())
	}
	store := system_cache.Store{Directory: filepath.Join(root, "go", "store")}
	environment := buildEnvironment(goos, cgo)
	listed, listError := goList(environment, "std")
	if listError != nil {
		return listError
	}
	packs, packsError := identifiedPacks(store, identity, listed)
	if packsError != nil {
		return packsError
	}
	if len(packs) == 0 {
		return fmt.Errorf("engine %s kept no pack in %s", identity, store.Directory)
	}
	for _, pack := range packs {
		// Ignored: a measurement's line on the terminal, whose loss costs only the line.
		fmt.Printf("pack %s: %d packages matched, %d records shared by two or more, %d matched none\n", pack.name,
			len(pack.keys), len(pack.ambiguous), pack.unmatched)
	}
	chosen := packs[0]
	// Ignored: a measurement's line on the terminal, whose loss costs only the line.
	fmt.Printf("%s/arm64 cgo=%s is pack %s\n", goos, cgo, chosen.name)
	unresolved := resolveAmbiguous(store, chosen)
	// Ignored: a measurement's line on the terminal, whose loss costs only the line.
	fmt.Printf("records decoded to learn their path: %d resolved, unresolved %v\n", len(chosen.ambiguous)-
		len(unresolved), unresolved)
	return measure(store, environment, chosen)
}

// buildEnvironment is the go environment of the build of goos and cgo on arm64 over the process's, with no
// workspace, flags or toolchain switch.
func buildEnvironment(goos string, cgo string) []string {
	var kept []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains([]string{"GOWORK", "GOFLAGS", "GOOS", "GOARCH", "CGO_ENABLED"}, name) {
			kept = append(kept, entry)
		}
	}
	return append(kept, "GOOS="+goos, "GOARCH=arm64", "CGO_ENABLED="+cgo, "GOTOOLCHAIN=local", "GOPROXY=off",
		"GOFLAGS=", "GOWORK=off")
}

// goList is what `go list -e -json` with arguments says under environment, in a directory of no module.
func goList(environment []string, arguments ...string) ([]listedPackage, error) {
	command := exec.Command("go", append([]string{"list", "-e", "-json"}, arguments...)...)
	command.Dir = os.TempDir()
	command.Env = environment
	command.Stderr = os.Stderr
	output, listError := command.Output()
	if listError != nil {
		return nil, fmt.Errorf("go list %v: %w", arguments, listError)
	}
	var listed []listedPackage
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var one listedPackage
		if decodeError := decoder.Decode(&one); errors.Is(decodeError, io.EOF) {
			return listed, nil
		} else if decodeError != nil {
			return nil, decodeError
		}
		listed = append(listed, one)
	}
}

// signature is a package as the command matches it: its name, then its imports sorted, "C" left out, which a check
// imports as a package of its own and go list does not.
func signature(name string, imports []string) string {
	kept := slices.DeleteFunc(slices.Clone(imports), func(path string) bool { return path == "C" })
	slices.Sort(kept)
	return name + " " + strings.Join(kept, " ")
}

// identifiedPacks is every pack the engine of identity kept in store, its records matched to listed, the best-matched
// first. Packs are read with cache.Store.ReadPack, which reads and writes nothing else.
func identifiedPacks(store system_cache.Store, identity string, listed []listedPackage) ([]identifiedPack, error) {
	bySignature := map[string][]string{}
	for _, one := range listed {
		bySignature[signature(one.Name, one.Imports)] = append(bySignature[signature(one.Name, one.Imports)],
			one.ImportPath)
	}
	directory := filepath.Join(store.Directory, "packs", cache.PackIdentity(identity).String())
	entries, listError := os.ReadDir(directory)
	if listError != nil {
		return nil, listError
	}
	reader := cache.OpenAt(store.Directory, nil)
	var packs []identifiedPack
	for _, entry := range entries {
		place := cache.PackPlace{Identity: cache.PackIdentity(identity)}
		decoded, decodeError := hex.DecodeString(entry.Name())
		if decodeError != nil || len(decoded) != len(place.Name) {
			continue
		}
		copy(place.Name[:], decoded)
		pack, refused := reader.ReadPack(place)
		if refused != "" {
			// Ignored: a measurement's line on the terminal, whose loss costs only the line.
			fmt.Printf("pack %s refused: %s\n", entry.Name(), refused)
			continue
		}
		identified := identifiedPack{name: entry.Name()[:12], keys: map[string]cache.Key{},
			ambiguous: map[cache.Key][]string{}}
		for asked, packed := range pack.Entries {
			paths := candidatePaths(store, packed.Entry, bySignature)
			switch len(paths) {
			case 0:
				identified.unmatched++
			case 1:
				identified.keys[paths[0]] = asked
			default:
				identified.ambiguous[asked] = paths
			}
		}
		packs = append(packs, identified)
	}
	slices.SortStableFunc(packs, func(left identifiedPack, right identifiedPack) int {
		return (len(right.keys) - right.unmatched) - (len(left.keys) - left.unmatched)
	})
	return packs, nil
}

// candidatePaths are the paths of bySignature's packages whose name and imports are those of the package whose
// record is contents, none for a record that does not read.
func candidatePaths(store system_cache.Store, contents []byte, bySignature map[string][]string) []string {
	var record packRecord
	if gob.NewDecoder(bytes.NewReader(contents)).Decode(&record) != nil || record.ExportData == nil {
		return nil
	}
	body := record.Inline
	if body == nil && len(record.BodyAt) == len(cache.Key{}) {
		holder, held := store.Get(cache.Key(record.BodyAt))
		var holding packRecord
		if !held || gob.NewDecoder(bytes.NewReader(holder)).Decode(&holding) != nil {
			return nil
		}
		body = holding.Inline
	}
	var decodedBody packBody
	if body == nil || gob.NewDecoder(bytes.NewReader(body)).Decode(&decodedBody) != nil {
		return nil
	}
	data, stored := exportDataOf(store, record.ExportData)
	if !stored {
		return nil
	}
	named, manifestError := manifest(data, "")
	if manifestError != nil || len(named) == 0 {
		return nil
	}
	return bySignature[signature(named[0].Name, decodedBody.Imports)]
}

// exportDataOf is the export data store keeps under digest, as the engine's check names it.
func exportDataOf(store system_cache.Store, digest []byte) ([]byte, bool) {
	return store.Get(sha256.Sum256(append([]byte("export data by content "), digest...)))
}

// errorManifestRead stops a read of export data once its manifest is read.
var errorManifestRead = errors.New("the manifest is read")

// manifest is every package data, the export data of the package at path, names, its own first.
func manifest(data []byte, path string) ([]export_data.GetPackagesItem, error) {
	var named []export_data.GetPackagesItem
	_, readError := export_data.IImportShallow(token.NewFileSet(), func(items []export_data.GetPackagesItem) error {
		named = slices.Clone(items)
		return errorManifestRead
	}, data, path, func(string, ...any) {
		// Nothing past the manifest is read, so nothing can be reported.
	})
	if !errors.Is(readError, errorManifestRead) {
		return nil, fmt.Errorf("reading a manifest: %w", readError)
	}
	return named, nil
}

// resolveAmbiguous decodes each record of pack that two or more packages could be as each of them in turn, against
// the packages pack's matched keys read, and gives it to the one whose declarations' file sits in that package's
// directory below `GOROOT`, which is how the engine's positions name it. It returns the records it could not resolve.
//
// Mutates: pack its keys gain each record resolved
func resolveAmbiguous(store system_cache.Store, pack identifiedPack) []string {
	var unresolved []string
	for asked, paths := range pack.ambiguous {
		resolved := ""
		for _, candidate := range paths {
			if _, claimed := pack.keys[candidate]; claimed {
				continue
			}
			trialKeys := map[string]cache.Key{candidate: asked}
			fileSet := token.NewFileSet()
			trial := system_cache.NewBuild(store, func(path string) (cache.Key, bool) {
				if key, held := trialKeys[path]; held {
					return key, true
				}
				key, held := pack.keys[path]
				return key, held
			}, fileSet, map[string]*types.Package{})
			present, readError := trial.Read(candidate)
			if readError != nil || !present {
				continue
			}
			if sitsIn(fileSet, trial.Packages[candidate], candidate) {
				resolved = candidate
				break
			}
		}
		if resolved == "" {
			unresolved = append(unresolved, strings.Join(paths, "|"))
			continue
		}
		pack.keys[resolved] = asked
	}
	return unresolved
}

// sitsIn reports whether a package-level object of typed is declared in a file of path's directory below `GOROOT`.
func sitsIn(fileSet *token.FileSet, typed *types.Package, path string) bool {
	for _, name := range typed.Scope().Names() {
		file := filepath.ToSlash(fileSet.Position(typed.Scope().Lookup(name).Pos()).Filename)
		if file != "" {
			return strings.HasSuffix(filepath.Dir(file), "/src/"+path)
		}
	}
	return false
}

// measure times pack's packages read from the system cache against the same paths' compiler export data for the
// build of environment, and prints the times.
func measure(store system_cache.Store, environment []string, pack identifiedPack) error {
	paths := make([]string, 0, len(pack.keys))
	for path := range pack.keys {
		if path != "unsafe" {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)

	started := time.Now()
	blobs := map[string][]byte{}
	for _, path := range paths {
		data, stored := store.ExportData(pack.keys[path])
		if !stored {
			return fmt.Errorf("the system cache holds no export data for %s", path)
		}
		blobs[path] = data
	}
	storeTime := time.Since(started)
	order, orderError := closureOrder(blobs, paths)
	if orderError != nil {
		return orderError
	}

	started = time.Now()
	fileSet := token.NewFileSet()
	closure := map[string]*types.Package{"unsafe": types.Unsafe}
	for _, path := range order {
		if _, decodeError := export_data.Read(fileSet, closure, blobs[path], path); decodeError != nil {
			return fmt.Errorf("decoding %s: %w", path, decodeError)
		}
	}
	decodeTime := time.Since(started)

	started = time.Now()
	whole := system_cache.NewBuild(store, func(path string) (cache.Key, bool) {
		key, held := pack.keys[path]
		return key, held
	}, token.NewFileSet(), map[string]*types.Package{})
	for _, path := range paths {
		present, readError := whole.Read(path)
		if readError != nil {
			return fmt.Errorf("reading %s through system_cache.Build: %w", path, readError)
		}
		if !present {
			return fmt.Errorf("reading %s through system_cache.Build: absent", path)
		}
	}
	buildTime := time.Since(started)

	listed, listError := goList(environment, append([]string{"-export", "-deps"}, paths...)...)
	if listError != nil {
		return listError
	}
	exports := map[string]string{}
	for _, one := range listed {
		exports[one.ImportPath] = one.Export
	}
	universe := gcimporter.NewUniverse()
	compiledSet := token.NewFileSet()
	started = time.Now()
	compiled := make([]*types.Package, 0, len(paths))
	for _, path := range paths {
		typed, importError := gcimporter.Import(compiledSet, universe, path, "",
			func(asked string) (io.ReadCloser, error) {
				file := exports[asked]
				if file == "" {
					return nil, fmt.Errorf("no export data for %q", asked)
				}
				return os.Open(file)
			})
		if importError != nil {
			return fmt.Errorf("importing %s's compiler export data: %w", path, importError)
		}
		compiled = append(compiled, typed)
	}
	importTime := time.Since(started)
	started = time.Now()
	for _, typed := range compiled {
		for _, name := range typed.Scope().Names() {
			if token.IsExported(name) {
				typed.Scope().Lookup(name)
			}
		}
	}
	lookupTime := time.Since(started)

	// Ignored: a measurement's line on the terminal, whose loss costs only the line.
	fmt.Printf("%d packages, every one the pack names, in closure order\n", len(paths))
	// Ignored: a measurement's line on the terminal, whose loss costs only the line.
	fmt.Printf("system cache: %v reading records and export data, %v decoding it whole, %v through Build.Read "+
		"(read, manifest and decode)\n", storeTime, decodeTime, buildTime)
	// Ignored: a measurement's line on the terminal, whose loss costs only the line.
	fmt.Printf("compiler export data, lazy gcimporter: %v imported, %v more to look up every exported object, %v\n",
		importTime, lookupTime, importTime+lookupTime)
	return nil
}

// closureOrder is paths ordered so each comes after every package its export data, of blobs, names, or an error for
// a package it names that blobs lacks.
func closureOrder(blobs map[string][]byte, paths []string) ([]string, error) {
	var order []string
	placed := map[string]bool{"unsafe": true}
	var place func(path string) error
	place = func(path string) error {
		if placed[path] {
			return nil
		}
		placed[path] = true
		data, held := blobs[path]
		if !held {
			return fmt.Errorf("%s is named but not in the pack", path)
		}
		named, manifestError := manifest(data, path)
		if manifestError != nil {
			return manifestError
		}
		for _, item := range named {
			if item.Path != path {
				if placeError := place(item.Path); placeError != nil {
					return placeError
				}
			}
		}
		order = append(order, path)
		return nil
	}
	for _, path := range paths {
		if placeError := place(path); placeError != nil {
			return nil, placeError
		}
	}
	return order, nil
}
