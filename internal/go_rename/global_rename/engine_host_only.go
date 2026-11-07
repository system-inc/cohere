package global_rename

import (
	"fmt"
	"path/filepath"
	"strings"
)

// unbuiltDirectories is, for a HostOnly plan, each directory of a root package that holds a Go file under the root
// the host's build leaves out (its IgnoredFiles, outside TypeScript/), with the first such file by name, relative to
// the root. A rename reaching such a package could be named in that file, which no load of this plan type-checks.
func (engine *engine) unbuiltDirectories() map[string]string {
	hostBuilt := map[string]bool{}
	for _, rootsPackage := range engine.roots {
		for _, list := range [][]string{rootsPackage.GoFiles, rootsPackage.CompiledGoFiles, rootsPackage.OtherFiles} {
			for _, filename := range list {
				hostBuilt[filepath.Clean(filename)] = true
			}
		}
	}
	unbuilt := map[string]string{}
	for _, rootsPackage := range engine.roots {
		for _, filename := range rootsPackage.IgnoredFiles {
			filename = filepath.Clean(filename)
			relative, inside := engine.workspace.Relative(filename)
			if !inside ||
				!strings.HasSuffix(filename, ".go") ||
				hostBuilt[filename] ||
				relative == "TypeScript" ||
				strings.HasPrefix(relative, "TypeScript/") {
				continue
			}
			directory := filepath.Dir(filename)
			if known, found := unbuilt[directory]; !found || relative < known {
				unbuilt[directory] = relative
			}
		}
	}
	return unbuilt
}

// hostOnlyRefusal is why a HostOnly plan refuses a candidate, or "": a site of it, its declaration or a use, is in the
// directory of a package holding a Go file the host does not build, which only a load under the platform that builds
// it could prove the rename against.
func (engine *engine) hostOnlyRefusal(key Key) string {
	for _, site := range engine.sites[key] {
		if relative, found := engine.unbuilt[filepath.Dir(site.filename)]; found {
			return fmt.Sprintf(
				"it reaches the package in %s, which holds %s, a file this platform does not build",
				engine.relative(filepath.Dir(site.filename)),
				relative,
			)
		}
	}
	return ""
}
