package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// skipPrefixes is the repeatable -skip flag: paths from the root that other work owns. A prefix matches
// a path equal to it or under it, at a path element boundary, so internal/sentence does not take
// internal/sentences. A directory it matches is skipped whole; a file it matches is pinned, which keeps
// it part of its package while nothing moves into or out of it.
type skipPrefixes []string

func (prefixes *skipPrefixes) String() string {
	return strings.Join(*prefixes, ",")
}

func (prefixes *skipPrefixes) Set(value string) error {
	prefix := strings.Trim(strings.TrimSpace(filepath.ToSlash(value)), "/")
	if prefix == "" {
		return fmt.Errorf("an empty -skip would skip everything")
	}
	*prefixes = append(*prefixes, prefix)
	return nil
}

func (prefixes skipPrefixes) matches(relative string) bool {
	for _, prefix := range prefixes {
		if relative == prefix || strings.HasPrefix(relative, prefix+"/") {
			return true
		}
	}
	return false
}
