package rule

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// Standing keys, in a pass's ResultOf, the package's StandingBodies, which the engine's checker hands every pass on a
// package when its checker.Options.Standing is this key and its Options.StandingBodies answers for the package
// (#e4rd5dw): the function declarations an extra build leaves to the host there, whose bodies a confined rule never
// hears and whose findings it reports from the host's as its own. It lives beside Inherited, the same seam one level
// up, a whole file, and like it is a key and never runs: no analyzer requires it, so it is in no graph. One that
// required it would fail on every package, naming it.
var Standing = &analysis.Analyzer{
	Name: "standing",
	Doc: "keys the function bodies an extra build leaves to the host, which the checker hands every pass, and never " +
		"runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of the standing bodies the checker "+
			"hands every pass, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}

// StandingBodies is what an extra build leaves to the host in one package (#e4rd5dw). A build's difference from the
// host reaches only some of the function bodies of a package it checks from source, and every other function
// declaration with a body, in a file both builds compile, stands: a confined rule, one that is dispatched and whose
// findings in a unit depend on that unit alone, finds in it what it found on the host, so the build takes the host's
// findings there rather than hear the body again. Only a function declaration's body stands. A type, variable or
// constant declaration, a value's initializer and the function literals inside them are always the build's own.
//
// Both dispatchers and every rule's own action read it, so every method may be called from several goroutines at
// once.
type StandingBodies interface {
	// Stands is whether the function declared at offset, the offset of its `func` in the file parsed from filename,
	// stands.
	Stands(filename string, offset int) bool
	// Confines is whether the rule named ruleName is confined. Any other rule hears every body, as on the host, and
	// keeps its own findings.
	Confines(ruleName string) bool
	// Findings is the host's findings of the rule named ruleName on filename, from the host's entry of this same
	// package, every one, before any dedupe or withholding, and false when the host holds none for it there, when the
	// rule judges the whole file itself.
	Findings(ruleName string, filename string) ([]InheritedFinding, bool)
	// Took is told, once the rule named ruleName has reported the host's findings in filename's standing declarations
	// as its own (ReportStanding), how many declarations stood there for it and how many findings it reported in
	// them, so a build counts what it showed of the host's, never what it asked for and then dropped: a file whose
	// findings would not place, or a rule that crashed.
	Took(ruleName string, filename string, declarations int, findings int)
}

// StandingSpan is one standing function declaration in a pass's file, as three positions: Start, where it starts
// counting its doc comment, Body, its body's opening brace, and End, just past its closing one. A rule confined on
// the file hears no node from Body to End, though it hears the declaration itself and its receiver, name and type,
// and from Start to End its findings are the host's.
type StandingSpan struct {
	Start token.Pos
	Body  token.Pos
	End   token.Pos
}

// StandingSpans is each standing function declaration of pass's package, by its token file and sorted by where it
// starts, from the package's StandingBodies, which the checker hands pass under Standing. A dispatcher reads them
// once as it starts, for every listener. A pass without a StandingBodies, the host's and any driver's but the
// engine's, has none, and neither has a package nothing stands in.
func StandingSpans(pass *analysis.Pass) map[*token.File][]StandingSpan {
	standing, stands := pass.ResultOf[Standing].(StandingBodies)
	if !stands {
		return nil
	}
	var spans map[*token.File][]StandingSpan
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		if tokenFile == nil {
			continue
		}
		var fileSpans []StandingSpan
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			span := StandingSpan{Start: function.Pos(), Body: function.Body.Lbrace, End: function.End()}
			if function.Doc != nil {
				span.Start = function.Doc.Pos()
			}
			// A declaration whose brace is not between its start and its end, as a parse past an error can leave one,
			// stands for nothing: a body's span read from it would cover what is not the body's.
			if span.Start > span.Body || span.Body >= span.End {
				continue
			}
			if standing.Stands(tokenFile.Name(), tokenFile.Offset(function.Pos())) {
				fileSpans = append(fileSpans, span)
			}
		}
		if len(fileSpans) == 0 {
			continue
		}
		// A parsed file's declarations are in source order already. Sorted all the same, since every search of a
		// file's spans, and the syntax walk's one pass over them, reads them in order.
		slices.SortFunc(fileSpans, func(one StandingSpan, other StandingSpan) int {
			return cmp.Compare(one.Start, other.Start)
		})
		if spans == nil {
			spans = map[*token.File][]StandingSpan{}
		}
		spans[tokenFile] = fileSpans
	}
	return spans
}

