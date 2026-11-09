package staticcheck_report

import (
	"fmt"
	"go/ast"
	"go/version"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// File is the file of the package's syntax that node is in, and nil for a node in none of them: staticcheck's
// code.File, which reads the same match from its tokenfile pass. A parsed file holds the positions from its FileStart
// to its FileEnd, both included: the bounds of its token.File, which the file set's own lookup reads. So comparing them
// gives the file set's answer without its lock, which every call took, and contended for once a package's walk ran on
// several workers, with a lookup per file besides.
func File(context *rule.Context, node Positioner) *ast.File {
	position := node.Pos()
	if !position.IsValid() {
		return nil
	}
	for _, file := range context.Files {
		if file.FileStart <= position && position <= file.FileEnd {
			return file
		}
	}
	return nil
}

// LanguageVersion is the Go language version node is written in, such as "go1.22": its file's own, from a
// `//go:build` line naming one, and otherwise its package's, as the type checker recorded it. It is "" where neither
// says, as for a package loaded outside a module, and for a node in none of the package's files: staticcheck's
// code.LanguageVersion, whose comment walks through why the type checker's record is the answer.
func LanguageVersion(context *rule.Context, node Positioner) string {
	return context.TypesInfo.FileVersions[File(context, node)]
}

// StandardLibraryVersion is the version of the standard library node can expect, which can differ from its language
// version: staticcheck's code.StdlibVersion. It panics, as staticcheck's does, for a node in none of the package's
// files.
func StandardLibraryVersion(context *rule.Context, node Positioner) string {
	// The package's version is its module's go line, which staticcheck's -go flag can set too.
	packageVersion := context.Package.GoVersion()
	file := File(context, node)
	if file == nil {
		// Invariant: a check reports only at nodes of its own package, which are in its files; a node elsewhere is
		// a port's bug, which the dispatcher records under the rule's name as staticcheck's crash would be.
		panic(fmt.Sprintf("no file found for node with position %s", context.Fset.PositionFor(node.Pos(), false)))
	}
	if file.GoVersion == "" {
		return packageVersion
	}
	// Before Go 1.21 a module's go line was the newest language it could use, and a module often stayed on an older
	// one while files tagged for a newer release used that release's library, so a tagged file has its tag's library,
	// older or newer than the module's.
	if version.Compare(packageVersion, "go1.21") == -1 {
		return file.GoVersion
	}
	// From Go 1.21 the go line is the oldest toolchain that builds the module, so every file has at least that
	// library, and a file tagged newer has its tag's.
	if version.Compare(file.GoVersion, packageVersion) == 1 {
		return file.GoVersion
	}
	return packageVersion
}
