package gotypes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A Product is the export data of every package a set of tests reads that is outside the module it
// inspects: the standard library, the TypeScript checker and its shims, the modules they need. Export data
// comes only from compiling, so a product is built once where building is allowed (Workshop, or the first
// test of a local `go test`) and found by key everywhere else; a test that reads it never builds it.
//
// It lives in adamic's build cache, as a product of adamic's internal/buildcache does, so Loom carries it
// from Workshop to a runner the same way: a directory named by its key under ADAMIC_BUILD_CACHE_DIR (or the
// user cache directory's adamic-build), its description beside it as <key>.inputs, and a census line in
// ADAMIC_BUILD_LOG when that is set.
//
// The key is a hash of what changes the export data and nothing else (#t37sw0f): the Go release, the target
// platform, GOEXPERIMENT, and for each package its import path and the files go list selects for it, by name
// for a standard package (the release fixes their contents), by module version for a module from the module
// cache (go.sum fixes theirs), and by repository-relative path and content for a package in the repository,
// with its module's go.mod. Never the -p a machine computes, nor any absolute path, nor which go.work file
// pointed at the tree, so a tree keys the same on every machine that has it. Build tags, CGO_ENABLED and a
// GOAMD64 level change export data only by changing which files go list selects, and those files are keyed.
type Product struct {
	// Name names it in the census line, its description and every message.
	Name string

	// Test is the TestProduct_ test that builds it, named when a reader cannot find it.
	Test string

	// Directory is where go list runs: the root of the module the tests inspect.
	Directory string

	// Patterns are the packages its readers load. The product holds every package outside the module that
	// they import, directly or not.
	Patterns []string

	// Environment is added to every go command's environment.
	Environment []string
}

// Exports is a found product: the export data file of each package it holds.
type Exports struct {
	name     string
	key      string
	patterns []string
	files    map[string]string
}

// Key is the product's key.
func (exports *Exports) Key() string { return exports.key }

// importer reads the product's export data. It is not safe for concurrent use.
func (exports *Exports) importer(fileSet *token.FileSet) types.Importer {
	return importer.ForCompiler(fileSet, "gc", func(path string) (io.ReadCloser, error) {
		file := exports.files[path]
		if file == "" {
			return nil, fmt.Errorf("the %s product (%s) holds no export data for %s", exports.name, exports.key[:12], path)
		}
		return os.Open(file)
	})
}

// productFormat changes whenever what a product holds or how it is keyed does, retiring every older one.
const productFormat = "cohere gotypes product v1"

// manifestName is the product's own index: each package's import path to its export data file.
const manifestName = "manifest.json"

type manifest struct {
	Name     string
	Packages map[string]string
}

// productState is everything Find and Build compute before they look in the cache.
type productState struct {
	key, description string
	cache            string
	listed           *listing
	exported         map[string]bool
}

func (product Product) state() (*productState, error) {
	root, sourceModule, err := moduleAt(product.Directory)
	if err != nil {
		return nil, err
	}
	listed, err := list(product.Directory, product.Environment, product.Patterns)
	if err != nil {
		return nil, err
	}
	for _, listedPackage := range listed.packages {
		if listedPackage.Error != nil {
			return nil, fmt.Errorf("listing the %s product's packages: %s: %s", product.Name, listedPackage.ImportPath, listedPackage.Error.Err)
		}
	}
	environment, err := goEnvironmentValues(product.Directory, product.Environment)
	if err != nil {
		return nil, err
	}
	exported := exportSet(listed, sourceModule)
	key, description, err := productKey(product, root, environment, listed, exported)
	if err != nil {
		return nil, err
	}
	cache, err := cacheDirectory()
	if err != nil {
		return nil, err
	}
	return &productState{key: key, description: description, cache: cache, listed: listed, exported: exported}, nil
}