// StandingFiles is what the rule named ruleName takes of the host over pass's standing declarations, spans
// (StandingSpans): the files it is confined on, each with its spans, and the host's findings inside those spans as
// diagnostics in pass's own file set, in the order of pass's files. On a file it is confined on, the rule hears no
// standing body and its own findings inside a standing span are dropped for the host's (OutsideStanding,
// ReportStanding). It is the sibling of InheritedFiles one level down, a declaration where that is a file, and a file
// among inherited, which the rule takes whole, stands in no part.
//
// A file is confined for the rule only when the package's StandingBodies confines the rule, the file has a standing
// span, the host holds the rule's findings on it, and every one of them places in the file. Otherwise the rule hears
// the whole file and keeps its own findings there, decided here before any walk, so every doubt lints. A finding that
// edits any other file, of the package or not, does not place: that file may be one the rule judges for itself. Of
// the host's findings on a confined file, those outside every span are dropped, since the build judges those places
// itself.
func StandingFiles(
	pass *analysis.Pass,
	ruleName string,
	spans map[*token.File][]StandingSpan,
	inherited map[*token.File]bool,
) (map[*token.File][]StandingSpan, []analysis.Diagnostic) {
	if len(spans) == 0 {
		return nil, nil
	}
	standing, stands := pass.ResultOf[Standing].(StandingBodies)
	if !stands || !standing.Confines(ruleName) {
		return nil, nil
	}
	var confined map[*token.File][]StandingSpan
	var diagnostics []analysis.Diagnostic
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		fileSpans := spans[tokenFile]
		if len(fileSpans) == 0 || inherited[tokenFile] {
			continue
		}
		findings, held := standing.Findings(ruleName, tokenFile.Name())
		if !held {
			continue
		}
		// Placed among the file alone, so an edit into any other file fails the file.
		placed, isPlaced := placedFindings(findings, tokenFile, map[string]*token.File{tokenFile.Name(): tokenFile})
		if !isPlaced {
			continue
		}
		if confined == nil {
			confined = map[*token.File][]StandingSpan{}
		}
		confined[tokenFile] = fileSpans
		for _, diagnostic := range placed {
			if inStandingSpan(fileSpans, diagnostic.Pos) {
				diagnostics = append(diagnostics, diagnostic)
			}
		}
	}
	return confined, diagnostics
}

// OutsideStanding is diagnostics, a rule's own findings over a package, less each one placed inside a standing span
// of a file the rule is confined on (StandingFiles), doc comment included, which the host's findings there replace
// (ReportStanding). A rule confined on a file still hears each standing declaration's header, and its FileExit and
// PackageExit may report anywhere, so what it finds from there inside a span would otherwise be reported twice, once
// as its own and once as the host's. A rule confined on no file keeps every finding, at the cost of reading confined's
// length.
func OutsideStanding(
	fileSet *token.FileSet,
	confined map[*token.File][]StandingSpan,
	diagnostics []analysis.Diagnostic,
) []analysis.Diagnostic {
	if len(confined) == 0 {
		return diagnostics
	}
	outside := make([]analysis.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		// A finding with no position is in no file, and no span.
		if tokenFile := fileSet.File(diagnostic.Pos); tokenFile != nil &&
			inStandingSpan(confined[tokenFile], diagnostic.Pos) {
			continue
		}
		outside = append(outside, diagnostic)
	}
	return outside
}

// ReportStanding reports diagnostics, the host's findings inside the standing declarations of the files the rule
// named ruleName is confined on (StandingFiles), on pass as the rule's own, and tells the package's StandingBodies,
// for each of those files, how many declarations stood there and how many findings it reported in them
// (StandingBodies.Took), so the build counts what it showed. Only a rule whose run finished without a crash calls it.
func ReportStanding(
	pass *analysis.Pass,
	ruleName string,
	confined map[*token.File][]StandingSpan,
	diagnostics []analysis.Diagnostic,
) {
	if len(confined) == 0 {
		return
	}
	reported := make(map[*token.File]int, len(confined))
	for _, diagnostic := range diagnostics {
		pass.Report(diagnostic)
		reported[pass.Fset.File(diagnostic.Pos)]++
	}
	standing, stands := pass.ResultOf[Standing].(StandingBodies)
	if !stands {
		return
	}
	for tokenFile, fileSpans := range confined {
		standing.Took(ruleName, tokenFile.Name(), len(fileSpans), reported[tokenFile])
	}
}

// inStandingSpan is whether position lies inside one of spans, a file's, sorted by where each starts: from a span's
// Start to its End, both included. Nothing of another declaration starts at a span's End, which is the newline or the
// semicolon that ends the declaration, or the file's end, so a finding placed there is the standing declaration's.
func inStandingSpan(spans []StandingSpan, position token.Pos) bool {
	index, starts := slices.BinarySearchFunc(spans, position, func(span StandingSpan, position token.Pos) int {
		return cmp.Compare(span.Start, position)
	})
	// The span starting at position, or else the last one starting before it, which is the only one that can hold it.
	return starts || (index > 0 && position <= spans[index-1].End)
}
