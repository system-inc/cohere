// Package consistency_no_utils_folder is the Go engine's rule that no package lives in a folder named utils, util or
// _utils, or is named that in its package clause.
//
// A folder named for nothing in particular collects everything, and Go's own guidance on package names says the same
// of util: a caller reads utils.Parse and learns nothing from the qualifier. "utilities" is not meaningfully better
// at that, but it is the spelling the house settled on, and one spelling searched is worth two spellings guessed.
// This is the Go form of nexus/consistency-no-utils-folder and cohere-swift's rule of the same name.
package consistency_no_utils_folder

import (
	"go/ast"
	"path"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-utils-folder"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_utils_folder",
	Doc: "Flags a package whose folder, the last element of its import path, or whose package clause is utils, " +
		"util or _utils, once per package at the package clause of its first file that is not generated. Only " +
		"the package's own folder is judged, never the folders above it, since the pass does not know where the " +
		"module starts.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads the import path and the package clause, both of
// which a load has before any type is checked, so it reads no type information.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var utilsFolderMessage = policy.MessageOf(Name, "noUtils")

var underscoreUtilsFolderMessage = policy.MessageOf(Name, "noUnderscoreUtils")

var utilsPackageNameMessage = policy.MessageOf(Name, "utilsPackageName")

// replacements maps each banned name to the house spelling that replaces it. The names are matched whole and as
// written: utilities, myutils and utilsomething pass, since a rule that fires on the spelling it asks for is worse
// than no rule, and casings Go's lowercase package names never take are left alone rather than guessed at. util is
// banned in Go alone, the common spelling there, and takes the same house spelling as utils.
var replacements = map[string]string{
	"utils":  "utilities",
	"util":   "utilities",
	"_utils": "_utilities",
}

func run(pass *analysis.Pass) (any, error) {
	first := firstWrittenFile(pass)
	if first == nil {
		return nil, nil
	}
	// The folder is read from the import path, whose last element names the package's folder, rather than from a
	// file's path, which for a cgo package is a translation in the build cache and which a //line directive can
	// rename. Only the last element is judged: the pass does not know where the module starts, and judging every
	// element would flag a module path or a GOPATH folder nobody inside the package can rename. An external test
	// package's import path ends in _test, so its folder, the one the package it tests already answers for, is
	// never reported twice.
	folder := path.Base(pass.Pkg.Path())
	if replacement, banned := replacements[folder]; banned {
		message := utilsFolderMessage
		if strings.HasPrefix(folder, "_") {
			message = underscoreUtilsFolderMessage
		}
		// Renaming the folder is the moment to rename a package clause that matches it, so a package whose folder
		// and clause are both banned is reported once, for the folder.
		report(pass, first, message, map[string]string{"folder": folder, "replacement": replacement})
		return nil, nil
	}
	if replacement, banned := replacements[first.Name.Name]; banned {
		report(pass, first, utilsPackageNameMessage, map[string]string{
			"package":     first.Name.Name,
			"replacement": replacement,
		})
	}
	return nil, nil
}

// firstWrittenFile is the package's first file that is not generated, where the package's one finding sits, or nil
// when every file is generated.
func firstWrittenFile(pass *analysis.Pass) *ast.File {
	for _, file := range pass.Files {
		// A generated file's package clause is its generator's, and a finding there points a reader at a file they
		// cannot edit. This is the one place the rule skips a file, so a package whose every file is generated is
		// its generator's to name and is not judged.
		if ast.IsGenerated(file) {
			continue
		}
		return file
	}
	return nil
}

// report flags file's package clause, which names the package a reader is about to rename.
func report(pass *analysis.Pass, file *ast.File, message policy.MessageHandle, values map[string]string) {
	pass.Report(analysis.Diagnostic{
		Pos:      file.Package,
		End:      file.Name.End(),
		Category: message.Id,
		Message:  message.Render(values),
	})
}
