// Command generate records the upstream text every copy in the module is of (internal/go_language/copied) into
// internal/go_language/copied/upstream_text/testdata/upstream, so the test that holds each copy to upstream
// (internal/go_language/copied/every_copy) reads only the repository. Run it from the module root once a copy is
// added or bumped, naming where upstream's source lives:
//
//	go run ./internal/go_language/copied/upstream_text/tools/generate \
//		-module-cache <directory> -toolchain-root <directory>
//
// -module-cache is the module cache (`go env GOMODCACHE`) and -toolchain-root the root of a Go distribution whose
// source a std copy is of, `LICENSE` and `VERSION` in it as the release archives ship them; Homebrew's root keeps
// LICENSE a directory up, so name a distribution's own, as the module cache's golang.org/toolchain holds them.
//
// It writes each upstream file a copy names at <module>@<version>/<path>, byte for byte, each upstream source's
// LICENSE once beside its files, and SHA256SUMS, the digest of every file it wrote, in sha256sum's form. It deletes
// nothing: a recorded file no copy names any more is left for the test to name, and for whoever bumped the copy to
// delete.
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_language/copied"
	"golang.org/x/mod/module"
)

// recordedDirectory is where the recorded text goes, from the module root.
const recordedDirectory = "internal/go_language/copied/upstream_text/testdata/upstream"

// skippedDirectories are what the walk leaves out, as the test's walk does: the TypeScript submodule, npm's
// node_modules, and testdata, whose planted copies name nothing that needs recording.
var skippedDirectories = []string{"TypeScript", "node_modules", "testdata"}

func main() {
	moduleCache := flag.String("module-cache", "", "the module cache's directory, as go env GOMODCACHE prints it")
	toolchainRoot := flag.String("toolchain-root", "", "the root of the Go distribution a std copy is of")
	flag.Parse()
	if *moduleCache == "" || *toolchainRoot == "" {
		exitWith(fmt.Errorf("name both -module-cache and -toolchain-root"))
	}

	headers, walkError := copyHeaders(".")
	if walkError != nil {
		exitWith(walkError)
	}
	// digests is each recorded file's sha256 by its path below recordedDirectory.
	digests := map[string][sha256.Size]byte{}
	for _, header := range headers {
		source := header.Module + "@" + header.Version
		upstreamRoot, rootError := sourceRoot(header, *moduleCache, *toolchainRoot)
		if rootError != nil {
			exitWith(rootError)
		}
		if _, licensed := digests[source+"/LICENSE"]; !licensed {
			// A std copy's LICENSE sits at the distribution's root, one above the src its files are read from.
			licenseRoot := upstreamRoot
			if header.Module == copied.StandardLibrary {
				licenseRoot = filepath.Dir(upstreamRoot)
			}
			digest, recordError := record(filepath.Join(licenseRoot, "LICENSE"), source+"/LICENSE")
			if recordError != nil {
				exitWith(recordError)
			}
			digests[source+"/LICENSE"] = digest
		}
		recordedPath := source + "/" + header.Path
		if _, recorded := digests[recordedPath]; recorded {
			continue
		}
		digest, recordError := record(filepath.Join(upstreamRoot, filepath.FromSlash(header.Path)), recordedPath)
		if recordError != nil {
			exitWith(recordError)
		}
		digests[recordedPath] = digest
	}

	var sums strings.Builder
	for _, recordedPath := range slices.Sorted(maps.Keys(digests)) {
		fmt.Fprintf(&sums, "%x  %s\n", digests[recordedPath], recordedPath)
	}
	sumsPath := filepath.Join(filepath.FromSlash(recordedDirectory), "SHA256SUMS")
	if writeError := os.WriteFile(sumsPath, []byte(sums.String()), 0o644); writeError != nil {
		exitWith(writeError)
	}
	if _, printError := fmt.Printf("wrote %s, %d files\n", sumsPath, len(digests)); printError != nil {
		exitWith(fmt.Errorf("saying %s was written: %w", sumsPath, printError))
	}
}

// copyHeaders walks root for every Go file outside testdata that declares itself a copy and returns its header. A
// header that breaks the grammar fails the run, since what it copies cannot be read from it.
func copyHeaders(root string) ([]copied.Header, error) {
	var headers []copied.Header
	walkError := filepath.WalkDir(root, func(walkedPath string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() {
			if walkedPath != root && (strings.HasPrefix(entry.Name(), ".") ||
				slices.Contains(skippedDirectories, entry.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".go" {
			return nil
		}
		file, parseError := parser.ParseFile(token.NewFileSet(), walkedPath, nil,
			parser.PackageClauseOnly|parser.ParseComments)
		if parseError != nil {
			return parseError
		}
		header, declared, readError := copied.Read(file)
		if readError != nil {
			return fmt.Errorf("%s: %w", walkedPath, readError)
		}
		if declared {
			headers = append(headers, header)
		}
		return nil
	})
	return headers, walkError
}

// sourceRoot is the directory header's path is below: the module's in the module cache, in the cache's escaped
// form of its path and version, or for a std copy the distribution's src. A std copy is read from whatever release
// the distribution is, which the run says when it is not the copy's, since the two may ship the file alike.
func sourceRoot(header copied.Header, moduleCache string, toolchainRoot string) (string, error) {
	if header.Module == copied.StandardLibrary {
		release, readError := os.ReadFile(filepath.Join(toolchainRoot, "VERSION"))
		if readError != nil {
			return "", fmt.Errorf("reading the toolchain's release: %w", readError)
		}
		line, _, _ := strings.Cut(string(release), "\n")
		if line != header.Version {
			if _, printError := fmt.Printf("recording %s@%s %s from %s's source: check the two ship it alike\n",
				header.Module, header.Version, header.Path, line); printError != nil {
				return "", fmt.Errorf("saying the toolchain is another release: %w", printError)
			}
		}
		return filepath.Join(toolchainRoot, "src"), nil
	}
	escapedPath, pathError := module.EscapePath(header.Module)
	if pathError != nil {
		return "", fmt.Errorf("escaping %s for the module cache: %w", header.Module, pathError)
	}
	escapedVersion, versionError := module.EscapeVersion(header.Version)
	if versionError != nil {
		return "", fmt.Errorf("escaping %s for the module cache: %w", header.Version, versionError)
	}
	return filepath.Join(moduleCache, escapedPath+"@"+escapedVersion), nil
}

// record copies the file at from to recordedPath below recordedDirectory and returns its digest.
func record(from string, recordedPath string) ([sha256.Size]byte, error) {
	contents, readError := os.ReadFile(from)
	if readError != nil {
		return [sha256.Size]byte{}, fmt.Errorf("reading upstream's %s: %w", recordedPath, readError)
	}
	to := filepath.Join(filepath.FromSlash(recordedDirectory), filepath.FromSlash(path.Clean(recordedPath)))
	if createDirectoryError := os.MkdirAll(filepath.Dir(to), 0o755); createDirectoryError != nil {
		return [sha256.Size]byte{}, createDirectoryError
	}
	if writeError := os.WriteFile(to, contents, 0o644); writeError != nil {
		return [sha256.Size]byte{}, writeError
	}
	return sha256.Sum256(contents), nil
}

// exitWith says why the run failed, on standard error, and exits 1.
func exitWith(failure error) {
	// Ignored: the exit code below says the run failed even when standard error cannot.
	fmt.Fprintln(os.Stderr, failure)
	os.Exit(1)
}
