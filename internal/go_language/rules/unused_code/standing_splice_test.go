package unused_code

import (
	"crypto/sha256"
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/lint"

	"github.com/system-inc/cohere/internal/go_language/rule"
)

// standingSources are Adamic's shape split over two files: a type only Answer's body builds, whose field group
// nothing reads, so the verdict hangs on that body. Answer's record names types.go, where the type and its fields are
// declared, past its own file, so it is spliced only where the side answers types.go's digest (FileDigestHost). Other
// gives types.go a body of its own, so the host's run digests that file too.
var standingSources = []recordedSource{
	{name: "shape.go", source: `package shape

func Answer() string {
	cases := []compilerAgreementCase{{path: "a", key: "b", rule: "all"}}
	return cases[0].path
}
`},
	{name: "types.go", source: `package shape

type compilerAgreementCase struct {
	path, key, rule string
	group           int
}

func Other() int { return 1 }
`},
}

// standingPackagePath is the path checkShapes checks every package under, which a pass asks the host's side by.
const standingPackagePath = "example.com/shape"

// standingAt is a package's standing answer under which the function declared at one key stands, and no rule is
// confined.
type standingAt struct {
	place recordedPlace
}

// Stands is whether the function at offset of filename is the one that stands.
func (standing standingAt) Stands(filename string, offset int) bool {
	return filename == standing.place.File && offset == standing.place.Offset
}

// Confines answers that no rule is confined: U1000 judges for itself.
func (standingAt) Confines(string) bool { return false }

// Findings answers that the host holds no finding a rule could take.
func (standingAt) Findings(string, string) ([]rule.InheritedFinding, bool) { return nil, false }

// Took records nothing: U1000 takes no finding of the host's.
func (standingAt) Took(string, string, int, int) {
	// No rule here is confined, so none takes anything.
}

// hostRecords are U1000's records of sources as a host's run asked to record hands them on.
func hostRecords(test *testing.T, sources []recordedSource) BodyRecords {
	test.Helper()
	asked := true
	verdict, runError := run(recordedPass(test, sources, &asked))
	if runError != nil {
		test.Fatalf("running the host's U1000: %v", runError)
	}
	recorded, isRecorded := RecordedOf(verdict)
	if !isRecorded {
		test.Fatal("the host's run handed no records on")
	}
	return recorded
}

// copiedRecords is a copy of recorded a test may change without changing recorded: its own list of bodies and its
// own digests.
func copiedRecords(recorded BodyRecords) BodyRecords {
	return BodyRecords{Bodies: slices.Clone(recorded.Bodies), Digests: maps.Clone(recorded.Digests)}
}

// answerKey is the key of Answer's body in pass, whose shape.go declares it, as a standing body is keyed.
func answerKey(test *testing.T, pass *analysis.Pass) recordedPlace {
	test.Helper()
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		if tokenFile.Name() != "shape.go" {
			continue
		}
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Name.Name == "Answer" {
				return recordedPlace{File: tokenFile.Position(function.Pos()).Filename,
					Offset: tokenFile.Offset(function.Pos())}
			}
		}
	}
	test.Fatal("the pass's shape.go declares no Answer")
	return recordedPlace{}
}

// ignoredSource is a file of the package a build constraint leaves out of every build: one a pass reads, as
// go/analysis lets it read an ignored file, but never parses.
var ignoredSource = recordedSource{name: "other.go", source: "//go:build ignore\n\npackage shape\n"}

