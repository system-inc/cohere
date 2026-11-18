package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

// systemCacheVariable names the environment variable that puts the system cache in a directory of the person's
// choosing, spelled as the environment spells it.
const systemCacheVariable = "COHERE_SYSTEM_CACHE"

// SystemCacheRoot is the directory the system cache lives in, kept across projects and runs: `$COHERE_SYSTEM_CACHE`
// when it is set, otherwise the user's cache directory joined with `cohere`, which is `~/Library/Caches/cohere` on
// macOS, `$XDG_CACHE_HOME/cohere` or `~/.cache/cohere` on Linux, and `%LocalAppData%\cohere` on Windows. When there is
// none, it is why, and the run keeps no system cache.
//
// Nothing is made or written here, so a directory that is found but cannot be written is said by the first write
// that fails. A variable set empty is read as unset, as Go reads an empty `GOCACHE`. A relative directory is a
// reason, never a guess: a run's working directory differs from project to project, and a cache under each would
// be neither one cache nor the person's choice.
func SystemCacheRoot() (string, *NotKept) {
	if chosen := os.Getenv(systemCacheVariable); chosen != "" {
		if !filepath.IsAbs(chosen) {
			return "", notKeptBecause(fmt.Sprintf("$%s is %q, a relative path, and must be absolute",
				systemCacheVariable, chosen))
		}
		return filepath.Clean(chosen), nil
	}
	userCacheDirectory, userCacheDirectoryError := os.UserCacheDir()
	if userCacheDirectoryError != nil {
		return "", notKeptBecause(fmt.Sprintf("finding the user's cache directory: %v", userCacheDirectoryError))
	}
	// os.UserCacheDir builds the directory from `HOME` on macOS without asking whether it is absolute.
	if !filepath.IsAbs(userCacheDirectory) {
		return "", notKeptBecause(fmt.Sprintf("the user's cache directory %q is a relative path", userCacheDirectory))
	}
	return filepath.Join(userCacheDirectory, "cohere"), nil
}
