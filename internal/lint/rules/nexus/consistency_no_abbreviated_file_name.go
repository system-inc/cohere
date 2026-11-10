package nexus

import (
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/file_name_words"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// consistencyNoAbbreviatedFileNameText are the rule's two messages, by the id file_name_words.Advice names, whose
// wording lives in `policy/messages/consistency-no-abbreviated-file-name.json`.
var consistencyNoAbbreviatedFileNameText = map[string]policy.MessageHandle{
	file_name_words.AbbreviatedFileName: policy.MessageOf(
		"nexus/consistency-no-abbreviated-file-name",
		file_name_words.AbbreviatedFileName,
	),
	file_name_words.AbbreviatedFileNameAdvice: policy.MessageOf(
		"nexus/consistency-no-abbreviated-file-name",
		file_name_words.AbbreviatedFileNameAdvice,
	),
}

// registeredRuleNames are the TypeScript registry's rule names, read once, after every package's init has
// registered its rules, which is before any rule runs. A file named for one of them, `max-lines` or
// `@typescript-eslint/no-unused-vars`, is named for a rule, upstream's name for a port, and only the words beside the
// name are judged (file_name_words.Abbreviated).
var registeredRuleNames = sync.OnceValue(func() file_name_words.RuleNames {
	registered := rule.Registered()
	names := make([]string, 0, len(registered))
	for _, entry := range registered {
		names = append(names, entry.Name)
	}
	return file_name_words.NewRuleNames(names)
})

// ConsistencyNoAbbreviatedFileName refuses a file name whose words abbreviate a word the house spells out, by the
// vocabulary nexus/consistency-no-abbreviated-identifier reads, policy/Abbreviations.json: `cfgLoader.ts`,
// `msg-printer.ts` and `errorMsg.test.ts` are each reported once, at the file's start (#2xt6v5r, the text rules
// family `abbreviated-file-name`). A file's name is read on every path and every import, so it is held to the words
// an identifier is.
//
// The words are internal/file_name_words', the judgment the Go engine's cohere-go/consistency-no-abbreviated-file-name
// reads too: the name is cut into words as nexus/consistency-no-utils-file-name cuts it (typeScriptFileNameWords),
// with the same files left alone, and its words are read as the one camelCase identifier they spell, so a word is
// judged in a file name exactly where it would be in an identifier holding the same words. The message names the word
// and how to spell it out, never a rewritten file name, since the name's own convention is its author's.
//
// A file named for a rule this engine registers holds the rule's name, upstream's for a port, which is not the file's
// author's to spell out, so a run of words spelling one is set aside and the words beside it judged:
// MaxClassesPerFileRule.ts is judged on `Rule` alone. The names are the registry's (registeredRuleNames), never a list
// kept here.
//
// The identifier rule's framework exemptions are not this rule's. They name properties and bindings a framework reads
// by spelling, and a file Next.js reads by name is left alone before any word is judged.
//
// The rule reads only the path, so it decides on a string and declines the file before any node is visited.
var ConsistencyNoAbbreviatedFileName = rule.Rule[ProjectRootOptions]{
	Name:       "nexus/consistency-no-abbreviated-file-name",
	NoListener: rule.NoListenerDeclinesIrrelevantFiles,
	Run: func(
		ruleContext rule.Context,
		options rule.Given[ProjectRootOptions],
	) rule.Listeners {
		if ruleContext.SourceFile == nil {
			return nil
		}
		settings, configured := options.Read()
		if !configured {
			// No root was handed in, so a folder of someone else's inside the project cannot be told from the
			// checkout's own folders, as nexus/consistency-no-utils-file-name declines then too.
			return nil
		}
		fileName, words, judged := typeScriptFileNameWords(
			settings.ProjectRoot,
			ruleContext.SourceFile.FileName().AsString(),
		)
		if !judged {
			return nil
		}
		finding, found := file_name_words.Abbreviated(words, registeredRuleNames())
		if !found {
			return nil
		}
		id, values := file_name_words.Advice(fileName, finding.Entry)
		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				ruleContext.ReportRange(node.Loc.WithEnd(node.Loc.Pos()), rule.Message{
					Id:          consistencyNoAbbreviatedFileNameText[id].Id,
					Description: consistencyNoAbbreviatedFileNameText[id].Render(values),
				})
			},
		}
	},
}
