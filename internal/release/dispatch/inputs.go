package dispatch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Inputs is everything that can change what the built binary does.
//
// Every field here is a thing that, if it changed without the hash changing, would leave a stale
// binary looking like a fresh one. The set is deliberately wider than "the rules directory": the
// toolchain miscompiles differently across versions, the pinned TypeScript commit is most of
// the code, and the build flags change the binary that comes out.
type Inputs struct {
	// GoVersion is the toolchain identity, as `go env GOVERSION GOOS GOARCH`. A binary built by a
	// different compiler, or for a different platform, is a different binary.
	GoVersion string

	// TypeScriptGoCommit pins the vendored compiler. It is most of the code in the binary, and it
	// moves independently of anything in this repo.
	TypeScriptGoCommit string

	// BuildFlags are the flags handed to `go build`. These change the output, and `-trimpath` in
	// particular is a build-input change rather than a link flag: adding it invalidates the whole
	// compile cache, measured at 33s on a warm 2.0 GB cache.
	BuildFlags []string

	// SourceFiles are the absolute paths of every non-stdlib file the binary compiles, discovered
	// transitively from the entry point. Contents are hashed, not names: a rule edited in place
	// keeps its filename, and a hash over names alone would miss it entirely.
	SourceFiles []string

	// SelfCommit and SourceTreeModified are the working tree's state, which a `--dev` build is stamped with
	// and reports about itself. A key without them served a binary built in one state to a tree in the
	// other: built dirty, it went on saying so after the work was committed, and built clean, it said clean
	// beside an uncommitted edit outside the Go sources, a Swift rule's included (#50wtb2s).
	SelfCommit         string
	SourceTreeModified bool
}

// Compute returns the hash naming the binary these inputs build.
//
// The digest covers file contents, not modification times. Times change when a branch is checked
// out and would cause a rebuild that changes nothing; contents are what actually decides what the
// binary does.
func (inputs Inputs) Compute() (string, error) {
	digest := sha256.New()

	// Each field is written with a label and a terminator so that no two different input sets can
	// serialize to the same bytes. Without the framing, moving a string from one field to another
	// would hash identically.
	fmt.Fprintf(digest, "goVersion\x00%s\x00", inputs.GoVersion)
	fmt.Fprintf(digest, "typeScriptCommit\x00%s\x00", inputs.TypeScriptGoCommit)
	for _, flag := range inputs.BuildFlags {
		fmt.Fprintf(digest, "buildFlag\x00%s\x00", flag)
	}
	fmt.Fprintf(digest, "selfCommit\x00%s\x00", inputs.SelfCommit)
	fmt.Fprintf(digest, "sourceTreeModified\x00%t\x00", inputs.SourceTreeModified)

	// Sorted so the digest does not depend on the order the toolchain happened to list packages in.
	files := append([]string(nil), inputs.SourceFiles...)
	sort.Strings(files)

	for _, path := range files {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			// A file that was listed as an input and cannot be read is a loud failure. Hashing
			// around it would produce a stable digest for a tree we could not actually see, which
			// is the stale-binary failure wearing a different hat.
			return "", fmt.Errorf("reading build input %s: %w", path, readError)
		}
		fmt.Fprintf(digest, "file\x00%s\x00%d\x00", filepath.Base(path), len(contents))
		digest.Write(contents)
	}

	return hex.EncodeToString(digest.Sum(nil))[:hashLength], nil
}
