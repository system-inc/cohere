package loop_closure

import (
	"go/ast"
	"go/version"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// perIterationVersion is the Go version from which each iteration of a loop declares its own variables
// (go.dev/issue/60078), x/tools' versions.Go1_22.
const perIterationVersion = "go1.22"

// fileUsesGoVersion reports whether file may use the features of goVersion, x/tools'
// analyzerutil.FileUsesGoVersion. A file the type checker recorded no version for, one it was never handed, is
// judged as older, the conservative answer.
func fileUsesGoVersion(context *rule.Context, file *ast.File, goVersion string) bool {
	fileVersion, recorded := context.TypesInfo.FileVersions[file]
	if !recorded {
		return false
	}
	// A bootstrap package must build with the bootstrap toolchain, so its files use no Go newer than that, whatever
	// their module says. Its tests are not built by the bootstrap, and some rely on being judged at their own version.
	// The file is named as it is on disk, not as a `//line` directive renames it, as the original's token.File.Name.
	if bootstrapPackages[context.Package.Path()] &&
		!strings.HasSuffix(context.FileSet.PositionFor(file.Pos(), false).Filename, "_test.go") {
		fileVersion = bootstrapVersion
	}
	return !versionBefore(fileVersion, goVersion)
}

// versionBefore reports whether the language version of goVersion is older than release's, x/tools'
// versions.Before, which go/version's Lang and Compare answer the same way. An empty version is a future one, newer
// than any release: the type checker records it for a file whose package names no Go version, as one loaded outside
// a module does.
func versionBefore(goVersion string, release string) bool {
	if goVersion == "" {
		return false
	}
	return version.Compare(version.Lang(goVersion), version.Lang(release)) < 0
}
