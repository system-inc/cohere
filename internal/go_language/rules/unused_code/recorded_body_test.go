package unused_code

import (
	"crypto/sha256"
	"errors"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/lint"

	"github.com/system-inc/cohere/internal/go_language/rule"
)

// recordedSources are a package of three files: one a `//line` directive renames at its one function, so that body is
// keyed by the directive's name and parsed from the file's own, and one that holds no body, only the variable Plain's
// body reads.
var recordedSources = []recordedSource{
	{name: "plain.go", source: "package recorded\n\nfunc Plain() int {\n\tvalue := declared\n\treturn value\n}\n"},
	{name: "lined.go", source: "package recorded\n\n//line other.go:40\nfunc Lined() int {\n\treturn Plain()\n}\n"},
	{name: "declared.go", source: "package recorded\n\nvar declared = 1\n"},
}

// recordedPass is a pass of U1000 over sources, holding what its requirements hand it and, under rule.BodyRecords,
// asked, or no ask at all for nil. Its ReadFile serves each source by its name, but those of unreadable.
func recordedPass(test *testing.T, sources []recordedSource, asked *bool, unreadable ...string) *analysis.Pass {
	test.Helper()
	checked := checkShapes(test, "recorded", sources, nil)
	resultOf := map[*analysis.Analyzer]any{
		directivesAnalyzer: []lint.Directive(nil),
		generated.Analyzer: map[string]generated.Generator{},
	}
	if asked != nil {
		resultOf[rule.BodyRecords] = *asked
	}
	return &analysis.Pass{
		Analyzer:  Analyzer,
		Fset:      checked.fileSet,
		Files:     checked.files,
		Pkg:       checked.typesPackage,
		TypesInfo: checked.typesInfo,
		ResultOf:  resultOf,
		Report: func(analysis.Diagnostic) {
			// What U1000 reports is not what these tests read: its result is.
		},
		ReadFile: func(filename string) ([]byte, error) {
			for _, each := range sources {
				if each.name == filename && !slices.Contains(unreadable, filename) {
					return []byte(each.source), nil
				}
			}
			return nil, errors.New("no file " + filename)
		},
	}
}

// A pass with no ask under rule.BodyRecords, as every plain run's is, has a graph with no recorder, so it makes no
// record and pays for none, and its result hands none on; nor does one whose ask is false. Asked, the graph records.
func TestAPassNotAskedRecordsNoBody(test *testing.T) {
	test.Parallel()
	asked, unasked := true, false
	for _, each := range []struct {
		name  string
		asked *bool
		wants bool
	}{
		{name: "no ask", asked: nil},
		{name: "asked false", asked: &unasked},
		{name: "asked", asked: &asked, wants: true},
	} {
		pass := recordedPass(test, recordedSources, each.asked)
		packageDirectives, _ := pass.ResultOf[directivesAnalyzer].([]lint.Directive)
		generatedFiles, _ := pass.ResultOf[generated.Analyzer].(map[string]generated.Generator)
		if recording := passGraph(pass, packageDirectives, generatedFiles).recorder != nil; recording != each.wants {
			test.Errorf("with %s the graph records %t, want %t", each.name, recording, each.wants)
		}
		verdict, runError := run(pass)
		if runError != nil {
			test.Fatalf("running U1000 with %s: %v", each.name, runError)
		}
		if _, isRecorded := RecordedOf(verdict); isRecorded != each.wants {
			test.Errorf("with %s the result hands records on %t, want %t", each.name, isRecorded, each.wants)
		}
	}
}

// Asked, U1000's result hands on each body's record keyed as a standing body is keyed, the `//line` directive's name
// for Lined's and the file's own for Plain's, each with the file it was parsed from and the files it rests on, those
// declaring what it uses outside it, none its own, and the sha256 of every file's bytes as the pass read them,
// declared.go's though it holds no body. Neither record is flagged, so each is summarizable. A file the pass cannot
// read again has no digest, while its body's record is still handed on, so the host's side answers no record there
// (go_engine's factReachPackage.BodyRecord). Any other analyzer's result holds none.
func TestAnAskedRunHandsEachBodyOnWithTheFileItWasParsedFrom(test *testing.T) {
	test.Parallel()
	asked := true
	verdict, runError := run(recordedPass(test, recordedSources, &asked))
	if runError != nil {
		test.Fatalf("running U1000: %v", runError)
	}
	recorded, isRecorded := RecordedOf(verdict)
	if !isRecorded {
		test.Fatal("the asked run hands no records on")
	}
	type handed struct {
		file         string
		offset       int
		parsedFile   string
		files        string
		summarizable bool
	}
	var got []handed
	for _, body := range recorded.Bodies {
		file, offset := body.Key()
		got = append(got, handed{file: file, offset: offset, parsedFile: body.ParsedFile,
			files: strings.Join(body.Files(), " "), summarizable: body.Summarizable()})
	}
	want := []handed{{file: "plain.go", offset: 18, parsedFile: "plain.go", files: "declared.go", summarizable: true},
		{file: "other.go", offset: 37, parsedFile: "lined.go", files: "plain.go", summarizable: true}}
	if !slices.Equal(got, want) {
		test.Errorf("the run handed the bodies %+v, want %+v", got, want)
	}
	for _, each := range recordedSources {
		if digest := recorded.Digests[each.name]; digest != sha256.Sum256([]byte(each.source)) {
			test.Errorf("the digest of %s is %x, want the sha256 of its bytes", each.name, digest)
		}
	}
	if len(recorded.Digests) != len(recordedSources) {
		test.Errorf("the run digested %d files, want plain.go, lined.go and declared.go", len(recorded.Digests))
	}

	unread, unreadError := run(recordedPass(test, recordedSources, &asked, "lined.go"))
	if unreadError != nil {
		test.Fatalf("running U1000 with lined.go unreadable: %v", unreadError)
	}
	partly, _ := RecordedOf(unread)
	if _, isDigested := partly.Digests["lined.go"]; isDigested || len(partly.Bodies) != 2 {
		test.Errorf("with lined.go unreadable the run digested it %t and handed %d bodies, want no digest and both",
			isDigested, len(partly.Bodies))
	}
	if _, isRecorded := RecordedOf([]lint.Directive(nil)); isRecorded {
		test.Error("another analyzer's result hands records on")
	}
}

// A body whose record carries a flag, here an interface type literal with an unexported method
// (recordedPresence.UnexportedInterfaceMethod), is one no record can stand for, so its record is not summarizable.
func TestAFlaggedRecordIsNotSummarizable(test *testing.T) {
	test.Parallel()
	asked := true
	sources := []recordedSource{{name: "flagged.go",
		source: "package recorded\n\nfunc Flagged() any {\n\tvar hidden interface{ hidden() }\n\treturn hidden\n}\n"}}
	verdict, runError := run(recordedPass(test, sources, &asked))
	if runError != nil {
		test.Fatalf("running U1000: %v", runError)
	}
	recorded, _ := RecordedOf(verdict)
	if len(recorded.Bodies) != 1 {
		test.Fatalf("the run handed %d bodies, want Flagged's", len(recorded.Bodies))
	}
	if !recorded.Bodies[0].record.Present.UnexportedInterfaceMethod || recorded.Bodies[0].Summarizable() {
		test.Errorf("Flagged's record is flagged %+v and summarizable %t, want the unexported method's flag and not "+
			"summarizable", recorded.Bodies[0].record.Present, recorded.Bodies[0].Summarizable())
	}
}
