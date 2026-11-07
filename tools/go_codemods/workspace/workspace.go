// Package workspace loads every package of a cohere worktree's go.work, type-checked with its tests, and
// decides which variants and files a codemod may read and edit.
package workspace

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/cohere/tools/go_codemods/rename"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// Workspace is one load of the tree.
type Workspace struct {
	Root string
	// Roots is every root package go/packages returned except the synthesized test mains.
	Roots []*packages.Package
	// Analyzed is the variant of each package a codemod reads: `p [p.test]` in place of `p` when it exists,
	// and `p_test [p.test]`.
	Analyzed []*packages.Package
	skips    []string
}

// Patterns are the package patterns for root's go.work: each use directory with /..., minus TypeScript/.
func Patterns(root string) ([]string, error) {
	contents, readError := os.ReadFile(filepath.Join(root, "go.work"))
	if readError != nil {
		return nil, readError
	}
	work, parseError := modfile.ParseWork("go.work", contents, nil)
	if parseError != nil {
		return nil, parseError
	}
	var patterns []string
	for _, use := range work.Use {
		directory := filepath.ToSlash(filepath.Clean(use.Path))
		if directory == "TypeScript" || strings.HasPrefix(directory, "TypeScript/") {
			continue
		}
		if directory == "." {
			patterns = append(patterns, "./...")
			continue
		}
		patterns = append(patterns, "./"+directory+"/...")
	}
	return patterns, nil
}

// Load loads root's workspace and fails on any package error: renaming over a tree that does not type-check
// is unsafe.
func Load(root string, skips []string) (*Workspace, error) {
	return LoadWithOverlay(root, skips, nil)
}