// splicedRun is U1000's run over sources as an extra build's, in which Answer stands and the host's side is host, its
// verdict worded, and what it says it did with Answer's body. Each of ignored is a file the pass reads and does not
// compile, as a package's IgnoredFiles are.
func splicedRun(
	test *testing.T,
	sources []recordedSource,
	host BodyRecordHost,
	ignored ...recordedSource,
) ([]string, SplicedBodies) {
	test.Helper()
	pass := recordedPass(test, sources, nil)
	compiledRead := pass.ReadFile
	pass.ReadFile = func(filename string) ([]byte, error) {
		for _, each := range ignored {
			if each.name == filename {
				return []byte(each.source), nil
			}
		}
		return compiledRead(filename)
	}
	for _, each := range ignored {
		pass.IgnoredFiles = append(pass.IgnoredFiles, each.name)
	}
	pass.ResultOf[rule.Standing] = standingAt{place: answerKey(test, pass)}
	pass.ResultOf[rule.FactHost] = host
	verdict, runError := run(pass)
	if runError != nil {
		test.Fatalf("running U1000 with Answer standing: %v", runError)
	}
	spliced, isSpliced := SplicedOf(verdict)
	if !isSpliced {
		test.Fatal("a run with Answer standing and a side that answers records says nothing of what it spliced")
	}
	worded, isWorded := verdict.(result)
	if !isWorded {
		test.Fatalf("U1000's result is %T", verdict)
	}
	return wordedVerdict(pass.Fset, worded), spliced
}

// wordedVerdict is verdict's used, unused and quiet, each worded with what list it is in.
func wordedVerdict(fileSet *token.FileSet, verdict result) []string {
	worded := []string{}
	for _, list := range []struct {
		name    string
		objects []object
	}{{"used", verdict.used}, {"unused", verdict.unused}, {"quiet", verdict.quiet}} {
		for _, each := range wordedObjects(fileSet, list.objects) {
			worded = append(worded, list.name+" "+each)
		}
	}
	slices.Sort(worded)
	return worded
}