// productKey is the product's key and its description, one line per keyed fact, from a listing: a pure
// function of what go list and go env said, and of the repository's files, so the same tree keys the same
// wherever it sits.
func productKey(product Product, root string, environment map[string]string, listed *listing, exported map[string]bool) (string, string, error) {
	hash := sha256.New()
	field := func(writer io.Writer, kind, value string) {
		fmt.Fprintf(writer, "%s %d\n%s\n", kind, len(value), value)
	}
	lines := []string{
		"name go types " + product.Name,
		"patterns " + strings.Join(product.Patterns, " "),
		fmt.Sprintf("go %s %s/%s GOEXPERIMENT=%s", environment["GOVERSION"], environment["GOOS"], environment["GOARCH"], environment["GOEXPERIMENT"]),
	}
	field(hash, "format", productFormat)
	field(hash, "name", product.Name)
	field(hash, "go", environment["GOVERSION"])
	field(hash, "target", environment["GOOS"]+"/"+environment["GOARCH"])
	field(hash, "experiment", environment["GOEXPERIMENT"])

	paths := make([]string, 0, len(exported))
	for path := range exported {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	relative := func(path string) (string, error) {
		within, err := filepath.Rel(root, path)
		if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
			return "", fmt.Errorf("%s is outside the repository at %s, so nothing can key it the same on another machine", path, root)
		}
		return filepath.ToSlash(within), nil
	}
	contentHash := func(path string) (string, error) {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(content)
		return hex.EncodeToString(sum[:]), nil
	}
	for _, path := range paths {
		listedPackage := listed.byPath[path]
		packageHash := sha256.New()
		field(packageHash, "package", path)
		field(packageHash, "files", strings.Join(append(append([]string{}, listedPackage.GoFiles...), listedPackage.CgoFiles...), " "))
		origin := "standard"
		if !listedPackage.Standard && path != "unsafe" {
			module := listedPackage.Module
			if module == nil {
				return "", "", fmt.Errorf("%s is in no module and not in the standard library, so it cannot be keyed", path)
			}
			modulePath, version, directory := module.Path, module.Version, module.Dir
			if module.Replace != nil {
				modulePath, version = module.Replace.Path, module.Replace.Version
				if module.Replace.Dir != "" {
					directory = module.Replace.Dir
				}
			}
			if version != "" {
				origin = module.Path + " " + modulePath + "@" + version
				field(packageHash, "module", origin)
			} else {
				moduleDirectory, err := relative(directory)
				if err != nil {
					return "", "", fmt.Errorf("%s: %w", path, err)
				}
				origin = module.Path + " " + moduleDirectory
				field(packageHash, "module", origin)
				goMod := filepath.Join(directory, "go.mod")
				sum, err := contentHash(goMod)
				if err != nil {
					return "", "", err
				}
				field(packageHash, "go.mod", sum)
				for _, name := range append(append([]string{}, listedPackage.GoFiles...), listedPackage.CgoFiles...) {
					file := filepath.Join(listedPackage.Dir, name)
					within, err := relative(file)
					if err != nil {
						return "", "", fmt.Errorf("%s: %w", path, err)
					}
					sum, err := contentHash(file)
					if err != nil {
						return "", "", err
					}
					field(packageHash, "file", within+" "+sum)
				}
			}
		}
		packageSum := hex.EncodeToString(packageHash.Sum(nil))
		field(hash, "package", packageSum)
		lines = append(lines, fmt.Sprintf("package %s %s %s", path, origin, packageSum[:12]))
	}
	return hex.EncodeToString(hash.Sum(nil)), strings.Join(lines, "\n") + "\n", nil
}

// cacheDirectory is adamic's build cache: ADAMIC_BUILD_CACHE_DIR, or the user cache directory's adamic-build.
func cacheDirectory() (string, error) {
	directory := os.Getenv("ADAMIC_BUILD_CACHE_DIR")
	if directory == "" {
		user, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		directory = filepath.Join(user, "adamic-build")
	}
	return directory, os.MkdirAll(directory, 0o755)
}

// Find returns the product from the build cache, and never builds it: building compiles. A product that is
// not there fails by name, with its key, its description, and any product of the same name the cache holds
// under another key and the first fact the two keyed differently.
func (product Product) Find() (*Exports, error) {
	started := time.Now()
	state, err := product.state()
	if err != nil {
		return nil, err
	}
	exports, err := open(product, state)
	if errors.Is(err, fs.ErrNotExist) {
		// The description's first lines say what was keyed; one line per package follows, which otherKeys
		// compares against any product of this name the cache does hold.
		lines := strings.SplitAfterN(state.description, "\n", 4)
		return nil, fmt.Errorf("the %s product %s is not in %s: it is the export data of the %d packages outside the module "+
			"that %s import, and a test that reads it never builds it, since building compiles. %s builds it: on Workshop "+
			"before the tests run, and locally as the first test of a plain `go test` of its package.%s\nIt keyed:\n%s",
			product.Name, state.key[:12], state.cache, len(state.exported), strings.Join(product.Patterns, " "), product.Test,
			otherKeys(product, state), strings.Join(lines[:min(3, len(lines))], ""))
	}
	if err != nil {
		return nil, err
	}
	record(product, state.key, "hit", started)
	return exports, nil
}

