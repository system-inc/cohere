package program

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// RunCacheKeyParts is what identifies a run apart from its files, in four parts hashed apart, so a run that
// misses can say which one moved rather than that something did (#547dhjz).
type RunCacheKeyParts struct {
	// Binary is the running binary: its resolved path, size and modification time, and the format and
	// platform it reads. Keyed by path, size and time rather than by hashing its bytes: it is about 70 MB,
	// reading it every run would spend the budget this cache exists to save, and a rebuilt binary has a
	// new modification time, which is all this needs.
	Binary string

	// Directory is the working directory the caller resolved, the project root's target.
	Directory string

	// Arguments is the command line.
	Arguments string

	// Facts is anything else the run's output depends on that is not a file the build reads: config paths
	// the command resolved, and results it computes from sources the build never sees. Order matters, and
	// an empty fact still counts.
	Facts string
}

// Key is the parts as one key.
func (parts RunCacheKeyParts) Key() string {
	sum := sha256.Sum256(
		[]byte(parts.Binary + "\x00" + parts.Directory + "\x00" + parts.Arguments + "\x00" + parts.Facts),
	)
	return fmt.Sprintf("%x", sum)
}

// Changed says which parts differ from recorded, in words, or that the record kept no parts to compare, as
// one written before they were kept.
func (parts RunCacheKeyParts) Changed(recorded RunCacheKeyParts) string {
	if recorded == (RunCacheKeyParts{}) {
		return "it was recorded without its key's parts, so which one moved cannot be said"
	}
	var changed []string
	if parts.Binary != recorded.Binary {
		changed = append(changed, "the cohere binary")
	}
	if parts.Directory != recorded.Directory {
		changed = append(changed, "the project root")
	}
	if parts.Arguments != recorded.Arguments {
		changed = append(changed, "the arguments")
	}
	if parts.Facts != recorded.Facts {
		changed = append(
			changed,
			"the configuration (the tsconfig or lint config, their extends chains, the rules, or the output's color)",
		)
	}
	if len(changed) == 0 {
		return "no part changed, though the key did"
	}
	return strings.Join(changed, " and ") + " changed"
}
