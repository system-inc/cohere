// Package consistency_require_separated_folder_words is the Go engine's rule that the words in a package's folder
// name are separated by underscores, never run together: go_engine, not goengine.
//
// A mind reading cold reads format_options at a glance and has to stop and parse formatoptions, and a folder's name
// is read before anything in it, on every import line and every path. The rule reads the package's folder, the last
// element of its import path below the module root, and its package clause where the clause differs from the folder
// and is not main. A name is judged one underscore-separated part at a time by internal/folder_words, the judgment
// TypeScript's nexus/consistency-require-separated-folder-words reads too: a part that is a word passes, and a part
// that splits into two or more words is the finding, with the split as the spelling it asks for. The words are
// policy's, so a word the lists lack is a reviewed change to policy, never a carve-out here.
//
// testdata and vendor folders are never judged, since the engine never loads a package inside either. A package at
// its module's root is not judged by its folder, whose name is the module's (typescript-go's tsoptions shim is one).
// A finding in a file that declares itself `cohere:copied` is withheld by the engine, not the rule, as every house
// rule's is there, so a copied package such as x/tools' analysistest needs nothing. Any other folder that mirrors
// another project's name, such as the micromark library's, keeps it through the house suppression, on its own line
// directly above the package clause the finding is at:
//
//	//cohere:ignore cohere-go/consistency-require-separated-folder-words the micromark library's name
//
// The rule knows no mirror itself, so the reason lives beside the name it excuses, and one that withholds nothing is
// a cohere-go/consistency-no-unused-ignore finding.
package consistency_require_separated_folder_words

import (
	"go/ast"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/folder_words"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-require-separated-folder-words"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_require_separated_folder_words",
	Doc: "Flags a package whose folder, the last element of its import path below its module root, runs two or more " +
		"words together, or whose package clause does where it differs from the folder and is not main, once per " +
		"package at the package clause of its first file by name that is neither a test nor generated.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads the import path, the file names and the package
// clause, all of which a load has before any type is checked, so it reads no type information.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var runTogetherFolderMessage = policy.MessageOf(Name, "runTogetherFolder")

var runTogetherPackageNameMessage = policy.MessageOf(Name, "runTogetherPackageName")

// externalTestSuffix ends the import path and the package clause of an external test package.
const externalTestSuffix = "_test"

// testFileSuffix ends the name of every file go test builds and go build leaves out.
const testFileSuffix = "_test.go"

// mainPackageName is the clause of a command, which names no folder and is never renamed.
const mainPackageName = "main"

// goSpelling separates a name's words with the underscore alone, the one separator a package clause may hold, so a
// folder and its clause are cut into the same parts and a split of either is spelled as the other would be.
var goSpelling = folder_words.Spelling{Separators: "_", Joiner: "_"}

func run(pass *analysis.Pass) (any, error) {
	file := reportedFile(pass)
	if file == nil {
		return nil, nil
	}
	folder, folderJudged := packageFolder(pass)
	// Renaming the folder is the moment to rename a clause that runs together too, so a package whose folder is a
	// finding is reported once, for the folder, as consistency-no-utils-folder does.
	if folderJudged {
		if replacement, runTogether := folder_words.Separated(folder, goSpelling); runTogether {
			report(pass, file, runTogetherFolderMessage, map[string]string{
				"folder":      folder,
				"replacement": replacement,
			})
			return nil, nil
		}
	}
	clause := file.Name.Name
	if strings.TrimSuffix(clause, externalTestSuffix) == folder || clause == mainPackageName {
		return nil, nil
	}
	if replacement, runTogether := folder_words.Separated(clause, goSpelling); runTogether {
		report(pass, file, runTogetherPackageNameMessage, map[string]string{
			"package":     clause,
			"replacement": replacement,
		})
	}
	return nil, nil
}

// packageFolder is the package's folder, the last element of its import path, read from the import path rather than
// from a file's path, which for a cgo package is a translation in the build cache. An external test package's path is
// its folder's with _test after it, so the suffix is trimmed and the folder judged as the package it tests is. The
// folder is not judged when the package is its module's root, whose name is the module's and nobody's to rename from
// inside it; the folder is still returned, so the clause is compared with it. Where the pass has no module, as in
// analysistest's `GOPATH` layout, the import path is already relative to its root.
func packageFolder(pass *analysis.Pass) (string, bool) {
	importPath := pass.Pkg.Path()
	if strings.HasSuffix(pass.Pkg.Name(), externalTestSuffix) {
		importPath = strings.TrimSuffix(importPath, externalTestSuffix)
	}
	isModuleRoot := pass.Module != nil && pass.Module.Path != "" && importPath == pass.Module.Path
	return path.Base(importPath), !isModuleRoot
}

// reportedFile is the file the package's one finding is at: the first by base name that is neither a test nor
// generated, so the finding sits in the source a reader opens first and a directive excusing it has one home, or the
// first of all when every file is one or the other, as in an external test package. The name is the one a finding
// prints, a //line directive's included, and is compared whole, so the order is the same on every run and machine.
func reportedFile(pass *analysis.Pass) *ast.File {
	if len(pass.Files) == 0 {
		return nil
	}
	files := slices.Clone(pass.Files)
	slices.SortStableFunc(files, func(left *ast.File, right *ast.File) int {
		return strings.Compare(baseName(pass, left), baseName(pass, right))
	})
	for _, file := range files {
		if !strings.HasSuffix(baseName(pass, file), testFileSuffix) && !ast.IsGenerated(file) {
			return file
		}
	}
	return files[0]
}

// baseName is the name of the file a finding at file's package clause prints, without its folder.
func baseName(pass *analysis.Pass, file *ast.File) string {
	return filepath.Base(pass.Fset.Position(file.Package).Filename)
}

// report flags the package clause from its `package` keyword to the end of its name, since the clause is where the
// package names itself, and where a directive excusing a mirror sits directly above.
func report(pass *analysis.Pass, file *ast.File, message policy.MessageHandle, values map[string]string) {
	pass.Report(analysis.Diagnostic{
		Pos:      file.Package,
		End:      file.Name.End(),
		Category: message.Id,
		Message:  message.Render(values),
	})
}
