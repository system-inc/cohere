package rule

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// Inherited keys, in a pass's ResultOf, the build's Inheritance, which the engine's checker hands every pass when its
// checker.Options.Inherited is this key and its Options.Inheritance is set (#y6q1x2j): the host's findings of each
// syntax-only rule on each file the host checked, which an extra build reports as the rule's own rather than walk the
// file again. It lives beside Resolved, for Resolved's reasons, and like it is a key and never runs: no analyzer
// requires it, so it is in no graph. One that required it would fail on every package, naming it.
var Inherited = &analysis.Analyzer{
	Name: "inherited",
	Doc:  "keys the host's findings an extra build inherits, which the checker hands every pass, and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of the inheritance the checker "+
			"hands every pass, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}

// Inheritance is what an extra build takes of the host's run for its syntax-only rules (see Listener.SyntaxOnly and
// SyntaxOnlyRun): a rule whose findings on a file depend on nothing but the file's bytes and the settings has the
// same findings on that file in every build of a run, which reads every file once, so a build checking a package from
// source takes the host's findings on each file the host checked rather than walk it again (#y6q1x2j).
type Inheritance interface {
	// Inherits is whether the rule named ruleName takes the host's findings on filename, the name the file was parsed
	// from: the host checked the file, in a package whose kept entry holds the rule's analysis there.
	Inherits(ruleName string, filename string) bool
	// Findings is the host's findings of the rule named ruleName on filename, every one, before any dedupe or
	// withholding, for a file Inherits answers true of.
	Findings(ruleName string, filename string) []InheritedFinding
}

// InheritedFinding is one of the host's findings an extra build inherits, as check.Finding holds it, in a type of its
// own here, since check imports rule: where it starts and ends in its file, 1-based, its column a byte's, as
// token.Position counts, its message identifier and message, and its suggested fixes.
type InheritedFinding struct {
	Line        int
	Column      int
	EndLine     int
	EndColumn   int
	MessageId   string
	Message     string
	Suggestions []InheritedSuggestion
}

// InheritedSuggestion is one suggested fix of an inherited finding: what it says, and its edits.
type InheritedSuggestion struct {
	Message string
	Edits   []InheritedEdit
}

// InheritedEdit is one edit of a suggested fix: the file it edits, by the name it was parsed from, the byte offsets it
// replaces, from Start to End, and what it writes there.
type InheritedEdit struct {
	File  string
	Start int
	End   int
	Text  string
}

// InheritedFiles is what the rule named ruleName inherits on pass's package from the build's Inheritance, which the
// checker hands pass under Inherited: the files it takes the host's findings on, which it never walks, and those
// findings as diagnostics in pass's own file set, in the order of pass's files. A pass without an Inheritance inherits
// nothing. A file whose findings cannot all be placed in pass's files, an edit into a file outside the package or a
// position past the file's end, is not inherited, decided here before any walk, so the rule judges it itself.
func InheritedFiles(pass *analysis.Pass, ruleName string) (map[*token.File]bool, []analysis.Diagnostic) {
	inheritance, inherits := pass.ResultOf[Inherited].(Inheritance)
	if !inherits {
		return nil, nil
	}
	// The package's files by the name each was parsed from, which is how the host's findings name them, read only
	// once some file of the package is inherited.
	var tokenFiles map[string]*token.File
	inherited := map[*token.File]bool{}
	var diagnostics []analysis.Diagnostic
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		if tokenFile == nil || !inheritance.Inherits(ruleName, tokenFile.Name()) {
			continue
		}
		if tokenFiles == nil {
			tokenFiles = packageTokenFiles(pass)
		}
		placed, isPlaced := placedFindings(inheritance.Findings(ruleName, tokenFile.Name()), tokenFile, tokenFiles)
		if !isPlaced {
			continue
		}
		inherited[tokenFile] = true
		diagnostics = append(diagnostics, placed...)
	}
	return inherited, diagnostics
}