// LoadWithOverlay is Load reading the files in overlay (absolute path to contents) in place of what is on
// disk, so a rewrite can be type-checked and compared before anything is written.
func LoadWithOverlay(root string, skips []string, overlay map[string][]byte) (*Workspace, error) {
	absoluteRoot, absoluteError := filepath.Abs(root)
	if absoluteError != nil {
		return nil, absoluteError
	}
	resolvedRoot, resolveError := filepath.EvalSymlinks(absoluteRoot)
	if resolveError != nil {
		return nil, resolveError
	}
	loaded, loadError := loadPackages(resolvedRoot, overlay, nil)
	if loadError != nil {
		return nil, loadError
	}
	var problems []string
	for _, loadedPackage := range loaded {
		for _, packageError := range loadedPackage.Errors {
			problems = append(problems, fmt.Sprintf("%s: %s", loadedPackage.ID, packageError))
		}
		if loadedPackage.Types == nil || loadedPackage.TypesInfo == nil {
			problems = append(problems, fmt.Sprintf("%s: no type information", loadedPackage.ID))
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%d package errors:\n%s", len(problems), strings.Join(problems, "\n"))
	}
	return newWorkspace(resolvedRoot, skips, loaded), nil
}

// loadPackages loads every package of root's go.work patterns, with tests, type-checked from source, under the
// environment given (nil for this process's own).
func loadPackages(root string, overlay map[string][]byte, environment []string) ([]*packages.Package, error) {
	patterns, patternsError := Patterns(root)
	if patternsError != nil {
		return nil, patternsError
	}
	configuration := &packages.Config{
		Dir:   root,
		Env:   environment,
		Tests: true,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Overlay: overlay,
	}
	return packages.Load(configuration, patterns...)
}

// newWorkspace sorts loaded packages into a workspace's roots and analyzed variants.
func newWorkspace(resolvedRoot string, skips []string, loaded []*packages.Package) *Workspace {
	result := &Workspace{Root: resolvedRoot, skips: skips}
	testVariants := map[string]bool{}
	for _, loadedPackage := range loaded {
		if loadedPackage.ID == loadedPackage.PkgPath+" ["+loadedPackage.PkgPath+".test]" {
			testVariants[loadedPackage.PkgPath] = true
		}
	}
	for _, loadedPackage := range loaded {
		if loadedPackage.Name == "main" &&
			loadedPackage.ID == loadedPackage.PkgPath &&
			strings.HasSuffix(loadedPackage.PkgPath, ".test") &&
			!result.anyFileInRoot(loadedPackage) {
			continue
		}
		result.Roots = append(result.Roots, loadedPackage)
		if loadedPackage.ID == loadedPackage.PkgPath && testVariants[loadedPackage.PkgPath] {
			continue
		}
		result.Analyzed = append(result.Analyzed, loadedPackage)
	}
	sort.Slice(
		result.Analyzed,
		func(left, right int) bool { return result.Analyzed[left].ID < result.Analyzed[right].ID },
	)
	return result
}

// anyFileInRoot reports whether any of pkg's compiled files lives under the root.
func (workspace *Workspace) anyFileInRoot(goPackage *packages.Package) bool {
	for _, filename := range goPackage.CompiledGoFiles {
		if _, inside := workspace.Relative(filename); inside {
			return true
		}
	}
	return false
}

// Relative returns filename relative to the root, and whether it is under it.
func (workspace *Workspace) Relative(filename string) (string, bool) {
	relative, relativeError := filepath.Rel(workspace.Root, filename)
	if relativeError != nil || relative == ".." || strings.HasPrefix(relative, "../") || filepath.IsAbs(relative) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

// Editable reports whether a codemod may edit file: under the root, not under TypeScript/, not generated, and
// not under a skipped prefix.
func (workspace *Workspace) Editable(goPackage *packages.Package, file *ast.File) bool {
	filename := goPackage.Fset.Position(file.Pos()).Filename
	relative, inside := workspace.Relative(filename)
	if !inside || relative == "TypeScript" || strings.HasPrefix(relative, "TypeScript/") || ast.IsGenerated(file) ||
		declaresCopy(file) {
		return false
	}
	for _, skip := range workspace.skips {
		skip = strings.TrimSuffix(filepath.ToSlash(skip), "/")
		if relative == skip ||
			strings.HasPrefix(relative, skip+"/") ||
			(strings.HasSuffix(skip, ".go") && relative == skip) {
			return false
		}
	}
	return true
}

// RenamePackage is pkg as the rename engine sees it, with only its editable files.
func (workspace *Workspace) RenamePackage(goPackage *packages.Package) rename.Package {
	var files []*ast.File
	for _, file := range goPackage.Syntax {
		if workspace.Editable(goPackage, file) {
			files = append(files, file)
		}
	}
	return rename.Package{FileSet: goPackage.Fset, Types: goPackage.Types, Info: goPackage.TypesInfo, Files: files}
}

// LoadPlatform loads the workspace's tree as platform builds it, reading overlay in place of what is on disk,
// with cgo off. Unlike Load it keeps going past package errors: a package of the host's that another platform
// cannot build is reported in Failed, not fatal.
func (workspace *Workspace) LoadPlatform(platform Platform, overlay map[string][]byte) (*PlatformLoad, error) {
	environment := append(
		os.Environ(),
		"GOOS="+platform.OperatingSystem,
		"GOARCH="+platform.Architecture,
		"CGO_ENABLED=0",
	)
	loaded, loadError := loadPackages(workspace.Root, overlay, environment)
	if loadError != nil {
		return nil, fmt.Errorf("loading as %s: %w", platform, loadError)
	}
	result := &PlatformLoad{Platform: platform, Failed: map[string]string{}}
	clean := map[*packages.Package]bool{}
	var isClean func(goPackage *packages.Package, visiting map[*packages.Package]bool) bool
	isClean = func(goPackage *packages.Package, visiting map[*packages.Package]bool) bool {
		if known, decided := clean[goPackage]; decided {
			return known
		}
		if visiting[goPackage] {
			return true
		}
		visiting[goPackage] = true
		// A dependency outside the tree comes from export data, with types and no syntax to check.
		answer := len(goPackage.Errors) == 0 && goPackage.Types != nil
		if len(goPackage.Errors) > 0 {
			result.Failed[goPackage.ID] = goPackage.Errors[0].Error()
		} else if !answer {
			result.Failed[goPackage.ID] = "no type information"
		}
		paths := make([]string, 0, len(goPackage.Imports))
		for path := range goPackage.Imports {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			if !isClean(goPackage.Imports[path], visiting) && answer {
				answer = false
				result.Failed[goPackage.ID] = "imports " + goPackage.Imports[path].ID + ", which does not type-check"
			}
		}
		clean[goPackage] = answer
		return answer
	}
	var kept []*packages.Package
	for _, loadedPackage := range loaded {
		if loadedPackage.TypesInfo == nil {
			result.Failed[loadedPackage.ID] = "no type information"
			continue
		}
		if isClean(loadedPackage, map[*packages.Package]bool{}) {
			kept = append(kept, loadedPackage)
		}
	}
	result.Clean = newWorkspace(workspace.Root, workspace.skips, kept)
	return result, nil
}