// A build in which Answer stands splices the host's record of its body where the record is usable, and walks the body
// where it is not, by why (#xeafx79): the side answers no record, the record holds a flag, the build reads Answer's
// own file otherwise than the host did, or a file the record names is one the build reads otherwise, one the side
// answers no digest of, one the build does not compile though it reads it with the host's bytes, or any past Answer's
// own file where the side is no FileDigestHost, or the record does not encode, which the build counts as a record
// that differs from its own. Each way, the verdict is the plain run's. A pass with no
// standing answer, or none with a side that answers records, says nothing of splicing, as the host's and every plain
// run's say nothing.
func TestAStandingBodyIsSplicedOnlyFromAUsableRecord(test *testing.T) {
	test.Parallel()
	recorded := hostRecords(test, standingSources)
	plainPass := recordedPass(test, standingSources, nil)
	plainVerdict, plainError := run(plainPass)
	if plainError != nil {
		test.Fatalf("running U1000 plainly: %v", plainError)
	}
	if _, isSpliced := SplicedOf(plainVerdict); isSpliced {
		test.Error("a plain run says what it spliced")
	}
	plainResult, isResult := plainVerdict.(result)
	if !isResult {
		test.Fatalf("U1000's result is %T", plainVerdict)
	}
	plain := wordedVerdict(plainPass.Fset, plainResult)
	if !slices.ContainsFunc(plain, func(worded string) bool {
		return strings.HasPrefix(worded, "unused field group at types.go ")
	}) {
		test.Fatalf("the plain verdict is %v, which reports no field group: the fixture holds nothing on Answer", plain)
	}

	unhosted := recordedPass(test, standingSources, nil)
	unhosted.ResultOf[rule.Standing] = standingAt{place: answerKey(test, unhosted)}
	if unhostedVerdict, unhostedError := run(unhosted); unhostedError != nil {
		test.Fatalf("running U1000 with no side: %v", unhostedError)
	} else if _, isSpliced := SplicedOf(unhostedVerdict); isSpliced {
		test.Error("a run with no side that answers records says what it spliced")
	}

	answerParsed := func(records BodyRecords) int {
		for index, body := range records.Bodies {
			if body.ParsedFile == "shape.go" {
				return index
			}
		}
		test.Fatal("the host recorded no body of shape.go")
		return -1
	}
	bodiesOnly := bodiesOnlyHost{sides: recordsHost{identifier: standingPackagePath, recorded: recorded}}
	if verdict, spliced := splicedRun(test, standingSources, bodiesOnly); spliced != (SplicedBodies{FilesDiffer: 1}) ||
		!slices.Equal(verdict, plain) {
		test.Errorf("with a side that answers no file's digest the run did %+v with Answer's body and found\n%v\nwant "+
			"it walked for its named file and the plain verdict\n%v", spliced, verdict, plain)
	}
	for _, each := range []struct {
		name    string
		change  func(*recordsHost)
		ignored []recordedSource
		want    SplicedBodies
	}{
		{name: "usable", change: func(*recordsHost) {
			// The host's records as the host's run handed them on.
		}, want: SplicedBodies{Spliced: 1}},
		{name: "another package's side", change: func(host *recordsHost) {
			host.identifier = standingPackagePath + " [" + standingPackagePath + ".test]"
		}, want: SplicedBodies{Unrecorded: 1}},
		{name: "no record", change: func(host *recordsHost) {
			index := answerParsed(host.recorded)
			host.recorded.Bodies = slices.Delete(host.recorded.Bodies, index, index+1)
		}, want: SplicedBodies{Unrecorded: 1}},
		{name: "flagged", change: func(host *recordsHost) {
			host.recorded.Bodies[answerParsed(host.recorded)].record.Present.AllFieldsUsed = true
		}, want: SplicedBodies{Flagged: 1}},
		{name: "own file differs", change: func(host *recordsHost) {
			host.recorded.Digests["shape.go"] = sha256.Sum256([]byte("package shape\n"))
		}, want: SplicedBodies{OwnFileDiffers: 1}},
		{name: "a named file differs", change: func(host *recordsHost) {
			host.recorded.Digests["types.go"] = sha256.Sum256([]byte("package shape\n"))
		}, want: SplicedBodies{FilesDiffer: 1}},
		{name: "a named file undigested", change: func(host *recordsHost) {
			delete(host.recorded.Digests, "types.go")
		}, want: SplicedBodies{FilesDiffer: 1}},
		// The side digests the named file and the build reads it with those bytes, so only its not being compiled
		// keeps the record out.
		{name: "a named file not compiled", change: func(host *recordsHost) {
			index := answerParsed(host.recorded)
			host.recorded.Bodies[index].record.Files = append(slices.Clone(host.recorded.Bodies[index].record.Files),
				ignoredSource.name)
			host.recorded.Digests[ignoredSource.name] = sha256.Sum256([]byte(ignoredSource.source))
		}, ignored: []recordedSource{ignoredSource}, want: SplicedBodies{FilesDiffer: 1}},
		// A local of a kind no word names, which no recorder writes and so no canonical form holds, passes every
		// check of its files and fails only to encode.
		{name: "a record that does not encode", change: func(host *recordsHost) {
			index := answerParsed(host.recorded)
			host.recorded.Bodies[index].record.Locals = slices.Clone(host.recorded.Bodies[index].record.Locals)
			host.recorded.Bodies[index].record.Locals[0].Kind = objectKind(len(kindWords))
		}, want: SplicedBodies{RecordsDiffer: 1, DifferingBodies: [namedDifferences]DifferingBody{
			{File: "shape.go", Offset: strings.Index(standingSources[0].source, "func Answer"), Field: "encoding"},
		}}},
	} {
		host := &recordsHost{identifier: standingPackagePath, recorded: copiedRecords(recorded)}
		each.change(host)
		verdict, spliced := splicedRun(test, standingSources, *host, each.ignored...)
		if spliced != each.want {
			test.Errorf("%s: the run did %+v with Answer's body, want %+v", each.name, spliced, each.want)
		}
		if !slices.Equal(verdict, plain) {
			test.Errorf("%s: the verdict is\n%v\nwant the plain run's\n%v", each.name, verdict, plain)
		}
	}
}

// A build with no record of its own at a body's key differs from the host's record at `record` (#xeafx79, machine
// 2), whatever own holds, even the host's record itself, so a recorder that wrote nothing never reads as agreement.
// It is a unit test because no run reaches it: two bodies at one key are the one way the build's own recorder keeps
// none (bodyRecords), and the files holding them share the key's name, so rule.StandingSpans stands neither.
func TestABuildWithNoRecordOfItsOwnDiffersAtRecord(test *testing.T) {
	test.Parallel()
	recorded := hostRecords(test, standingSources)
	var host bodyRecord
	for _, body := range recorded.Bodies {
		if body.ParsedFile == "shape.go" {
			host = body.record
		}
	}
	if host.Body.File != "shape.go" {
		test.Fatal("the host recorded no body of shape.go")
	}
	for _, each := range []struct {
		name string
		own  bodyRecord
	}{{"no record", bodyRecord{}}, {"the host's own record", host}} {
		field, differError := recordDifference(host, each.own, false)
		if differError != nil {
			test.Fatalf("%s: %v", each.name, differError)
		}
		if field != "record" {
			test.Errorf("%s: with no record of its own the build differs at %q, want record", each.name, field)
		}
	}
}

