package git_tree

import (
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"hash"
	"strings"
)

// ObjectFormat is a repository's hash, spelled as `git init --object-format` spells it.
type ObjectFormat string

// The two object formats git knows. Their values are git's own spellings, so they cross into a git
// command's arguments unchanged.
const (
	Sha1   ObjectFormat = "sha1"
	Sha256 ObjectFormat = "sha256"
)

// FormatOf is the object format an object's name is written in: 40 lowercase hexadecimal digits are
// a sha1 name, 64 a sha256 one. A repository's ids are all one length, so this is how a caller with
// only an id learns its repository's hash.
func FormatOf(objectName string) (ObjectFormat, error) {
	if !ValidObjectName(objectName) {
		return "", fmt.Errorf("%q isn't a git object's name: 40 or 64 lowercase hexadecimal digits", objectName)
	}
	if len(objectName) == 64 {
		return Sha256, nil
	}
	return Sha1, nil
}

// ValidObjectName reports whether text is a git object's full name, as git prints one: 40 lowercase
// hexadecimal digits (sha1) or 64 (sha256).
func ValidObjectName(text string) bool {
	return (len(text) == 40 || len(text) == 64) && strings.Trim(text, "0123456789abcdef") == ""
}

// digits is how many hexadecimal digits an object's name takes in the format.
func (format ObjectFormat) digits() (int, error) {
	switch format {
	case Sha1:
		return 40, nil
	case Sha256:
		return 64, nil
	}
	return 0, fmt.Errorf("%q isn't an object format git knows (sha1 or sha256)", string(format))
}

// newHash is the format's hash, fresh.
func (format ObjectFormat) newHash() (hash.Hash, error) {
	switch format {
	case Sha1:
		return sha1.New(), nil
	case Sha256:
		return sha256.New(), nil
	}
	return nil, fmt.Errorf("%q isn't an object format git knows (sha1 or sha256)", string(format))
}