// SyntaxOnlyRun is run, the Run of a rule's own analyzer that walks pass.Files itself, for the rule named ruleName,
// which the registry marks a syntax-only walk (ValidateSyntaxOnlyWalk): run is handed a pass holding only the files the
// rule does not inherit (InheritedFiles), and once it returns without an error the inherited findings are reported on
// the pass as its own, after its own. Whether or not it inherits, run's pass holds no type information, no package and
// no sizes, so a walk marked syntax-only that reads any of them panics in its own fixtures rather than pass silently
// while an extra build hands on the host's findings, which only a file's bytes and the settings may decide.
//
//cohere:ignore consistency-no-any an analyzer's Run returns its result as any, which SyntaxOnlyRun hands on untouched
func SyntaxOnlyRun(ruleName string, run func(*analysis.Pass) (any, error)) func(*analysis.Pass) (any, error) {
	return func(pass *analysis.Pass) (any, error) {
		inherited, diagnostics := InheritedFiles(pass, ruleName)
		// A copy of the pass, every function of it the checker's, so what run reports and exports lands on the action
		// as it would from pass, with the inherited files and everything a syntax-only walk may not read left out.
		own := *pass
		own.TypesInfo = nil
		own.Pkg = nil
		own.TypesSizes = nil
		if len(inherited) == 0 {
			return run(&own)
		}
		own.Files = slices.DeleteFunc(slices.Clone(pass.Files), func(file *ast.File) bool {
			return inherited[pass.Fset.File(file.FileStart)]
		})
		result, runError := run(&own)
		if runError != nil {
			return result, runError
		}
		for _, diagnostic := range diagnostics {
			pass.Report(diagnostic)
		}
		return result, nil
	}
}

// packageTokenFiles is each of pass's files' token file, by the name it was parsed from.
func packageTokenFiles(pass *analysis.Pass) map[string]*token.File {
	tokenFiles := make(map[string]*token.File, len(pass.Files))
	for _, file := range pass.Files {
		if tokenFile := pass.Fset.File(file.FileStart); tokenFile != nil {
			tokenFiles[tokenFile.Name()] = tokenFile
		}
	}
	return tokenFiles
}

// placedFindings is findings, the host's on tokenFile, as diagnostics at the same lines and columns of tokenFile, each
// edit at its offsets in its own file among tokenFiles, the package's, and whether every one of them could be placed.
// Its Category is the host's message identifier, which newFinding reads back as the finding's own.
func placedFindings(
	findings []InheritedFinding,
	tokenFile *token.File,
	tokenFiles map[string]*token.File,
) ([]analysis.Diagnostic, bool) {
	diagnostics := make([]analysis.Diagnostic, 0, len(findings))
	for _, finding := range findings {
		position, positionPlaced := placedPosition(tokenFile, finding.Line, finding.Column)
		if !positionPlaced {
			return nil, false
		}
		diagnostic := analysis.Diagnostic{Pos: position, Category: finding.MessageId, Message: finding.Message}
		// A finding without an end has none here either, as newFinding left its end at 0.
		if finding.EndLine > 0 {
			end, endPlaced := placedPosition(tokenFile, finding.EndLine, finding.EndColumn)
			if !endPlaced {
				return nil, false
			}
			diagnostic.End = end
		}
		for _, suggestion := range finding.Suggestions {
			fix := analysis.SuggestedFix{Message: suggestion.Message}
			for _, edit := range suggestion.Edits {
				editFile, held := tokenFiles[edit.File]
				if !held || edit.Start < 0 || edit.Start > edit.End || edit.End > editFile.Size() {
					return nil, false
				}
				fix.TextEdits = append(fix.TextEdits, analysis.TextEdit{Pos: editFile.Pos(edit.Start),
					End: editFile.Pos(edit.End), NewText: []byte(edit.Text)})
			}
			diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, fix)
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics, true
}

// placedPosition is the position of line and column, a byte's, 1-based, in tokenFile, and false when tokenFile has no
// such line or the column runs past its end.
func placedPosition(tokenFile *token.File, line int, column int) (token.Pos, bool) {
	if line < 1 || line > tokenFile.LineCount() || column < 1 {
		return token.NoPos, false
	}
	offset := tokenFile.Offset(tokenFile.LineStart(line)) + column - 1
	if offset > tokenFile.Size() {
		return token.NoPos, false
	}
	return tokenFile.Pos(offset), true
}
