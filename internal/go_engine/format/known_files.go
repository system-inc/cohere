package format

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"sync"
)

// Verdict is what gofmt made of a file that a run need not format again while its bytes stay the same.
type Verdict string

const (
	// VerdictUnchanged is a file gofmt leaves as it is, learned by a run that does not wrap long lines, so a run
	// that wraps them never trusts it: the file may hold a long line the wrap would change.
	VerdictUnchanged Verdict = "Unchanged"

	// VerdictWrapUnchanged is a file gofmt and the long-line wrap both leave as it is: it holds no long line the
	// rule reports, or none the wrap could bring within the limit. Every run trusts it.
	VerdictWrapUnchanged Verdict = "WrapUnchanged"

	// VerdictGenerated is a file skipped as generated.
	VerdictGenerated Verdict = "Generated"
)

// KnownFiles is the verdicts of files gofmt had nothing to do to, by the sha256 of their bytes, which a run reads
// in place of formatting them again: gofmt's answer, and the wrap's, is a function of a file's bytes and of the
// gofmt and the wrap the binary links, which whoever keeps the verdicts keys them on. A verdict says whether the
// wrap was asked too, since a run that wraps must not trust one that only gofmt gave. A file the run would change,
// could not format, or whose wrap it declined is never known, so its finding or its rewrite is always the run's own.
// Run asks it from any goroutine.
type KnownFiles struct {
	lock     sync.Mutex
	verdicts map[[sha256.Size]byte]Verdict

	// seen are the verdicts the run read or learned, which are what Encode keeps, so files long gone are dropped.
	seen map[[sha256.Size]byte]Verdict
}

// DecodeKnownFiles is the verdicts encoded, none when they do not decode.
func DecodeKnownFiles(encoded []byte) *KnownFiles {
	known := &KnownFiles{verdicts: map[[sha256.Size]byte]Verdict{}, seen: map[[sha256.Size]byte]Verdict{}}
	if len(encoded) > 0 {
		// Ignored: verdicts that do not decode are none, and every file is formatted as if never seen.
		_ = gob.NewDecoder(bytes.NewReader(encoded)).Decode(&known.verdicts)
	}
	return known
}

// Encode is the verdicts the run read or learned, to be decoded by DecodeKnownFiles.
func (known *KnownFiles) Encode() ([]byte, error) {
	known.lock.Lock()
	defer known.lock.Unlock()
	var encoded bytes.Buffer
	if encodeError := gob.NewEncoder(&encoded).Encode(known.seen); encodeError != nil {
		return nil, encodeError
	}
	return encoded.Bytes(), nil
}

// verdict is what is known of a file with digest, and false for a file not known, or known only to gofmt when the
// run wraps long lines.
func (known *KnownFiles) verdict(digest [sha256.Size]byte, wraps bool) (Verdict, bool) {
	known.lock.Lock()
	defer known.lock.Unlock()
	verdict, held := known.verdicts[digest]
	if !held || (wraps && verdict == VerdictUnchanged) {
		return "", false
	}
	known.seen[digest] = verdict
	return verdict, true
}

// learn keeps what the run made of a file with digest.
func (known *KnownFiles) learn(digest [sha256.Size]byte, verdict Verdict) {
	known.lock.Lock()
	defer known.lock.Unlock()
	known.verdicts[digest] = verdict
	known.seen[digest] = verdict
}
