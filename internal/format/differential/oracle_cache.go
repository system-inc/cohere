package differential

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// OracleCache stores oracle output on disk, keyed by everything the output depends on.
//
// The key is the bundle digest, a description of the options, the file name (the parser is chosen by
// it) and the file's bytes. Leave any of those out and a stale answer is served for a changed input,
// which would make the harness report a match against an oracle that no longer exists. A nil cache
// formats every time.
type OracleCache struct {
	Directory string

	// Identity is the bundle digest plus the options, everything about the oracle that is not the file.
	Identity string
}

func (cache *OracleCache) format(oracle Formatter, path string, text string) (string, error) {
	if cache == nil {
		return oracle.Format(path, text)
	}

	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00%d\x00", cache.Identity, filepath.Base(path), len(text))
	hash.Write([]byte(text))
	key := hex.EncodeToString(hash.Sum(nil))
	entry := filepath.Join(cache.Directory, key[:2], key)

	if cached, readError := os.ReadFile(entry); readError == nil {
		return string(cached), nil
	}

	formatted, formatError := oracle.Format(path, text)
	if formatError != nil {
		return "", formatError
	}
	// A unique temporary per writer, then rename. Two workers can hold the same file (a duplicated
	// fixture, a copied config) and would otherwise write one shared temporary at the same instant.
	// The cache is an optimization, so any failure here is swallowed and the answer still returned.
	if createDirectoryError := os.MkdirAll(filepath.Dir(entry), 0o755); createDirectoryError == nil {
		if temporary, temporaryFileError := os.CreateTemp(
			filepath.Dir(entry),
			key+".*.tmp",
		); temporaryFileError == nil {
			_, writeError := temporary.WriteString(formatted)
			closeError := temporary.Close()
			if writeError == nil && closeError == nil {
				// Ignored: a rename that fails leaves the entry uncached, and the next run formats the file again.
				_ = os.Rename(temporary.Name(), entry)
			} else {
				// Ignored: a temporary left behind is only a stray file in the cache directory, never read as an entry.
				_ = os.Remove(temporary.Name())
			}
		}
	}
	return formatted, nil
}
