package format

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// WriteRoot is the directory a run may write under: every write the engine makes of a project file is of a file whose
// real path, every symbolic link resolved, is the root or under it. A file reached through a link to somewhere
// outside, a linked vendor or shared directory, is read and checked as any other, and never written, so a fix of one
// project never rewrites another (#hwz114g: a linked `TypeScript` led a fix to rewrite a shared checkout).
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

// NewWriteRoot is the WriteRoot of root, made absolute and resolved through every symbolic link, so a file is
// judged by where it really is against where the root really is.
func NewWriteRoot(root string) (WriteRoot, error) {
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
// rename asks this of every file a plan reads. A file that cannot be resolved is an error, and never inside.
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
		return realPath, writeRoot.holds(realPath), nil
	}
	directory, resolveError := writeRoot.realDirectory(filepath.Dir(file))
	if resolveError != nil {
		return "", false, resolveError
	}
	realPath = filepath.Join(directory, filepath.Base(file))
	return realPath, writeRoot.holds(realPath), nil
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

// holds is whether realPath, already resolved, is the root or under it, by its path relative to the root rather
// than by a prefix of its spelling, since `/a/bc` is not under `/a/b`. The zero WriteRoot holds nothing.
func (writeRoot WriteRoot) holds(realPath string) bool {
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
