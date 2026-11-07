package global_rename

import (
	"fmt"
	"go/token"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// Platforms are the platforms the plan read files under, in order, each of which the rewrite must be proven under
// as well as the host (ProvePlatform).
func (result *Result) Platforms() []workspace.Platform {
	return slices.Clone(result.platforms)
}

// RenamesUnder are the accepted renames that reach a package holding a file only platform builds, the ones a proof
// that fails under it names, in the order of Accepted.
func (result *Result) RenamesUnder(platform workspace.Platform) []Rename {
	var under []Rename
	for _, accepted := range result.Accepted {
		if result.platformKeys[platform][accepted.Key] {
			under = append(under, accepted)
		}
	}
	return under
}

// ProvePlatform proves a plan's rewrite, overlay, under one of its Platforms, through options.LoadPlatform as the plan
// read it: every root package that type-checks there before the rewrite must after, and every identifier in it must
// resolve to the same declaration under its expected name, as Compare requires of the host. A load that fails is a
// PlatformLoad mismatch, so the rewrite is never taken as proven under a platform nothing was compared under.
func ProvePlatform(
	loaded *workspace.Workspace,
	expectation *Expectation,
	platform workspace.Platform,
	overlay map[string][]byte,
	options Options,
) Verification {
	loadPlatform := options.LoadPlatform
	if loadPlatform == nil {
		loadPlatform = loaded.LoadPlatform
	}
	verification, proveError := provePlatform(loadPlatform, platform, overlay, expectation.newNameAt)
	if proveError != nil {
		return Verification{Mismatches: []Mismatch{{
			Kind: MismatchKindPlatformLoad, Platform: platform.String(), Reason: proveError.Error(),
		}}}
	}
	return verification
}

// loadPlatform is the tree as platform builds it, through options.LoadPlatform when the caller gives one.
func (engine *engine) loadPlatform(platform workspace.Platform, overlay map[string][]byte) (*workspace.PlatformLoad,
	error) {
	if engine.options.LoadPlatform != nil {
		return engine.options.LoadPlatform(platform, overlay)
	}
	return engine.workspace.LoadPlatform(platform, overlay)
}

// reachedDirectories are the directories of the packages a candidate reaches, each with the candidates that reach
// it: every directory one of its sites is in, and, for a name another package can name, every root package that
// imports its package, since a file of that package the host does not build may name it with no site the host sees.
// Only a platform a reached directory's files need is loaded, so a plan whose renames reach no such package loads
// none.
func (engine *engine) reachedDirectories() map[string]map[Key]bool {
	reached := map[string]map[Key]bool{}
	reach := func(directory string, key Key) {
		if reached[directory] == nil {
			reached[directory] = map[Key]bool{}
		}
		reached[directory][key] = true
	}
	directoryOf := map[string]string{}
	for _, rootsPackage := range engine.roots {
		for _, filename := range append(slices.Clone(rootsPackage.GoFiles), rootsPackage.IgnoredFiles...) {
			directoryOf[rootsPackage.PkgPath] = filepath.Dir(filepath.Clean(filename))
			break
		}
	}
	for key, candidate := range engine.candidates {
		for _, site := range engine.sites[key] {
			reach(filepath.Dir(filepath.Clean(site.filename)), key)
		}
		if candidate.local || !token.IsExported(candidate.OldName) {
			continue
		}
		for _, rootsPackage := range engine.roots {
			if _, imports := rootsPackage.Imports[candidate.Key.PackagePath]; imports {
				if directory, known := directoryOf[rootsPackage.PkgPath]; known {
					reach(directory, key)
				}
			}
		}
	}
	return reached
}

// platformFailure is why a platform cannot prove a rename that reaches a directory: its whole load failed, or the
// package in that directory does not type-check under it.
type platformFailure struct {
	load     string
	packages map[string]string
}

// platformLoadRefusal is why a plan with RequireSoundPlatforms refuses a candidate, or "": a site of it is in the
// directory of a package holding a file only some platform builds, and that platform's load failed, or the package
// does not type-check under it, so nothing there can prove the rename. The reason names the platform.
func (engine *engine) platformLoadRefusal(key Key) string {
	if !engine.options.RequireSoundPlatforms {
		return ""
	}
	for _, site := range engine.sites[key] {
		directory := filepath.Dir(filepath.Clean(site.filename))
		for _, unbuilt := range engine.unbuiltFiles[directory] {
			failure, failed := engine.platformFailures[unbuilt.platform]
			if !failed {
				continue
			}
			if failure.load != "" {
				return fmt.Sprintf("it reaches %s, which only %s builds, and the tree does not load as %s: %s",
					unbuilt.relative, unbuilt.platform, unbuilt.platform, failure.load)
			}
			if reason, broken := failure.packages[directory]; broken {
				return fmt.Sprintf("it reaches %s, which only %s builds, and its package does not type-check as %s: %s",
					unbuilt.relative, unbuilt.platform, unbuilt.platform, reason)
			}
		}
	}
	return ""
}

// unbuiltFile is a Go file under the root that the host's build leaves out and a platform builds.
type unbuiltFile struct {
	relative string
	platform workspace.Platform
}

// sortedUnbuilt sorts each directory's unbuilt files by name, so a refusal names the same one every run.
func sortedUnbuilt(files map[string][]unbuiltFile) {
	for _, list := range files {
		sort.Slice(list, func(left, right int) bool { return list[left].relative < list[right].relative })
	}
}

// packageDirectory is the directory of the root package with ID, read from its files, empty when no root has it.
func (engine *engine) packageDirectory(id string) string {
	for _, rootsPackage := range engine.roots {
		if rootsPackage.ID != id {
			continue
		}
		for _, filename := range append(slices.Clone(rootsPackage.GoFiles), rootsPackage.IgnoredFiles...) {
			return filepath.Dir(filepath.Clean(filename))
		}
	}
	return ""
}

// isTypeScript reports whether a name relative to the root is under the TypeScript submodule.
func isTypeScript(relative string) bool {
	return relative == "TypeScript" || strings.HasPrefix(relative, "TypeScript/")
}
