// Package product_store_manifest is the manifest of one product in Loom's artifacts store, the format
// two programs share: Loom's builder writes it (builder/held.go, HeldProducts.prepare) and Adamic's
// buildcache reads it (internal/buildcache/store.go, fetch).
//
// A product is a manifest blob naming one blob per file, and refs/build/<key> holds the sha256 of the
// manifest's encoded bytes. So the encoding is the format: a byte that moves moves every product's sum.
// Loom is the only writer, so Encode writes Loom's bytes exactly, and Decode reads what either side has
// written. The goldens in testdata are those bytes, and its tests hold Encode and Decode to them.
//
// It holds the manifest and nothing about where one lives. The paths refs/build/<key>,
// refs/build-candidate/<key> and blobs/<sha256> are loom/protocol's, with the product key's pattern and
// who may write each namespace, since they are Loom's knowledge of its store and not the format's.
//
// It is a module of its own because Adamic and Loom import it by reference, and Go forbids importing
// cohere's internal/ from another module.
package product_store_manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Version is the manifest's version, the only one written or read.
const Version = 1

// Manifest is one product: its key and every file it holds.
//
// The fields, their JSON names and their order are the format, the same as both sides' structs had
// before this module (adamic's manifest, loom's heldManifest). None is omitted when empty, so a product
// with no files encodes "files":[] and one with no name "name":"".
type Manifest struct {
	Version int    `json:"version"`
	Key     string `json:"key"`
	// Name is a product's readable name. Loom writes it empty and no reader uses it; it stays because
	// dropping it would move every manifest's bytes, and so every product's sum.
	Name  string `json:"name"`
	Files []File `json:"files"`
}

// File is one file of a product.
type File struct {
	// Path is relative to the product's root, slash separated and clean.
	Path string `json:"path"`
	// Sha256 is the hex sha256 of the file's content, which names its blob. Both sides spelled the Go
	// name SHA256; the bytes see only the tag.
	Sha256 string `json:"sha256"`
	// Size is the content's length in bytes.
	Size int64 `json:"size"`
	// Mode is 0o755 or 0o644: Mode's rule, which keeps only whether anyone may execute it.
	Mode uint32 `json:"mode"`
}

// Mode is 0o755 for a file any execute bit is set on, else 0o644. A product keeps only that much of a
// file's mode, so the same build on boxes with different umasks gives the same manifest.
func Mode(mode fs.FileMode) uint32 {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

// Describe is the manifest of the product at root: every regular file by slash path, sha256, length and
// Mode, sorted by path. Files is never nil, so an empty product encodes as Loom's does.
//
// Anything else in a product (a symlink, a device) can't be stored, so it is an error, and so is a root
// that isn't a directory, whose one file would be named "." and refused by Decode. A missing root is an
// error wrapping fs.ErrNotExist. Beside any error is the empty product's manifest, never a partial one:
// Loom reads a missing root as a product with no files, and keeps that manifest.
func Describe(key, name, root string) (Manifest, error) {
	product := Manifest{Version: Version, Key: key, Name: name, Files: []File{}}
	walkError := filepath.WalkDir(root, func(file string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if file == root && !entry.IsDir() {
			return fmt.Errorf("%s is not a directory, so it isn't a product", root)
		}
		if entry.IsDir() {
			return nil
		}
		info, infoError := entry.Info()
		if infoError != nil {
			return infoError
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file, so the product can't be stored", file)
		}
		content, readError := os.ReadFile(file)
		if readError != nil {
			return readError
		}
		relative, relativeError := filepath.Rel(root, file)
		if relativeError != nil {
			return relativeError
		}
		sum := sha256.Sum256(content)
		// The length read, not info.Size(): the size the manifest names is the blob's, the bytes hashed.
		product.Files = append(product.Files, File{
			Path:   filepath.ToSlash(relative),
			Sha256: hex.EncodeToString(sum[:]),
			Size:   int64(len(content)),
			Mode:   Mode(info.Mode()),
		})
		return nil
	})
	if walkError != nil {
		return Manifest{Version: Version, Key: key, Name: name, Files: []File{}}, walkError
	}
	// WalkDir's order is lexical per directory, which isn't the paths' order: "a.txt" sorts before
	// "a/b", but the walk visits a/ first.
	slices.SortFunc(product.Files, func(left, right File) int { return strings.Compare(left.Path, right.Path) })
	return product, nil
}

// Encode is the manifest's bytes as Loom writes them, the bytes whose sha256 a refs/build ref names:
// json.Marshal of the manifest, with no trailing newline. Nil Files encodes as [], not null, because
// Loom's writer starts its list empty, so no caller can write an empty product another way.
func (manifest Manifest) Encode() ([]byte, error) {
	if manifest.Files == nil {
		manifest.Files = []File{}
	}
	return json.Marshal(manifest)
}

// Decode reads a manifest blob fetched for key. It refuses one that isn't version Version for key, or
// that names a path a reader would write outside its product: one that isn't clean, is absolute, is the
// product's root itself, or climbs out of it.
//
// These are buildcache's checks on read, moved here because every manifest Loom writes passes them: its
// paths are a walk's below a directory, relative to it, so always clean and inside. A reader that
// adopts Decode refuses nothing Loom writes.
//
// Files is never nil: a null list, as Adamic's removed publisher wrote for an empty product, reads as the
// empty list Loom writes. Name is read as written and never checked, since no reader uses it. What
// Decode can't see is its caller's to check: that each file's blob hashes to its Sha256 and is Size
// bytes long.
func Decode(content []byte, key string) (Manifest, error) {
	var product Manifest
	if decodeError := json.Unmarshal(content, &product); decodeError != nil {
		return Manifest{}, fmt.Errorf("the manifest doesn't decode: %w", decodeError)
	}
	if product.Version != Version || product.Key != key {
		return Manifest{}, fmt.Errorf("the manifest is version %d for key %s, asked for version %d for %s",
			product.Version, product.Key, Version, key)
	}
	for _, file := range product.Files {
		clean := path.Clean(file.Path)
		if file.Path != clean || path.IsAbs(clean) || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") {
			return Manifest{}, fmt.Errorf("the manifest names the path %q, which isn't clean and inside its product",
				file.Path)
		}
	}
	if product.Files == nil {
		product.Files = []File{}
	}
	return product, nil
}
