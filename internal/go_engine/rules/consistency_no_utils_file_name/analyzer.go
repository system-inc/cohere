// Package consistency_no_utils_file_name is the Go engine's rule that no file's name holds `util` or `utils` as a word:
// util.go, string_utils.go and util_test.go are each reported once, at the file's start (#2xt6v5r).
//
// A file named for nothing in particular collects whatever has no other home, which is what a folder of that name
// does, one level down, and Go's own guidance says of a util package what holds of a util file. This is the Go form of
// nexus/consistency-no-utils-file-name and cohere-swift's rule of the same name, and reads a file name's words through
// internal/file_name_words, the judgment the TypeScript rule reads too (the text rules family `utils-file-name`): the
// name is cut at its underscores, go/build's `_test` and build-constraint suffixes are no words, and each word is held
// whole to file_name_words.IsUtils, the reading cohere-go/consistency-no-utils-folder gives a folder, so utilities.go
// and stringutils.go pass.
//
// A generated file's name is its generator's, so a file go/ast.IsGenerated accepts is not judged, and neither is
// doc.go, whose name the Go project's convention fixes, nor a file of a package under a folder whose files are someone
// else's (file_name_words.IsForeignFolder). The go tool loads no package under testdata or vendor at all.
package consistency_no_utils_file_name

import (
	"go/ast"
	"path/filepath"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/file_name_words"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-utils-file-name"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_utils_file_name",
	Doc: "Flags every file whose name holds util or utils as a word, in any casing, once at the file's start: the " +
		"name is cut at its underscores, with go/build's _test and build-constraint suffixes set aside. A generated " +
		"file, doc.go, and a file under a vendor, third_party or testdata folder are not judged.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads file names and the import path, which a load has
// before any type is checked, so it reads no type information.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var utilsFileNameMessage = policy.MessageOf(Name, "utilsFileName")

func run(pass *analysis.Pass) (any, error) {
	modulePath := ""
	if pass.Module != nil {
		modulePath = pass.Module.Path
	}
	if file_name_words.InForeignFolder(file_name_words.GoModuleFolders(pass.Pkg.Path(), modulePath)) {
		return nil, nil
	}
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		// The name the file has on disk, read at its start, which no //line directive reaches.
		fileName := filepath.Base(pass.Fset.Position(file.FileStart).Filename)
		words, judged := file_name_words.Words(fileName, file_name_words.LanguageGo)
		if !judged {
			continue
		}
		word, found := file_name_words.UtilsWord(words)
		if !found {
			continue
		}
		// A point at the file's start rather than a range, since the finding is about the file's name and no text
		// inside the file is to blame, as every engine's folder finding is.
		pass.Report(analysis.Diagnostic{
			Pos:      file.FileStart,
			End:      file.FileStart,
			Category: utilsFileNameMessage.Id,
			Message: utilsFileNameMessage.Render(map[string]string{
				"fileName":    fileName,
				"word":        word,
				"replacement": file_name_words.UtilitiesReplacement(word),
			}),
		})
	}
	return nil, nil
}
