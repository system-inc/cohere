package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ProfilePath is the CPU profile a release's Go is compiled with, relative to the module (#r6sqd2c).
//
// It is where Go's own `-pgo=auto` looks for the main package's profile, so a plain `go build ./command/cohere`
// picks it up too, but the release passes it by name: an inherited `GOFLAGS=-pgo=off` would otherwise drop it
// with nothing said. The profile is made by internal/release/tools/train_profile.sh from fixed training trees,
// never by hand and never from a tree the bench measures, and committed, so a build from a commit reproduces.
const ProfilePath = "command/cohere/default.pgo"

// Profile is the CPU profile a release build carries.
type Profile struct {
	// Path is ProfilePath when the module holds a profile, and "" when it holds none.
	Path string

	// Digest is the profile's sha256 in hex, stamped into the binary, so `--version` says which profile it was.
	Digest string
}

// ReadProfile reads the module's profile, or none when ProfilePath is absent. A release without one still builds,
// with `-pgo=off`, and its summary says it is not profile-guided.
func ReadProfile(moduleDirectory string) (Profile, error) {
	contents, readError := os.ReadFile(filepath.Join(moduleDirectory, filepath.FromSlash(ProfilePath)))
	if errors.Is(readError, fs.ErrNotExist) {
		return Profile{}, nil
	}
	if readError != nil {
		return Profile{}, fmt.Errorf("reading the release's profile: %w", readError)
	}
	digest := sha256.Sum256(contents)
	return Profile{Path: ProfilePath, Digest: hex.EncodeToString(digest[:])}, nil
}

// buildFlag is the `-pgo` the compile takes: the profile by its path, or off. Off is said rather than left to
// Go's default, so what a release passes is what it reports, whatever the environment's `GOFLAGS` holds.
func (profile Profile) buildFlag() string {
	if profile.Path == "" {
		return "-pgo=off"
	}
	return "-pgo=" + profile.Path
}

// Describe says which profile a build carried, for the release's summary, with the digest `--version` prints.
func (profile Profile) Describe() string {
	if profile.Path == "" {
		return "no profile: " + ProfilePath + " is absent, so this build is not profile-guided"
	}
	return "profile-guided by " + profile.Path + ", sha256 " + ShortDigest(profile.Digest)
}
