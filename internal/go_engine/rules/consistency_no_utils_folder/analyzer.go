// Package consistency_no_utils_folder is the Go engine's rule that no file lives in a folder named utils or util, in
// any casing and with any leading underscores, or in a package named that in its package clause.
//
// A folder named for nothing in particular collects everything, and Go's own guidance on package names says the same
// of util: a caller reads utils.Parse and learns nothing from the qualifier. "utilities" is not meaningfully better
// at that, but it is the spelling the house settled on, and one spelling searched is worth two spellings guessed.
// This is the Go form of nexus/consistency-no-utils-folder and cohere-swift's rule of the same name, and reads a
// folder's name the way they do (textrules-decisions.md, "Utils folder").
package consistency_no_utils_folder

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-utils-folder"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_utils_folder",
	Doc: "Flags every file whose package's path below its module root has a folder named utils or util, lowercased " +
		"and with leading underscores removed, or whose package clause is so named, once per file at the file's " +
		"start. The module's own path is never judged, since nobody inside it can rename it.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads the import path and the package clause, both of
// which a load has before any type is checked, so it reads no type information.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var utilsFolderMessage = policy.MessageOf(Name, "noUtils")

var underscoreUtilsFolderMessage = policy.MessageOf(Name, "noUnderscoreUtils")

var utilsPackageNameMessage = policy.MessageOf(Name, "utilsPackageName")

// externalTestSuffix ends the import path and the package clause of an external test package.
const externalTestSuffix = "_test"

func run(pass *analysis.Pass) (any, error) {
	folder, folderBanned := bannedFolder(pass)
	for _, file := range pass.Files {
		// Renaming the folder is the moment to rename a package clause that matches it, so a file whose folder and
		// clause are both banned is reported once, for the folder.
		if folderBanned {
			message := utilsFolderMessage
			if strings.HasPrefix(folder, "_") {
				message = underscoreUtilsFolderMessage
			}
			report(pass, file, message, map[string]string{"folder": folder, "replacement": replacement(folder)})
			continue
		}
		if isBanned(file.Name.Name) {
			report(pass, file, utilsPackageNameMessage, map[string]string{
				"package":     file.Name.Name,
				"replacement": replacement(file.Name.Name),
			})
		}
	}
	return nil, nil
}

// bannedFolder is the first folder of the package's path below its module root whose name is banned. The folders are
// read from the import path rather than from a file's path, which for a cgo package is a translation in the build
// cache and which a //line directive can rename. The module's own path is trimmed first, since a module named utils
// is nobody's to rename from inside it. Where the pass has no module path, as in analysistest's `GOPATH` layout, the
// import path is already relative to its root, so all of it is judged.
func bannedFolder(pass *analysis.Pass) (string, bool) {
	relative := pass.Pkg.Path()
	if pass.Module != nil && pass.Module.Path != "" {
		if relative == pass.Module.Path {
			return "", false
		}
		relative = strings.TrimPrefix(relative, pass.Module.Path+"/")
	}
	// An external test package's last element is its folder's name with _test after it, and its files sit in that
	// folder, so they are judged by the folder, each file once, as the package it tests is.
	if strings.HasSuffix(pass.Pkg.Name(), externalTestSuffix) {
		relative = strings.TrimSuffix(relative, externalTestSuffix)
	}
	for folder := range strings.SplitSeq(relative, "/") {
		if isBanned(folder) {
			return folder, true
		}
	}
	return "", false
}

// isBanned reports whether name, lowercased and with its leading underscores removed, is utils or util. The name is
// matched whole, so utilities, myutils and utilsomething pass, since a rule that fires on the spelling it asks for is
// worse than no rule. util is banned beside utils, the common spelling in Go, in every engine alike.
func isBanned(name string) bool {
	switch strings.ToLower(strings.TrimLeft(name, "_")) {
	case "utils", "util":
		return true
	}
	return false
}

// replacement is the house spelling for a banned name: utilities, lowercase whatever casing the name was written in,
// after the same leading underscores.
func replacement(name string) string {
	return strings.Repeat("_", len(name)-len(strings.TrimLeft(name, "_"))) + "utilities"
}

// report flags file at its start, a point rather than a range, since the finding is about the file's path and no
// text inside the file is to blame. A finding in a generated file is reported like any other and left to the
// engine, which withholds a house rule's findings there and counts them.
func report(pass *analysis.Pass, file *ast.File, message policy.MessageHandle, values map[string]string) {
	pass.Report(analysis.Diagnostic{
		Pos:      file.FileStart,
		End:      file.FileStart,
		Category: message.Id,
		Message:  message.Render(values),
	})
}