// otherKeys names each product of the same name the cache holds under another key, with the first line its
// description and this one's differ on: a key a runner computed differently from Workshop's, named.
func otherKeys(product Product, state *productState) string {
	descriptions, err := filepath.Glob(filepath.Join(state.cache, "*.inputs"))
	if err != nil {
		return ""
	}
	wanted := strings.Split(state.description, "\n")
	var found []string
	for _, path := range descriptions {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		if lines[0] != wanted[0] {
			continue
		}
		key := strings.TrimSuffix(filepath.Base(path), ".inputs")
		difference := "the same description"
		for index := 0; index < len(lines) || index < len(wanted); index++ {
			theirs, ours := "", ""
			if index < len(lines) {
				theirs = lines[index]
			}
			if index < len(wanted) {
				ours = wanted[index]
			}
			if theirs != ours {
				difference = fmt.Sprintf("%q where this tree has %q", theirs, ours)
				break
			}
		}
		found = append(found, fmt.Sprintf("\nThe cache holds it under %s instead, which keyed %s.", key[:min(12, len(key))], difference))
	}
	return strings.Join(found, "")
}

// open reads a product's manifest and checks every file it names is there.
func open(product Product, state *productState) (*Exports, error) {
	directory := filepath.Join(state.cache, state.key)
	content, err := os.ReadFile(filepath.Join(directory, manifestName))
	if err != nil {
		return nil, err
	}
	var read manifest
	if err := json.Unmarshal(content, &read); err != nil {
		return nil, fmt.Errorf("the %s product %s's manifest: %w", product.Name, state.key[:12], err)
	}
	exports := &Exports{name: product.Name, key: state.key, patterns: product.Patterns, files: map[string]string{}}
	for path, file := range read.Packages {
		absolute := filepath.Join(directory, filepath.FromSlash(file))
		if _, err := os.Stat(absolute); err != nil {
			return nil, fmt.Errorf("the %s product %s names %s for %s, and it is not there: the product is incomplete: %w", product.Name, state.key[:12], file, path, err)
		}
		exports.files[path] = absolute
	}
	for path := range state.exported {
		if path != "unsafe" && exports.files[path] == "" {
			return nil, fmt.Errorf("the %s product %s holds no export data for %s, which its key covers: the product is incomplete", product.Name, state.key[:12], path)
		}
	}
	return exports, nil
}

// Build builds the product if the cache does not hold it, and returns it: for its TestProduct_ test, run
// where building is allowed. ADAMIC_BUILD_CACHE=off builds it again even when the cache holds it, and
// replaces it, the uncached proof mode adamic's buildcache has.
func (product Product) Build() (*Exports, error) {
	started := time.Now()
	state, err := product.state()
	if err != nil {
		return nil, err
	}
	uncached := os.Getenv("ADAMIC_BUILD_CACHE") == "off"
	if !uncached {
		if exports, err := open(product, state); err == nil {
			record(product, state.key, "hit", started)
			return exports, nil
		}
	}
	scratch, err := os.MkdirTemp(state.cache, ".building-"+state.key[:12]+"-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	if err := product.compile(state, scratch); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(state.cache, state.key+".inputs"), []byte(state.description), 0o644); err != nil {
		return nil, err
	}
	destination := filepath.Join(state.cache, state.key)
	if uncached {
		if _, err := os.Stat(destination); err == nil {
			retired := scratch + "-retired"
			if err := os.Rename(destination, retired); err != nil {
				return nil, err
			}
			defer os.RemoveAll(retired)
		}
	}
	// The product appears whole, by one rename. A builder that lost the race to another finds the winner's.
	if err := os.Rename(scratch, destination); err != nil {
		if _, statError := os.Stat(filepath.Join(destination, manifestName)); statError != nil {
			return nil, err
		}
	}
	exports, err := open(product, state)
	if err != nil {
		return nil, err
	}
	record(product, state.key, "miss", started)
	return exports, nil
}

