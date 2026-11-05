package dispatch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// committedBuild is what one committed-tree build is made from.
type committedBuild struct {
	// Commit is the commit of this repository being built.
	Commit string

	// CompilerCommit is the vendored compiler commit that Commit pins, read from Commit's own tree.
	// The submodule's checkout is not consulted, because a member bumping the pin locally is exactly
	// an uncommitted change.
	CompilerCommit string

	// GoVersion identifies the toolchain, as Inputs.GoVersion does.
	GoVersion string
}

// hash names the binary this build produces.
//
// The commit decides everything committed: the rules, the shims and the pinned compiler commit are
// all in its tree. What it does not decide is the toolchain and the flags, so those
// are framed in beside it.
func (build committedBuild) hash() string {
	digest := sha256.New()
	fmt.Fprintf(digest, "kind\x00%s\x00", snapshotTag)
	fmt.Fprintf(digest, "commit\x00%s\x00", build.Commit)
	fmt.Fprintf(digest, "goVersion\x00%s\x00", build.GoVersion)
	for _, flag := range ReleaseBuildFlags {
		fmt.Fprintf(digest, "buildFlag\x00%s\x00", flag)
	}
	return hex.EncodeToString(digest.Sum(nil))[:hashLength]
}
