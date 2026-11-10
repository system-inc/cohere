// Package consistency_no_abbreviated_file_name is the Go engine's rule that a file's name abbreviates no word the house
// spells out, by the vocabulary cohere-go/consistency-no-abbreviated-identifier reads, policy/Abbreviations.json:
// cfg.go, msg_printer.go and error_msg_test.go are each reported once, at the file's start (#2xt6v5r).
//
// A file's name is read on every path, so it is held to the words an identifier is. This is the Go form of
// nexus/consistency-no-abbreviated-file-name and cohere-swift's rule of the same name, and reads a file name's words
// through internal/file_name_words, the judgment the TypeScript rule reads too (the text rules family
// `abbreviated-file-name`): the name is cut at its underscores, go/build's `_test` and build-constraint suffixes are
// set aside, and its words are read as the one camelCase identifier they spell, so msg_printer.go is judged as
// msgPrinter would be. The message names the word and how to spell it out.
//
// A file named for a registered rule holds the rule's name, upstream's for a port, which is not the file's author's to
// spell out: max_lines.go is max-lines, and no_var_cfg.go is no-var and `cfg`, judged on `cfg` alone. The names are
// every engine's, from docs/data/rules.json (registeredRuleNames). Otherwise the identifier rule's reading of whose a
// name is has no counterpart here: a file's name is the package's own.
// A generated file's name is its generator's, so a file go/ast.IsGenerated accepts is not judged, and neither is
// doc.go, whose name the Go project's convention fixes, nor a file of a package under a folder whose files are someone
// else's (file_name_words.IsForeignFolder). The go tool loads no package under testdata or vendor at all.
package consistency_no_abbreviated_file_name

import (
	"go/ast"
	"path/filepath"
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/docs"
	"github.com/system-inc/cohere/internal/file_name_words"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-abbreviated-file-name"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_abbreviated_file_name",
	Doc: "Flags every file whose name abbreviates a word in policy/Abbreviations.json, the house vocabulary every " +
		"engine shares, once at the file's start: the name's words, cut at its underscores with go/build's _test " +
		"and build-constraint suffixes set aside, are judged as the camelCase identifier they spell. A generated " +
		"file, doc.go, and a file under a vendor, third_party or testdata folder are not judged.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads file names and the import path, which a load has
// before any type is checked, so it reads no type information.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

// messages are the rule's two messages, by the id file_name_words.Advice names.
var messages = map[string]policy.MessageHandle{
	file_name_words.AbbreviatedFileName:       policy.MessageOf(Name, file_name_words.AbbreviatedFileName),
	file_name_words.AbbreviatedFileNameAdvice: policy.MessageOf(Name, file_name_words.AbbreviatedFileNameAdvice),
}

// registeredRuleNames are every engine's rule names, from docs/data/rules.json, read once. A Go file is named for a
// rule as often for a TypeScript one, internal/lint/rules/core/max_lines.go for max-lines, as for one of this
// engine's, and this engine's registry holds only its own, so the names are the generated list of every registry's
// rather than knownRuleNames.
var registeredRuleNames = sync.OnceValue(func() file_name_words.RuleNames {
	return file_name_words.NewRuleNames(docs.RuleNames())
})

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
		finding, found := file_name_words.Abbreviated(words, registeredRuleNames())
		if !found {
			continue
		}
		id, values := file_name_words.Advice(fileName, finding.Entry)
		// A point at the file's start rather than a range, since the finding is about the file's name and no text
		// inside the file is to blame, as every engine's folder finding is.
		pass.Report(analysis.Diagnostic{
			Pos:      file.FileStart,
			End:      file.FileStart,
			Category: messages[id].Id,
			Message:  messages[id].Render(values),
		})
	}
	return nil, nil
}
