// Package write_root is where a fix may write: the run's root, judged by real paths, every symbolic link resolved. Each
// engine that writes a project's files asks it before it writes one, so a file reached through a link to somewhere
// outside the root, a linked vendor or shared directory, is read and checked as any other and never written, and a
// fix of one project never rewrites another (#hwz114g: a linked `TypeScript` led a Go fix to rewrite a shared
// checkout).
package write_root

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// WriteRoot is the directory a run may write under: a file may be written only when its real path is the root or
// under it.
//
// The zero WriteRoot refuses every write, so a caller that never said where the run may write writes nothing.
type WriteRoot struct {
	// resolved is the root's real path, empty in the zero WriteRoot.
	resolved string

	// directories are the real paths of the directories Inside has resolved, shared by every copy of the WriteRoot,
	// nil in the zero one.
	directories *resolvedDirectories
}

// resolvedDirectories are directories' real paths by their paths as given, which Inside asks from any goroutine.
type resolvedDirectories struct {
	lock  sync.Mutex
	known map[string]string
}

// New is the WriteRoot of root, made absolute and resolved through every symbolic link, so a file is judged by where
// it really is against where the root really is.
func New(root string) (WriteRoot, error) {
	absolute, absoluteError := filepath.Abs(root)
	if absoluteError != nil {
		return WriteRoot{}, fmt.Errorf("resolving the root %s: %w", root, absoluteError)
	}
	resolved, resolveError := filepath.EvalSymlinks(absolute)
	if resolveError != nil {
		return WriteRoot{}, fmt.Errorf("resolving the root %s: %w", root, resolveError)
	}
	return WriteRoot{resolved: resolved, directories: &resolvedDirectories{known: map[string]string{}}}, nil
}

// Inside is file's real path, every symbolic link resolved, and whether that is the root or under it. A file that is
// itself a link is resolved on its own, and every other file through its directory, whose real path is kept, since a
// caller may ask this of every file a run reads. A file that cannot be resolved is an error, and never inside.
func (writeRoot WriteRoot) Inside(file string) (realPath string, inside bool, resolveError error) {
	information, statError := os.Lstat(file)
	if statError != nil {
		return "", false, fmt.Errorf("resolving %s: %w", file, statError)
	}
	if information.Mode()&os.ModeSymlink != 0 {
		realPath, resolveError = filepath.EvalSymlinks(file)
		if resolveError != nil {
			return "", false, fmt.Errorf("resolving %s: %w", file, resolveError)
		}
		return realPath, writeRoot.Holds(realPath), nil
	}
	directory, resolveError := writeRoot.realDirectory(filepath.Dir(file))
	if resolveError != nil {
		return "", false, resolveError
	}
	realPath = filepath.Join(directory, filepath.Base(file))
	return realPath, writeRoot.Holds(realPath), nil
}

// Holds is whether realPath, a path the caller already resolved through every symbolic link, is the root or under
// it, by its path relative to the root rather than by a prefix of its spelling, since `/a/bc` is not under `/a/b`. A
// writer that resolves its target anyway asks this rather than Inside, and pays nothing more. The zero WriteRoot
// holds nothing.
func (writeRoot WriteRoot) Holds(realPath string) bool {
	if writeRoot.resolved == "" {
		return false
	}
	relative, relativeError := filepath.Rel(writeRoot.resolved, realPath)
	if relativeError != nil {
		return false
	}
	first, _, _ := strings.Cut(filepath.ToSlash(relative), "/")
	return first != ".." && !filepath.IsAbs(relative)
}

// realDirectory is directory's real path, resolved once for the WriteRoot and its copies.
func (writeRoot WriteRoot) realDirectory(directory string) (string, error) {
	if writeRoot.directories == nil {
		return resolveDirectory(directory)
	}
	writeRoot.directories.lock.Lock()
	defer writeRoot.directories.lock.Unlock()
	if resolved, known := writeRoot.directories.known[directory]; known {
		return resolved, nil
	}
	resolved, resolveError := resolveDirectory(directory)
	if resolveError != nil {
		return "", resolveError
	}
	writeRoot.directories.known[directory] = resolved
	return resolved, nil
}

// resolveDirectory is directory's real path.
func resolveDirectory(directory string) (string, error) {
	resolved, resolveError := filepath.EvalSymlinks(directory)
	if resolveError != nil {
		return "", fmt.Errorf("resolving %s: %w", directory, resolveError)
	}
	return resolved, nil
}
