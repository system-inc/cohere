package cache

import (
	"crypto/sha256"
	"fmt"
	"io"
	"iter"
	"maps"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"golang.org/x/tools/go/packages"
)

// FileDigests is the sha256 of each file a run has hashed, by path. The zero value holds none.
type FileDigests struct {
	byPath map[string]fileDigest
}

// Paths are every path digests holds a digest of, in no order.
func (digests FileDigests) Paths() iter.Seq[string] {
	return maps.Keys(digests.byPath)
}

// Digest is the sha256 digests holds of file, and false when it holds none.
func (digests FileDigests) Digest(file string) ([sha256.Size]byte, bool) {
	digest, held := digests.byPath[file]
	return digest, held
}

// filesHashed counts the files this process has hashed for a key, which FilesHashed reads.
var filesHashed atomic.Int64

// FilesHashed is how many files this process has hashed for a key so far, so a test can tell a run that hashes each
// file once from one that hashes some twice.
func FilesHashed() int64 {
	return filesHashed.Load()
}

// fileDigests is the digest of every file of the packages that are not keyed by path alone: known's when it holds
// the file, else hashed, each once, across the cores, since reading and hashing the files is nearly all of Keys'
// work, from held's bytes when held holds the file and from disk otherwise. A file that cannot be read is an error,
// since a key without its bytes would say nothing about it.
func fileDigests(
	reachable []*packages.Package,
	known FileDigests,
	held func(file string) ([]byte, bool),
) (map[string]fileDigest, error) {
	seen := map[string]bool{}
	var files []string
	byPath := map[string]fileDigest{}
	for _, reached := range reachable {
		if pathOnly(reached) {
			continue
		}
		for _, list := range fileLists(reached) {
			for _, file := range list {
				if seen[file] {
					continue
				}
				seen[file] = true
				if digest, hashed := known.byPath[file]; hashed {
					byPath[file] = digest
					continue
				}
				files = append(files, file)
			}
		}
	}
	// Sorted so that of several unreadable files the same one is reported every time.
	slices.Sort(files)

	digests := make([]fileDigest, len(files))
	readErrors := make([]error, len(files))
	indexes := make(chan int)
	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), max(len(files), 1)) {
		workers.Go(func() {
			for index := range indexes {
				digests[index], readErrors[index] = digestOf(files[index], held)
			}
		})
	}
	for index := range files {
		indexes <- index
	}
	close(indexes)
	workers.Wait()

	for index, file := range files {
		if readErrors[index] != nil {
			return nil, readErrors[index]
		}
		byPath[file] = digests[index]
	}
	return byPath, nil
}

// digestOf is the sha256 of one file's bytes: held's when held holds them, and otherwise the disk's, streamed rather
// than read whole.
func digestOf(file string, held func(file string) ([]byte, bool)) (fileDigest, error) {
	if held != nil {
		if contents, isHeld := held(file); isHeld {
			filesHashed.Add(1)
			return sha256.Sum256(contents), nil
		}
	}
	opened, openError := os.Open(file)
	if openError != nil {
		return fileDigest{}, fmt.Errorf("hashing %s: %w", file, openError)
	}
	defer func() {
		// Ignored: the file was only read, and every byte of it hashed or the copy failed.
		_ = opened.Close()
	}()
	filesHashed.Add(1)
	digester := sha256.New()
	if _, copyError := io.Copy(digester, opened); copyError != nil {
		return fileDigest{}, fmt.Errorf("hashing %s: %w", file, copyError)
	}
	var digest fileDigest
	copy(digest[:], digester.Sum(nil))
	return digest, nil
}
