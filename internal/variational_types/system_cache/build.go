package system_cache

import (
	"errors"
	"fmt"
	"go/token"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/variational_types/system_cache/export_data"
)

// Build is one build's packages read from a store as the fork's types, each complete, into Packages, which the
// caller may share with another source of the same build: a path Packages holds already is never read here. It is
// not safe for use from more than one goroutine at once.
type Build struct {
	Store Store

	// KeyOf is a package's key in this build, false for a path the build keyed nothing under.
	KeyOf func(path string) (cache.Key, bool)

	FileSet  *token.FileSet
	Packages map[string]*types.Package

	// reading are the paths whose reads are running, one inside another, so an import cycle in what the store holds
	// is an error rather than a read without end.
	reading map[string]bool
}

// NewBuild is the build whose packages' keys keyOf gives, read from store into packages, their positions added to
// fileSet.
func NewBuild(
	store Store,
	keyOf func(path string) (cache.Key, bool),
	fileSet *token.FileSet,
	packages map[string]*types.Package,
) *Build {
	return &Build{Store: store, KeyOf: keyOf, FileSet: fileSet, Packages: packages, reading: map[string]bool{}}
}

// errorManifestRead stops a read of export data once its manifest, the packages it names, is read.
var errorManifestRead = errors.New("the manifest is read")

// Read reads the package at path into Packages, and every package its export data names first, unless Packages holds
// it already, and reports whether the build has it: present is false, with no error, when the build keyed nothing
// under path, the store holds no export data for it, or a package it names is one the build does not have either,
// and Packages then gains nothing of path's. An error is export data that is there but does not read, which leaves
// Packages holding any package it named that was read on the way.
func (build *Build) Read(path string) (present bool, readError error) {
	if _, held := build.Packages[path]; held {
		return true, nil
	}
	if path == "unsafe" {
		build.Packages[path] = types.Unsafe
		return true, nil
	}
	if build.reading[path] {
		return false, fmt.Errorf("reading %s from the system cache: an import cycle runs back through it", path)
	}
	key, keyed := build.KeyOf(path)
	if !keyed {
		return false, nil
	}
	data, stored := build.Store.ExportData(key)
	if !stored {
		return false, nil
	}
	build.reading[path] = true
	defer delete(build.reading, path)
	named, manifestError := manifest(data, path)
	if manifestError != nil {
		return false, manifestError
	}
	for _, namedPath := range named {
		if namedPath == path {
			continue
		}
		namedPresent, namedError := build.Read(namedPath)
		if namedError != nil || !namedPresent {
			return false, namedError
		}
	}
	if _, readError := export_data.Read(build.FileSet, build.Packages, data, path); readError != nil {
		return false, readError
	}
	return true, nil
}

// manifest is the path of every package data, the export data of the package at path, names, its own among them, read
// without decoding a declaration, as x/tools' GetPackagesFunc allows.
func manifest(data []byte, path string) ([]string, error) {
	var named []string
	_, readError := export_data.IImportShallow(token.NewFileSet(), func(items []export_data.GetPackagesItem) error {
		for _, item := range items {
			named = append(named, item.Path)
		}
		return errorManifestRead
	}, data, path, func(string, ...any) {
		// Nothing past the manifest is read, so nothing can be reported.
	})
	if readError == nil {
		return nil, fmt.Errorf("reading the manifest of %s's export data: it read without naming its packages", path)
	}
	if !errors.Is(readError, errorManifestRead) {
		return nil, fmt.Errorf("reading the manifest of %s's export data: %w", path, readError)
	}
	return named, nil
}