// A splice the graph fails at falls back to walking every body (#xeafx79): handed a record it cannot take, one a flag
// keeps present, or one it takes and cannot replay, Answer's with a use of types.go's field moved to the offset of
// the file's `package`, where nothing is declared, the run walks a fresh graph whole and finds the plain verdict,
// counts the dropped record in SpliceFailed and not in Spliced, keeps the error as text, and fails nothing.
func TestAFailedSpliceFallsBackToWalkingEveryBody(test *testing.T) {
	test.Parallel()
	recorded := hostRecords(test, standingSources)
	plainPass := recordedPass(test, standingSources, nil)
	plainVerdict, plainError := run(plainPass)
	if plainError != nil {
		test.Fatalf("running U1000 plainly: %v", plainError)
	}
	plainResult, isResult := plainVerdict.(result)
	if !isResult {
		test.Fatalf("U1000's result is %T", plainVerdict)
	}
	plain := wordedVerdict(plainPass.Fset, plainResult)
	var answer bodyRecord
	for _, body := range recorded.Bodies {
		if body.ParsedFile == "shape.go" {
			answer = body.record
		}
	}
	if answer.Body.File != "shape.go" {
		test.Fatal("the host recorded no body of shape.go")
	}
	typesFile := slices.Index(answer.PlaceFiles, "types.go")
	declaredUse := slices.IndexFunc(answer.Uses, func(use recordedUse) bool {
		return use.Used.Kind == endDeclared && int(use.Used.Place.File) == typesFile
	})
	if typesFile < 0 || declaredUse < 0 {
		test.Fatalf("Answer's record %+v uses nothing declared in types.go: the fixture holds nothing to move", answer)
	}

	for _, each := range []struct {
		name    string
		change  func(*bodyRecord)
		failure string
	}{
		{name: "a record the graph cannot take", change: func(record *bodyRecord) {
			record.Present.AllFieldsUsed = true
		}, failure: "is kept present"},
		{name: "a record the graph cannot replay", change: func(record *bodyRecord) {
			record.Uses = slices.Clone(record.Uses)
			record.Uses[declaredUse].Used.Place.Offset = 0
		}, failure: "an object declared at types.go offset 0"},
	} {
		record := answer
		each.change(&record)
		pass := recordedPass(test, standingSources, nil)
		packageDirectives, _ := pass.ResultOf[directivesAnalyzer].([]lint.Directive)
		generatedFiles, _ := pass.ResultOf[generated.Analyzer].(map[string]generated.Generator)
		built := passGraph(pass, packageDirectives, generatedFiles)
		walked, spliced, entryError := enterSpliced(pass, packageDirectives, generatedFiles, built,
			[]bodyRecord{record}, &SplicedBodies{Spliced: 1})
		if entryError != nil {
			test.Errorf("%s: the run failed: %v", each.name, entryError)
			continue
		}
		if spliced.Spliced != 0 || spliced.SpliceFailed != 1 || !strings.Contains(spliced.SpliceFailure, each.failure) {
			test.Errorf("%s: the run did %+v with Answer's body, want it dropped into SpliceFailed for an error "+
				"naming %q", each.name, *spliced, each.failure)
		}
		if walked == built {
			test.Errorf("%s: the verdict is read from the graph whose splice failed", each.name)
		}
		if verdict := wordedVerdict(pass.Fset, walked.results()); !slices.Equal(verdict, plain) {
			test.Errorf("%s: the verdict is\n%v\nwant the plain run's\n%v", each.name, verdict, plain)
		}
	}
}
