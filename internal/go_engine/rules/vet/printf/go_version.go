package printf

import (
	"go/ast"
	"go/types"
	"go/version"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The Go versions whose features gate a check, as x/tools' versions names them.
const (
	go124 = "go1.24"
	go126 = "go1.26"
	go127 = "go1.27"
)

// fileVersionOf is file's Go version as the type checker recorded it, or "" for a version it could not tell, which
// reads as an unknown future one: x/tools' versions.FileVersion, whose package is a clone of go/version.
func fileVersionOf(info *types.Info, file *ast.File) string {
	if recorded := info.FileVersions[file]; version.IsValid(recorded) {
		return recorded
	}
	return ""
}

// atLeast reports whether the file version fileVersion comes at or after the Go release, an unknown future version
// always does: x/tools' versions.AtLeast.
func atLeast(fileVersion string, release string) bool {
	if fileVersion == "" {
		return true
	}
	return version.Compare(version.Lang(fileVersion), version.Lang(release)) >= 0
}

// before reports whether the file version fileVersion comes before the Go release, which an unknown future version
// never does: x/tools' versions.Before.
func before(fileVersion string, release string) bool {
	if fileVersion == "" {
		return false
	}
	return version.Compare(version.Lang(fileVersion), version.Lang(release)) < 0
}

// fileUsesGoVersion reports whether file may use the features of the Go release, false when the type checker
// recorded no version for it, such as a file it ignored: x/tools' analyzerutil.FileUsesGoVersion. A non-test file of
// a bootstrap package reads as bootstrapVersion's, since it must compile with that toolchain.
func fileUsesGoVersion(pass *analysis.Pass, file *ast.File, release string) bool {
	fileVersion, recorded := pass.TypesInfo.FileVersions[file]
	if !recorded {
		return false
	}
	packagePath := pass.Pkg.Path()
	fileName := ""
	if tokenFile := pass.Fset.File(file.Pos()); tokenFile != nil {
		fileName = tokenFile.Name()
	}
	// bootstrapPackages holds no external test package, `p_test`, and the suffix leaves out every other test.
	if maybeStandardPackage(packagePath) && bootstrapPackages[packagePath] && !strings.HasSuffix(fileName, "_test.go") {
		fileVersion = bootstrapVersion
	}
	return !before(fileVersion, release)
}

// maybeStandardPackage reports whether packagePath may be a standard library package, one whose first element holds
// no dot: x/tools' packagepath.MaybeStdPackage.
func maybeStandardPackage(packagePath string) bool {
	firstElement, _, _ := strings.Cut(packagePath, "/")
	return !strings.Contains(firstElement, ".") && packagePath != "testdata"
}