// compile has go list compile the export data of every package the product holds, and copies each file
// into directory with the manifest naming them. It is the only thing here that compiles.
func (product Product) compile(state *productState, directory string) error {
	// The packages the source packages import from outside, and any named package outside: with -deps they
	// reach every package the product holds and none of the source module's, so nothing of it is compiled.
	frontier := map[string]bool{}
	for _, listedPackage := range state.listed.packages {
		if state.exported[listedPackage.ImportPath] {
			if !listedPackage.DepOnly {
				frontier[listedPackage.ImportPath] = true
			}
			continue
		}
		for _, imported := range listedPackage.Imports {
			if imported = canonical(listedPackage, imported); state.exported[imported] {
				frontier[imported] = true
			}
		}
	}
	patterns := make([]string, 0, len(frontier))
	for path := range frontier {
		patterns = append(patterns, path)
	}
	sort.Strings(patterns)
	// -trimpath, so the export data's positions name no checkout path and the product's bytes are the same
	// from any checkout.
	output, err := goCommand(product.Directory, product.Environment, append([]string{"list", "-export", "-trimpath", "-deps", "-json=ImportPath,Export,Error"}, patterns...)...)
	if err != nil {
		return fmt.Errorf("building the %s product: %w", product.Name, err)
	}
	exportFiles := map[string]string{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var listed struct {
			ImportPath string
			Export     string
			Error      *struct{ Err string }
		}
		if err := decoder.Decode(&listed); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return err
		}
		if listed.Error != nil {
			return fmt.Errorf("building the %s product: %s: %s", product.Name, listed.ImportPath, listed.Error.Err)
		}
		if !state.exported[listed.ImportPath] {
			return fmt.Errorf("building the %s product, go list reached %s, which the key does not cover", product.Name, listed.ImportPath)
		}
		exportFiles[listed.ImportPath] = listed.Export
	}
	paths := make([]string, 0, len(state.exported))
	for path := range state.exported {
		if path != "unsafe" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	written := manifest{Name: product.Name, Packages: map[string]string{}}
	if err := os.MkdirAll(filepath.Join(directory, "exports"), 0o755); err != nil {
		return err
	}
	for index, path := range paths {
		if exportFiles[path] == "" {
			return fmt.Errorf("building the %s product, go list compiled no export data for %s", product.Name, path)
		}
		content, err := os.ReadFile(exportFiles[path])
		if err != nil {
			return err
		}
		if content, err = exportDataOnly(content); err != nil {
			return fmt.Errorf("building the %s product, %s's export file %s: %w", product.Name, path, exportFiles[path], err)
		}
		name := fmt.Sprintf("exports/%04d.a", index)
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(name)), content, 0o644); err != nil {
			return err
		}
		written.Packages[path] = name
	}
	encoded, err := json.MarshalIndent(written, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, manifestName), append(encoded, '\n'), 0o644)
}

// exportDataOnly is a compiled package's archive cut to its export data: the archive's __.PKGDEF member,
// which is all go/importer reads. The object code beside it is the archive's bulk (215 MB of the checker's
// 216 MB, measured Oct 10) and no type checker reads it.
func exportDataOnly(archive []byte) ([]byte, error) {
	const magic, headerSize = "!<arch>\n", 60
	if !bytes.HasPrefix(archive, []byte(magic)) {
		return nil, errors.New("it is not an archive")
	}
	offset := len(magic)
	for offset+headerSize <= len(archive) {
		header := archive[offset : offset+headerSize]
		var size int
		if _, err := fmt.Sscan(string(bytes.TrimSpace(header[48:58])), &size); err != nil || size < 0 || offset+headerSize+size > len(archive) {
			return nil, fmt.Errorf("its member at %d has a malformed size", offset)
		}
		if string(bytes.TrimSpace(header[:16])) == "__.PKGDEF" {
			member := archive[offset : offset+headerSize+size]
			cut := append(append([]byte(magic), member...), make([]byte, size&1)...)
			if size&1 == 1 {
				cut[len(cut)-1] = '\n'
			}
			return cut, nil
		}
		offset += headerSize + size + size&1
	}
	return nil, errors.New("it holds no __.PKGDEF member")
}

// record writes the census line adamic's buildcache writes for each product it uses, 'build <name> <key12>
// hit|miss <seconds>', to ADAMIC_BUILD_LOG when that is set: Loom's builder reads it to know which products
// a product test made, and carries those to the runners.
func record(product Product, key, outcome string, started time.Time) {
	path := os.Getenv("ADAMIC_BUILD_LOG")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "build %s %s %s %.2f\n", strings.ReplaceAll("go types "+product.Name, " ", "_"), key[:12], outcome, time.Since(started).Seconds())
}
